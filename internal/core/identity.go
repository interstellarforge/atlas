package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/media"
)

// verifyOwnPassword checks the actor's current password inside the transaction.
func verifyOwnPassword(q queryer, userID int64, password string) error {
	var hash string
	if err := q.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return err
	}
	if ok, _ := auth.VerifyPassword(password, hash); !ok {
		return rule("the password is not correct")
	}
	return nil
}

// ChangeEmail needs the current password. There is no mail server to confirm the new address with, so the
// address is only checked for shape and uniqueness; sessions stay.
func (s *Service) ChangeEmail(ctx context.Context, a auth.Actor, email, password string) (User, error) {
	if err := selfOnly(a); err != nil {
		return User{}, err
	}
	email, err := validEmail(email)
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.tx(func(tx *sql.Tx) error {
		if err := verifyOwnPassword(tx, a.UserID, password); err != nil {
			return err
		}
		var old string
		if err := tx.QueryRow(`SELECT email FROM users WHERE id = ?`, a.UserID).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET email = ? WHERE id = ?`, email, a.UserID); err != nil {
			if isUnique(err) {
				return rule("email already in use")
			}
			return err
		}
		if err := tx.QueryRow(`SELECT id, email, name, is_admin, avatar_path IS NOT NULL FROM users WHERE id = ?`, a.UserID).
			Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.Avatar); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "account.email", Target: a.String(), Event: "account.updated", Changes: map[string]any{"from": old, "to": email}})
	})
	return u, err
}

// DeleteAccount closes the actor's own account for good: it needs the password, refuses to orphan the
// instance (last admin) or a project (sole admin), removes memberships, sessions, tokens, follows and
// notifications, and anonymises the user row. Items, comments and the audit trail stay, now showing
// "Deleted user". It cannot be undone.
func (s *Service) DeleteAccount(ctx context.Context, a auth.Actor, password string) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	var avatar sql.NullString
	err := s.tx(func(tx *sql.Tx) error {
		if err := verifyOwnPassword(tx, a.UserID, password); err != nil {
			return err
		}
		var admin bool
		if err := tx.QueryRow(`SELECT is_admin, avatar_path FROM users WHERE id = ?`, a.UserID).Scan(&admin, &avatar); err != nil {
			return err
		}
		if admin {
			if n, err := activeAdmins(tx, a.UserID); err != nil {
				return err
			} else if n == 0 {
				return rule("you are the only admin; make someone else admin first")
			}
		}
		rows, err := tx.Query(`SELECT project_id FROM project_members WHERE user_id = ?`, a.UserID)
		if err != nil {
			return err
		}
		var projects []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			projects = append(projects, id)
		}
		rows.Close()
		for _, id := range projects {
			if err := s.guardLastAdmin(tx, id, a.UserID); err != nil {
				return rule("you are the only admin of a project; give someone else admin first")
			}
		}
		if err := scrubAuditPII(tx, a); err != nil {
			return err
		}
		now := s.nowMs()
		junk := make([]byte, 32)
		_, _ = rand.Read(junk)
		stmts := []struct {
			q    string
			args []any
		}{
			{`DELETE FROM project_members WHERE user_id = ?`, []any{a.UserID}},
			{`DELETE FROM sessions WHERE user_id = ?`, []any{a.UserID}},
			{`UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, []any{now, a.UserID}},
			{`DELETE FROM item_follows WHERE user_id = ?`, []any{a.UserID}},
			{`DELETE FROM notifications WHERE user_id = ?`, []any{a.UserID}},
			{`DELETE FROM recovery_codes WHERE user_id = ?`, []any{a.UserID}},
			{`UPDATE work_items SET assignee_id = NULL WHERE assignee_id = ?`, []any{a.UserID}},
			{`UPDATE users SET name = 'Deleted user', email = ?, password_hash = ?, is_admin = 0, deactivated_at = ?, avatar_path = NULL,
				totp_secret = NULL, totp_pending = NULL, totp_enabled = 0, preferences = '{}' WHERE id = ?`,
				[]any{"deleted-" + strconv.FormatInt(a.UserID, 10) + "@deleted.invalid", "!" + hex.EncodeToString(junk), now, a.UserID}},
		}
		for _, st := range stmts {
			if _, err := tx.Exec(st.q, st.args...); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "account.delete", Target: a.String(), Event: "account.updated", Changes: map[string]any{}})
	})
	if err == nil && avatar.Valid && s.media != nil {
		s.media.RemoveAll(avatar.String)
	}
	return err
}

// ---- profile picture ----

// SetAvatar stores the image through the upload pipeline (limits, magic bytes, pixel count). Only a
// re-encoded thumbnail is ever served, which drops EXIF and other metadata.
func (s *Service) SetAvatar(ctx context.Context, a auth.Actor, filename string, r io.Reader) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	if s.media == nil {
		return rule("uploads are not enabled")
	}
	saved, err := s.media.Save(r, cleanFilename(filename))
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return ErrTooLarge
	case errors.Is(err, media.ErrType), errors.Is(err, media.ErrCorrupt):
		return invalid("%s", err.Error())
	case err != nil:
		return err
	}
	if _, err := s.media.Thumb(saved.Path); err != nil { // also proves the image decodes
		s.media.RemoveAll(saved.Path)
		return invalid("%s", err.Error())
	}
	var old sql.NullString
	err = s.tx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT avatar_path FROM users WHERE id = ?`, a.UserID).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET avatar_path = ? WHERE id = ?`, saved.Path, a.UserID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "account.avatar", Target: a.String(), Event: "account.updated", Changes: map[string]any{"set": true}})
	})
	if err != nil {
		s.media.RemoveAll(saved.Path)
		return err
	}
	if old.Valid {
		s.media.RemoveAll(old.String)
	}
	return nil
}

func (s *Service) RemoveAvatar(ctx context.Context, a auth.Actor) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	var old sql.NullString
	err := s.tx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT avatar_path FROM users WHERE id = ?`, a.UserID).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET avatar_path = NULL WHERE id = ?`, a.UserID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "account.avatar", Target: a.String(), Event: "account.updated", Changes: map[string]any{"set": false}})
	})
	if err == nil && old.Valid && s.media != nil {
		s.media.RemoveAll(old.String)
	}
	return err
}

// AvatarThumb returns the thumbnail path of a user's picture; any signed-in person may see it (people are listed
// anyway), a token only for people it can list.
func (s *Service) AvatarThumb(ctx context.Context, a auth.Actor, userID int64) (string, error) {
	if !a.Can(auth.ScopeRead) || s.media == nil {
		return "", ErrForbidden
	}
	if a.Kind == auth.KindToken {
		q, args := s.sharedUsersQuery(a)
		var n int
		if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM (`+q+`) WHERE id = ?`, append(args, userID)...).Scan(&n); err != nil || n == 0 {
			return "", ErrNotFound
		}
	}
	var p sql.NullString
	if err := s.db.R.QueryRow(`SELECT avatar_path FROM users WHERE id = ?`, userID).Scan(&p); err != nil || !p.Valid {
		return "", ErrNotFound
	}
	return s.media.Thumb(p.String)
}

// ---- two-factor authentication (TOTP) ----

type Security struct {
	TOTP              bool `json:"totp"`
	RecoveryRemaining int  `json:"recovery_remaining"`
}

func (s *Service) GetSecurity(ctx context.Context, a auth.Actor) (Security, error) {
	if err := selfOnly(a); err != nil {
		return Security{}, err
	}
	var sec Security
	if err := s.db.R.QueryRow(`SELECT totp_enabled FROM users WHERE id = ?`, a.UserID).Scan(&sec.TOTP); err != nil {
		return sec, err
	}
	err := s.db.R.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, a.UserID).Scan(&sec.RecoveryRemaining)
	return sec, err
}

// TOTPSetup starts enrolment: a fresh secret is stored as pending until a valid code confirms it.
func (s *Service) TOTPSetup(ctx context.Context, a auth.Actor) (secret, uri string, err error) {
	if err := selfOnly(a); err != nil {
		return "", "", err
	}
	secret = auth.NewTOTPSecret()
	err = s.tx(func(tx *sql.Tx) error {
		var on bool
		var email string
		if err := tx.QueryRow(`SELECT totp_enabled, email FROM users WHERE id = ?`, a.UserID).Scan(&on, &email); err != nil {
			return err
		}
		if on {
			return rule("two-factor authentication is already on")
		}
		if _, err := tx.Exec(`UPDATE users SET totp_pending = ? WHERE id = ?`, secret, a.UserID); err != nil {
			return err
		}
		uri = auth.TOTPURI("Atlas", email, secret)
		return nil
	})
	return secret, uri, err
}

// TOTPEnable confirms enrolment with a code from the app and returns ten recovery codes, once.
func (s *Service) TOTPEnable(ctx context.Context, a auth.Actor, code string) ([]string, error) {
	if err := selfOnly(a); err != nil {
		return nil, err
	}
	codes := auth.NewRecoveryCodes(10)
	err := s.tx(func(tx *sql.Tx) error {
		var pending sql.NullString
		if err := tx.QueryRow(`SELECT totp_pending FROM users WHERE id = ? AND totp_enabled = 0`, a.UserID).Scan(&pending); err != nil || !pending.Valid {
			return rule("start the setup first")
		}
		step, ok := auth.VerifyTOTP(pending.String, code, time.Now(), 0)
		if !ok {
			return rule("that code is not correct")
		}
		if _, err := tx.Exec(`UPDATE users SET totp_secret = totp_pending, totp_pending = NULL, totp_enabled = 1, totp_last_step = ? WHERE id = ?`, step, a.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, a.UserID); err != nil {
			return err
		}
		for _, c := range codes {
			if _, err := tx.Exec(`INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, a.UserID, auth.HashRecoveryCode(c)); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "account.totp.enable", Target: a.String(), Event: "account.updated", Changes: map[string]any{}})
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// TOTPDisable needs the password and a current code or an unused recovery code.
func (s *Service) TOTPDisable(ctx context.Context, a auth.Actor, password, code string) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if err := verifyOwnPassword(tx, a.UserID, password); err != nil {
			return err
		}
		var secret sql.NullString
		var on bool
		var last int64
		if err := tx.QueryRow(`SELECT totp_secret, totp_enabled, totp_last_step FROM users WHERE id = ?`, a.UserID).Scan(&secret, &on, &last); err != nil {
			return err
		}
		if !on {
			return rule("two-factor authentication is not on")
		}
		ok := false
		if _, good := auth.VerifyTOTP(secret.String, code, time.Now(), last); good {
			ok = true
		} else if res, err := tx.Exec(`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
			s.nowMs(), a.UserID, auth.HashRecoveryCode(code)); err == nil {
			n, _ := res.RowsAffected()
			ok = n == 1
		}
		if !ok {
			return rule("that code is not correct")
		}
		return s.clearTOTP(ctx, tx, a, a.UserID, "account.totp.disable")
	})
}

func (s *Service) clearTOTP(ctx context.Context, tx *sql.Tx, a auth.Actor, userID int64, op string) error {
	if _, err := tx.Exec(`UPDATE users SET totp_secret = NULL, totp_pending = NULL, totp_enabled = 0, totp_last_step = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return s.journal(ctx, tx, a, change{Operation: op, Target: userTarget(userID), Event: "account.updated", Changes: map[string]any{}})
}

// ResetUserTOTP lets a global admin switch 2FA off for a user who lost their device and recovery codes.
func (s *Service) ResetUserTOTP(ctx context.Context, a auth.Actor, id int64) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&n); err != nil || n == 0 {
			return ErrNotFound
		}
		return s.clearTOTP(ctx, tx, a, id, "user.totp.reset")
	})
}

var _ = strings.TrimSpace

// scrubAuditPII removes the account's e-mail addresses from audit entries (email changes, invitations to it),
// which would otherwise outlive the anonymised user row.
func scrubAuditPII(tx *sql.Tx, a auth.Actor) error {
	var email string
	if err := tx.QueryRow(`SELECT email FROM users WHERE id = ?`, a.UserID).Scan(&email); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE audit_entries SET changes = '{}' WHERE operation = 'account.email' AND target = ?`, a.String()); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE audit_entries SET changes = json_remove(changes, '$.email')
		WHERE operation = 'invite.create' AND lower(json_extract(changes, '$.email')) = lower(?)`, email)
	return err
}

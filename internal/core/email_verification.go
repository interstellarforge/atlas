package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Email verification (ADR-0031): a mailed single-use link proves an address (users.email_verified_at). Verified email
// is what lets a confirmed domain join an organization at once. Without outgoing mail nothing can be verified this
// way (an accepted invitation also verifies its address).

const (
	verifyTTLHours   = 24
	verifyResendMs   = 60_000
	verifyTTLMs      = verifyTTLHours * 3_600_000
	errVerifyMessage = "this link is not valid"
)

// JoinOption is an organization the caller's verified email domain may join.
type JoinOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// RequestEmailVerification mails a link to the caller's own address. Already verified, or a link sent in the last
// minute: nothing changes. Needs outgoing mail.
func (s *Service) RequestEmailVerification(ctx context.Context, a auth.Actor) error {
	if !a.Can(auth.ScopeWrite) || a.Projects != nil || a.Kind != auth.KindHuman {
		return ErrForbidden
	}
	return s.tx(func(tx *sql.Tx) error {
		st, err := s.standing(tx, a)
		if err != nil {
			return err
		}
		if !st.active {
			return ErrForbidden
		}
		mail, _, err := s.MailEffective(tx)
		if err != nil {
			return err
		}
		if !mail.Configured() {
			return rule("outgoing email is not set up on this instance")
		}
		var email string
		var verified sql.NullInt64
		if err := tx.QueryRow(`SELECT email, email_verified_at FROM users WHERE id = ?`, a.UserID).Scan(&email, &verified); err != nil {
			return err
		}
		now := s.nowMs()
		var recent bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM email_verifications WHERE user_id = ? AND created_at > ?)`, a.UserID, now-verifyResendMs).Scan(&recent); err != nil {
			return err
		}
		if verified.Valid || recent {
			return nil
		}
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		token := base64.RawURLEncoding.EncodeToString(b)
		if _, err := tx.Exec(`INSERT INTO email_verifications (user_id, email, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			a.UserID, email, auth.HashRecoveryCode(token), now, now+verifyTTLMs); err != nil {
			return err
		}
		return s.enqueueMail(tx, email, "verify_email", map[string]any{"Link": s.link("/verify/" + token), "Hours": verifyTTLHours})
	})
}

// VerifyEmail spends a link (compare-and-set), marks the address verified and joins the organizations whose
// confirmed domain in mode auto matches it. Every unusable link gives the same error. The link must still match the
// account's current address.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	return s.tx(func(tx *sql.Tx) error {
		now := s.nowMs()
		var id, userID int64
		var email, current string
		err := tx.QueryRow(`SELECT v.id, v.user_id, v.email, u.email FROM email_verifications v JOIN users u ON u.id = v.user_id
			WHERE v.token_hash = ? AND v.used_at IS NULL AND v.expires_at > ? AND u.status = 'active'`, auth.HashRecoveryCode(token), now).Scan(&id, &userID, &email, &current)
		if err == sql.ErrNoRows || (err == nil && !strings.EqualFold(email, current)) {
			return rule(errVerifyMessage)
		}
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE email_verifications SET used_at = ? WHERE id = ? AND used_at IS NULL`, now, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return rule(errVerifyMessage)
		}
		if _, err := tx.Exec(`UPDATE users SET email_verified_at = COALESCE(email_verified_at, ?) WHERE id = ?`, now, userID); err != nil {
			return err
		}
		actor := auth.Actor{Kind: auth.KindHuman, UserID: userID}
		if err := s.journal(ctx, tx, actor, change{Operation: "user.email.verify", Target: userTarget(userID), Event: "admin.updated", Changes: map[string]any{}}); err != nil {
			return err
		}
		return s.autoJoin(ctx, tx, actor, 0)
	})
}

// autoJoin adds the account as USER to the organizations whose confirmed domain (mode auto) matches its verified
// email, when outgoing mail is set up and no ADMIN ever rejected a request of the account. orgID limits it to one
// organization (0 = all). It reports whether something was joined.
func (s *Service) autoJoin(ctx context.Context, tx *sql.Tx, a auth.Actor, orgID int64) error {
	mail, _, err := s.MailEffective(tx)
	if err != nil || !mail.Configured() {
		return err
	}
	rows, err := tx.Query(`SELECT o.id, o.key FROM organization_domains d JOIN organizations o ON o.id = d.organization_id AND o.status = 'active'
		JOIN users u ON u.id = ?1 AND u.email_verified_at IS NOT NULL AND lower(substr(u.email, instr(u.email, '@') + 1)) = lower(d.domain)
		WHERE d.status = 'confirmed' AND d.mode = 'auto' AND (?2 = 0 OR o.id = ?2)
		AND NOT EXISTS (SELECT 1 FROM organization_memberships m WHERE m.organization_id = o.id AND m.user_id = ?1)
		AND NOT EXISTS (SELECT 1 FROM organization_join_requests r WHERE r.organization_id = o.id AND r.user_id = ?1 AND r.status = 'rejected')`, a.UserID, orgID)
	if err != nil {
		return err
	}
	type target struct {
		id  int64
		key string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.key); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	rows.Close()
	for _, t := range targets {
		if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at, created_by) VALUES (?, ?, 'user', ?, ?)
			ON CONFLICT (organization_id, user_id) DO NOTHING`, t.id, a.UserID, s.nowMs(), a.UserID); err != nil {
			return err
		}
		if err := s.journal(ctx, tx, a, change{Operation: "org.join.auto", Target: orgTarget(t.key), Event: "admin.updated", Changes: map[string]any{}}); err != nil {
			return err
		}
	}
	return nil
}

// JoinOptions lists organizations whose confirmed domain matches the caller's verified email. An unverified address
// sees nothing, so a stranger cannot learn which organization claims a domain by registering with it.
func (s *Service) JoinOptions(ctx context.Context, a auth.Actor) ([]JoinOption, error) {
	if !a.Can(auth.ScopeRead) || a.Projects != nil || a.Kind != auth.KindHuman {
		return nil, ErrForbidden
	}
	rows, err := s.db.R.Query(`SELECT o.key, o.name, d.mode FROM organization_domains d JOIN organizations o ON o.id = d.organization_id AND o.status = 'active'
		JOIN users u ON u.id = ?1 AND u.status = 'active' AND u.email_verified_at IS NOT NULL AND lower(substr(u.email, instr(u.email, '@') + 1)) = lower(d.domain)
		WHERE d.status = 'confirmed' AND NOT EXISTS (SELECT 1 FROM organization_memberships m WHERE m.organization_id = o.id AND m.user_id = ?1)
		ORDER BY o.name`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JoinOption{}
	for rows.Next() {
		var o JoinOption
		if err := rows.Scan(&o.Key, &o.Name, &o.Mode); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

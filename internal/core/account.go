package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Account operations act on the actor's own account only, and only through a user session: a token
// cannot change a password, list sessions or mint other credentials.
func selfOnly(a auth.Actor) error {
	if a.Kind != auth.KindHuman {
		return ErrForbidden
	}
	return nil
}

type Session struct {
	ID        int64 `json:"id"`
	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
	Current   bool  `json:"current"`
}

func (s *Service) UpdateProfile(ctx context.Context, a auth.Actor, name string) (User, error) {
	if err := selfOnly(a); err != nil {
		return User{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return User{}, invalid("name is required (max 100 characters)")
	}
	var u User
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE users SET name = ? WHERE id = ?`, name, a.UserID); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT id, email, name, is_admin FROM users WHERE id = ?`, a.UserID).Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "account.profile", Target: a.String(), Event: "account.updated", Changes: map[string]any{"name": name}})
	})
	return u, err
}

// ChangePassword needs the current password, and signs out every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, a auth.Actor, current, next string) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	if err := checkPassword(s.Policy(), next); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		var hash string
		if err := tx.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, a.UserID).Scan(&hash); err != nil {
			return err
		}
		if ok, _ := auth.VerifyPassword(current, hash); !ok {
			return rule("the current password is not correct")
		}
		fresh, err := auth.HashPassword(next)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?`, fresh, a.UserID); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ? AND rowid <> ?`, a.UserID, a.SessionID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		return s.journal(ctx, tx, a, change{Operation: "account.password", Target: a.String(), Event: "account.updated",
			Changes: map[string]any{"sessions_revoked": n}}) // never the password itself
	})
}

func (s *Service) ListSessions(ctx context.Context, a auth.Actor) ([]Session, error) {
	if err := selfOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT rowid, created_at, expires_at FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC`,
		a.UserID, s.nowMs())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.CreatedAt, &x.ExpiresAt); err != nil {
			return nil, err
		}
		x.Current = x.ID == a.SessionID
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) RevokeSession(ctx context.Context, a auth.Actor, id int64) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM sessions WHERE rowid = ? AND user_id = ?`, id, a.UserID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "account.session.revoke", Target: a.String(), Event: "account.updated", Changes: map[string]any{"session": id}})
	})
}

// Preferences are per user and small: theme, default project view and muted notification types.
type Preferences struct {
	Theme              string   `json:"theme"`        // system, light or dark
	DefaultView        string   `json:"default_view"` // board, list, backlog or tree
	MutedNotifications []string `json:"muted_notifications"`
}

var (
	themes       = []string{"system", "light", "dark"}
	defaultViews = []string{"board", "list", "backlog", "tree"}
	notifyTypes  = []string{NotifyAssigned, NotifyMention, NotifyBlockerDone, NotifyFollowed}
	defaultPrefs = Preferences{Theme: "system", DefaultView: "board", MutedNotifications: []string{}}
)

func loadPrefs(q queryer, userID int64) (Preferences, error) {
	var raw string
	if err := q.QueryRow(`SELECT preferences FROM users WHERE id = ?`, userID).Scan(&raw); err != nil {
		return Preferences{}, err
	}
	p := defaultPrefs
	_ = json.Unmarshal([]byte(raw), &p)
	if p.MutedNotifications == nil {
		p.MutedNotifications = []string{}
	}
	return p, nil
}

func (s *Service) GetPreferences(ctx context.Context, a auth.Actor) (Preferences, error) {
	if err := selfOnly(a); err != nil {
		return Preferences{}, err
	}
	return loadPrefs(s.db.R, a.UserID)
}

func (s *Service) SetPreferences(ctx context.Context, a auth.Actor, p Preferences) (Preferences, error) {
	if err := selfOnly(a); err != nil {
		return Preferences{}, err
	}
	if !slices.Contains(themes, p.Theme) || !slices.Contains(defaultViews, p.DefaultView) {
		return Preferences{}, invalid("theme must be one of %s and default_view one of %s", strings.Join(themes, ", "), strings.Join(defaultViews, ", "))
	}
	for _, n := range p.MutedNotifications {
		if !slices.Contains(notifyTypes, n) {
			return Preferences{}, invalid("unknown notification type %q", n)
		}
	}
	if p.MutedNotifications == nil {
		p.MutedNotifications = []string{}
	}
	body, _ := json.Marshal(p)
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE users SET preferences = ? WHERE id = ?`, string(body), a.UserID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "account.preferences", Target: a.String(), Event: "account.updated", Changes: p})
	})
	return p, err
}

// muted reports whether the user switched this notification type off.
func muted(q queryer, userID int64, typ string) bool {
	p, err := loadPrefs(q, userID)
	return err == nil && slices.Contains(p.MutedNotifications, typ)
}

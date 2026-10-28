package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// Policy is the instance-wide account policy, kept in instance_settings. Missing keys use these defaults.
type Policy struct {
	MinPasswordLength int    `json:"min_password_length"` // 8-128
	Registration      string `json:"registration"`        // closed (admins create accounts), invite or open
	AllowedDomain     string `json:"allowed_domain"`      // open registration only: e.g. example.com; empty = any
	SessionDays       int    `json:"session_days"`        // 1-90
	DefaultRole       string `json:"default_role"`        // project role used when a member is added without one
	DoneVisibleDays   int    `json:"done_visible_days"`   // 0-3650: lists hide completed items not updated for this long; 0 = never
}

var defaultPolicy = Policy{MinPasswordLength: 8, Registration: "closed", SessionDays: 14, DefaultRole: "member", DoneVisibleDays: 14}

func loadPolicy(q queryer) Policy {
	p := defaultPolicy
	rows, err := q.Query(`SELECT key, value FROM instance_settings`)
	if err != nil {
		return p
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		switch k {
		case "min_password_length":
			p.MinPasswordLength, _ = strconv.Atoi(v)
		case "registration":
			p.Registration = v
		case "allowed_domain":
			p.AllowedDomain = v
		case "session_days":
			p.SessionDays, _ = strconv.Atoi(v)
		case "default_role":
			p.DefaultRole = v
		case "done_visible_days":
			if n, err := strconv.Atoi(v); err == nil {
				p.DoneVisibleDays = n
			}
		}
	}
	if p.MinPasswordLength < 8 || p.SessionDays < 1 {
		return defaultPolicy
	}
	return p
}

func (s *Service) Policy() Policy { return loadPolicy(s.db.R) }

// DoneCutoff is the updated_at (unix ms) before which completed items count as old, or nil when they never do.
func (s *Service) DoneCutoff() *int64 {
	days := s.Policy().DoneVisibleDays
	if days <= 0 {
		return nil
	}
	t := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	return &t
}

// ApplyPolicy pushes the session length to the auth service; call at startup and after every change.
func (s *Service) ApplyPolicy() {
	s.users.SetSessionTTL(time.Duration(s.Policy().SessionDays) * 24 * time.Hour)
}

func (s *Service) GetPolicy(ctx context.Context, a auth.Actor) (Policy, error) {
	if err := adminOnly(a); err != nil {
		return Policy{}, err
	}
	return s.Policy(), nil
}

func (s *Service) SetPolicy(ctx context.Context, a auth.Actor, p Policy) (Policy, error) {
	if err := adminOnly(a); err != nil {
		return Policy{}, err
	}
	p.AllowedDomain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p.AllowedDomain), "@"))
	if p.MinPasswordLength < 8 || p.MinPasswordLength > 128 || p.SessionDays < 1 || p.SessionDays > 90 || p.DoneVisibleDays < 0 || p.DoneVisibleDays > 3650 ||
		!slices.Contains([]string{"closed", "invite", "open"}, p.Registration) || strings.ContainsAny(p.AllowedDomain, " @/") {
		return Policy{}, invalid("min_password_length 8-128, session_days 1-90, done_visible_days 0-3650, registration closed, invite or open, allowed_domain like example.com")
	}
	if p.DefaultRole == "" {
		p.DefaultRole = defaultPolicy.DefaultRole
	}
	err := s.tx(func(tx *sql.Tx) error {
		var known int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM roles WHERE name = ?`, p.DefaultRole).Scan(&known); err != nil {
			return err
		}
		if known == 0 {
			return invalid("unknown default_role")
		}
		for k, v := range map[string]string{"min_password_length": strconv.Itoa(p.MinPasswordLength), "registration": p.Registration,
			"allowed_domain": p.AllowedDomain, "session_days": strconv.Itoa(p.SessionDays), "default_role": p.DefaultRole,
			"done_visible_days": strconv.Itoa(p.DoneVisibleDays)} {
			if _, err := tx.Exec(`INSERT INTO instance_settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "policy.update", Target: "instance", Event: "admin.updated", Changes: p})
	})
	if err == nil {
		s.ApplyPolicy()
	}
	return p, err
}

func checkPassword(p Policy, password string) error {
	if len(password) < p.MinPasswordLength {
		return invalid("the password needs at least %d characters", p.MinPasswordLength)
	}
	if len(password) > 256 {
		return invalid("the password is too long")
	}
	return nil
}

func validEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 200 {
		return "", invalid("enter a valid email address")
	}
	return email, nil
}

// ---- registration and invites ----

type Invite struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	UsedAt    *int64 `json:"used_at"`
}

// CreateInvite returns the invite code once; only its hash is stored. An invite with an email can only be
// redeemed with that email. With email set up (ADR-0012) the invite is also mailed; otherwise the admin passes the code on.
func (s *Service) CreateInvite(ctx context.Context, a auth.Actor, email string, admin bool, days int) (Invite, string, error) {
	if err := adminOnly(a); err != nil {
		return Invite{}, "", err
	}
	if email = strings.TrimSpace(email); email != "" {
		var err error
		if email, err = validEmail(email); err != nil {
			return Invite{}, "", err
		}
	}
	if days < 1 || days > 30 {
		days = 7
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Invite{}, "", err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	now := s.nowMs()
	inv := Invite{Email: email, IsAdmin: admin, CreatedAt: now, ExpiresAt: now + int64(days)*86_400_000}
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO invites (code_hash, email, is_admin, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
			auth.HashRecoveryCode(code), email, admin, a.UserID, inv.CreatedAt, inv.ExpiresAt)
		if err != nil {
			return err
		}
		inv.ID, _ = res.LastInsertId()
		if email != "" {
			var inviter string
			_ = tx.QueryRow(`SELECT name FROM users WHERE id = ?`, a.UserID).Scan(&inviter)
			if err := s.enqueueMail(tx, email, "invite", map[string]any{"Inviter": inviter, "Code": code, "Days": days,
				"Link": s.link("/register?invite=" + code)}); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "invite.create", Target: "invite-" + strconv.FormatInt(inv.ID, 10), Event: "admin.updated",
			Changes: map[string]any{"email": email, "is_admin": admin}}) // never the code
	})
	return inv, code, err
}

func (s *Service) ListInvites(ctx context.Context, a auth.Actor) ([]Invite, error) {
	if err := adminOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, email, is_admin, created_at, expires_at, used_at FROM invites ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		var used sql.NullInt64
		if err := rows.Scan(&i.ID, &i.Email, &i.IsAdmin, &i.CreatedAt, &i.ExpiresAt, &used); err != nil {
			return nil, err
		}
		if used.Valid {
			i.UsedAt = &used.Int64
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Service) RevokeInvite(ctx context.Context, a auth.Actor, id int64) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM invites WHERE id = ? AND used_at IS NULL`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "invite.revoke", Target: "invite-" + strconv.FormatInt(id, 10), Event: "admin.updated", Changes: map[string]any{}})
	})
}

// Register creates an account without an admin: with a valid invite, or when the policy allows open
// registration (optionally for one email domain). Everything else is refused.
func (s *Service) Register(ctx context.Context, email, name, password, invite string) (User, error) {
	email, err := validEmail(email)
	if err != nil {
		return User{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return User{}, invalid("name is required (max 100 characters)")
	}
	pol := s.Policy()
	if err := checkPassword(pol, password); err != nil {
		return User{}, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.tx(func(tx *sql.Tx) error {
		admin := false
		var inviteID int64
		switch {
		case invite != "":
			var invEmail string
			var expires int64
			var used sql.NullInt64
			err := tx.QueryRow(`SELECT id, email, is_admin, expires_at, used_at FROM invites WHERE code_hash = ?`, auth.HashRecoveryCode(invite)).
				Scan(&inviteID, &invEmail, &admin, &expires, &used)
			if err == sql.ErrNoRows || (err == nil && (used.Valid || expires < s.nowMs() || (invEmail != "" && !strings.EqualFold(invEmail, email)))) {
				return rule("this invite is not valid")
			}
			if err != nil {
				return err
			}
		case pol.Registration == "open":
			if pol.AllowedDomain != "" && !strings.HasSuffix(strings.ToLower(email), "@"+pol.AllowedDomain) {
				return rule("registration is limited to @%s addresses", pol.AllowedDomain)
			}
		default:
			return ErrForbidden
		}
		res, err := tx.Exec(`INSERT INTO users (email, name, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`, email, name, hash, admin, s.nowMs())
		if err != nil {
			if isUnique(err) {
				return rule("email already in use")
			}
			return err
		}
		u = User{Email: email, Name: name, IsAdmin: admin}
		u.ID, _ = res.LastInsertId()
		if inviteID != 0 {
			if _, err := tx.Exec(`UPDATE invites SET used_at = ?, used_by = ? WHERE id = ?`, s.nowMs(), u.ID, inviteID); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, auth.Actor{Kind: auth.KindHuman, UserID: u.ID}, change{Operation: "user.register", Target: userTarget(u.ID), Event: "admin.updated",
			Changes: map[string]any{"invite": inviteID != 0}})
	})
	return u, err
}

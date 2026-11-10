package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// OrgInvite is an invitation of an email address to an organization (ADR-0031). State is pending, accepted, revoked
// or expired (derived from expires_at).
type OrgInvite struct {
	ID        int64    `json:"id"`
	Email     string   `json:"email"`
	State     string   `json:"state"`
	Projects  []string `json:"projects"`
	CreatedAt int64    `json:"created_at"`
	ExpiresAt int64    `json:"expires_at"`
}

// InvitePreview is what the holder of a valid token sees before accepting.
type InvitePreview struct {
	Organization  string `json:"organization"`
	Email         string `json:"email"`
	AccountExists bool   `json:"account_exists"`
}

func errInviteInvalid() error { return rule("this invitation is not valid") }

// CreateOrgInvite invites an email address as USER of the organization, optionally onto projects of that organization.
// Only an explicit ADMIN of the organization may invite (not the Atlas admin, not a support grant). The answer is the
// same whether or not the address has an account anywhere. The token is returned once; only its hash is stored. A new
// invitation for the same address replaces the waiting one.
func (s *Service) CreateOrgInvite(ctx context.Context, a auth.Actor, orgKey, email string, projectKeys []string, days int) (OrgInvite, string, error) {
	email, err := validEmail(email)
	if err != nil {
		return OrgInvite{}, "", err
	}
	if days < 1 || days > 30 {
		days = 7
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return OrgInvite{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	now := s.nowMs()
	inv := OrgInvite{Email: email, State: "pending", Projects: []string{}, CreatedAt: now, ExpiresAt: now + int64(days)*86_400_000}
	err = s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, orgKey, orgMembers)
		if err != nil {
			return err
		}
		if o.Role != "admin" {
			return ErrForbidden
		}
		var projectIDs []int64
		for _, k := range projectKeys {
			var id int64
			err := tx.QueryRow(`SELECT id FROM projects WHERE key = ? AND organization_id = ? AND archived_at IS NULL`, strings.ToUpper(strings.TrimSpace(k)), o.ID).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				return invalid("unknown project %q in this organization", k)
			}
			if err != nil {
				return err
			}
			projectIDs = append(projectIDs, id)
			inv.Projects = append(inv.Projects, strings.ToUpper(strings.TrimSpace(k)))
		}
		if _, err := tx.Exec(`UPDATE organization_invites SET status = 'revoked' WHERE organization_id = ? AND email = ? AND status = 'pending'`, o.ID, email); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO organization_invites (organization_id, email, token_hash, invited_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
			o.ID, email, auth.HashRecoveryCode(token), a.UserID, inv.CreatedAt, inv.ExpiresAt)
		if err != nil {
			return err
		}
		inv.ID, _ = res.LastInsertId()
		for _, pid := range projectIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO organization_invite_projects (invite_id, project_id) VALUES (?, ?)`, inv.ID, pid); err != nil {
				return err
			}
		}
		var inviter string
		_ = tx.QueryRow(`SELECT name FROM users WHERE id = ?`, a.UserID).Scan(&inviter)
		if err := s.enqueueMail(tx, email, "org_invite", map[string]any{"Inviter": inviter, "Organization": o.Name, "Days": days,
			"Link": s.link("/invite/" + token)}); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.invite.create", Target: orgTarget(o.Key), Event: "admin.updated",
			Changes: map[string]any{"invite": inv.ID, "email": email, "projects": len(projectIDs)}}) // never the token
	})
	return inv, token, err
}

func (s *Service) ListOrgInvites(ctx context.Context, a auth.Actor, orgKey string) ([]OrgInvite, error) {
	o, err := s.authorizeOrg(s.db.R, a, orgKey, orgMembers)
	if err != nil {
		return nil, err
	}
	now := s.nowMs()
	rows, err := s.db.R.Query(`SELECT i.id, i.email, CASE WHEN i.status = 'pending' AND i.expires_at <= ? THEN 'expired' ELSE i.status END,
		i.created_at, i.expires_at, COALESCE((SELECT group_concat(p.key) FROM organization_invite_projects x JOIN projects p ON p.id = x.project_id WHERE x.invite_id = i.id), '')
		FROM organization_invites i WHERE i.organization_id = ? ORDER BY i.id DESC LIMIT 200`, now, o.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgInvite{}
	for rows.Next() {
		var i OrgInvite
		var projects string
		if err := rows.Scan(&i.ID, &i.Email, &i.State, &i.CreatedAt, &i.ExpiresAt, &projects); err != nil {
			return nil, err
		}
		i.Projects = []string{}
		if projects != "" {
			i.Projects = strings.Split(projects, ",")
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Service) RevokeOrgInvite(ctx context.Context, a auth.Actor, orgKey string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, orgKey, orgMembers)
		if err != nil {
			return err
		}
		if o.Role != "admin" {
			return ErrForbidden
		}
		res, err := tx.Exec(`UPDATE organization_invites SET status = 'revoked' WHERE id = ? AND organization_id = ? AND status = 'pending'`, id, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "org.invite.revoke", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"invite": id}})
	})
}

type inviteRow struct {
	id, orgID int64
	orgKey    string
	email     string
}

// validInvite finds a waiting, unexpired invitation of an active organization. Every other case is the same error.
func (s *Service) validInvite(q queryer, token string) (inviteRow, error) {
	var r inviteRow
	err := q.QueryRow(`SELECT i.id, i.organization_id, o.key, i.email FROM organization_invites i JOIN organizations o ON o.id = i.organization_id
		WHERE i.token_hash = ? AND i.status = 'pending' AND i.expires_at > ? AND o.status = 'active'`, auth.HashRecoveryCode(token), s.nowMs()).
		Scan(&r.id, &r.orgID, &r.orgKey, &r.email)
	if errors.Is(err, sql.ErrNoRows) {
		return inviteRow{}, errInviteInvalid()
	}
	return r, err
}

// PreviewOrgInvite tells the token holder which organization invited which address and whether to sign in or register.
func (s *Service) PreviewOrgInvite(token string) (InvitePreview, error) {
	r, err := s.validInvite(s.db.R, token)
	if err != nil {
		return InvitePreview{}, err
	}
	p := InvitePreview{Email: r.email}
	if err := s.db.R.QueryRow(`SELECT name FROM organizations WHERE id = ?`, r.orgID).Scan(&p.Organization); err != nil {
		return InvitePreview{}, err
	}
	err = s.db.R.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE email = ?)`, r.email).Scan(&p.AccountExists)
	return p, err
}

// claimInvite spends the invitation (compare-and-set: one concurrent accept wins) and adds the membership as USER
// plus the initial project memberships. An existing membership, even as ADMIN, is never changed.
func (s *Service) claimInvite(ctx context.Context, tx *sql.Tx, r inviteRow, userID int64, a auth.Actor) error {
	now := s.nowMs()
	res, err := tx.Exec(`UPDATE organization_invites SET status = 'accepted', accepted_by = ?, accepted_at = ? WHERE id = ? AND status = 'pending' AND expires_at > ?`, userID, now, r.id, now)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errInviteInvalid()
	}
	if _, err := tx.Exec(`UPDATE users SET email_verified_at = COALESCE(email_verified_at, ?) WHERE id = ?`, now, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at, created_by) VALUES (?, ?, 'user', ?, ?)
		ON CONFLICT (organization_id, user_id) DO NOTHING`, r.orgID, userID, now, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) SELECT x.project_id, ?, 'member' FROM organization_invite_projects x
		JOIN projects p ON p.id = x.project_id AND p.organization_id = ? AND p.archived_at IS NULL WHERE x.invite_id = ?
		ON CONFLICT (project_id, user_id) DO NOTHING`, userID, r.orgID, r.id); err != nil {
		return err
	}
	return s.journal(ctx, tx, a, change{Operation: "org.invite.accept", Target: orgTarget(r.orgKey), Event: "admin.updated", Changes: map[string]any{"invite": r.id}})
}

// AcceptOrgInvite accepts as the signed-in account, whose email must be the invited address.
func (s *Service) AcceptOrgInvite(ctx context.Context, a auth.Actor, token string) (string, error) {
	if !a.Can(auth.ScopeWrite) || a.Projects != nil {
		return "", ErrForbidden
	}
	var orgKey string
	err := s.tx(func(tx *sql.Tx) error {
		st, err := s.standing(tx, a)
		if err != nil {
			return err
		}
		if !st.active {
			return ErrForbidden
		}
		r, err := s.validInvite(tx, token)
		if err != nil {
			return err
		}
		var email string
		if err := tx.QueryRow(`SELECT email FROM users WHERE id = ?`, a.UserID).Scan(&email); err != nil {
			return err
		}
		if !strings.EqualFold(email, r.email) {
			return errInviteInvalid()
		}
		orgKey = r.orgKey
		return s.claimInvite(ctx, tx, r, a.UserID, a)
	})
	return orgKey, err
}

// AcceptOrgInviteNew creates the account for the invited address (verified by the token) and joins the organization.
// An address that already has an account must sign in instead.
func (s *Service) AcceptOrgInviteNew(ctx context.Context, token, name, password string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", invalid("name is required (max 100 characters)")
	}
	if err := checkPassword(s.Policy(), password); err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	var orgKey string
	err = s.tx(func(tx *sql.Tx) error {
		r, err := s.validInvite(tx, token)
		if err != nil {
			return err
		}
		now := s.nowMs()
		res, err := tx.Exec(`INSERT INTO users (email, name, password_hash, created_at, status_changed_at) VALUES (?, ?, ?, ?, ?)`, r.email, name, hash, now, now)
		if isUnique(err) {
			return rule("an account with this email already exists: sign in to accept the invitation")
		}
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		orgKey = r.orgKey
		return s.claimInvite(ctx, tx, r, id, auth.Actor{Kind: auth.KindHuman, UserID: id})
	})
	return orgKey, err
}

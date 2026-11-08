package core

import (
	"database/sql"
	"errors"

	"github.com/kees/atlas/internal/auth"
)

// Authorization decisions (ADR-0029). The Actor carries identity only; account status and global rights are read
// from the database on every decision, so revoking or suspending applies to open sessions and tokens at once.
//
// shortcut: until the ORG 2 data migration the global-admin source is the legacy users.is_admin column and project
// rights still come from role_actions. ORG 2 switches this file to is_superadmin, organization membership and the
// single member role; no other file may decide on those columns.

// standing is the live state of the actor's account.
type standing struct{ active, super bool }

func (s *Service) standing(q queryer, a auth.Actor) (standing, error) {
	var st standing
	err := q.QueryRow(`SELECT deactivated_at IS NULL, is_admin = 1 FROM users WHERE id = ?`, a.UserID).Scan(&st.active, &st.super)
	if errors.Is(err, sql.ErrNoRows) {
		return standing{}, nil // unknown account: fail closed
	}
	return st, err
}

// IsSuperadmin reports whether the actor is an active global administrator acting through a user session.
// Any error counts as no.
func (s *Service) IsSuperadmin(a auth.Actor) bool {
	if a.Kind != auth.KindHuman {
		return false
	}
	st, err := s.standing(s.db.R, a)
	return err == nil && st.active && st.super
}

// adminOnly guards instance administration: global admins only, through a user session (never a token).
func (s *Service) adminOnly(a auth.Actor) error {
	if !s.IsSuperadmin(a) {
		return ErrForbidden
	}
	return nil
}

// canRead reports whether a user can see a project: active global admin or member.
func (s *Service) canRead(q queryer, userID int64, projectKey string) (bool, error) {
	var ok int
	err := q.QueryRow(`SELECT 1 FROM users u WHERE u.id = ? AND u.deactivated_at IS NULL AND (u.is_admin = 1 OR EXISTS (
		SELECT 1 FROM project_members m JOIN projects p ON p.id = m.project_id WHERE m.user_id = u.id AND p.key = ?))`, userID, projectKey).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// canReachProject is canRead by project id.
func canReachProject(q queryer, userID, projectID int64) (bool, error) {
	var ok int
	err := q.QueryRow(`SELECT 1 FROM users u WHERE u.id = ? AND u.deactivated_at IS NULL AND (u.is_admin = 1 OR
		EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = ? AND m.user_id = u.id))`, userID, projectID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// orgAction is what an actor wants to do inside an organization. Creating and archiving organizations is
// SUPERADMIN-only and goes through adminOnly.
type orgAction int

const (
	orgView    orgAction = iota // see the organization: any member
	orgUpdate                   // settings: organization ADMIN
	orgMembers                  // list, add, remove members, assign ADMIN: organization ADMIN
)

// authorizeOrg resolves an organization for the actor. Visibility comes first: an organization the actor does not
// belong to (or an archived one, for anyone but SUPERADMIN) is not found, never forbidden. A member without the
// needed role is forbidden.
func (s *Service) authorizeOrg(q queryer, a auth.Actor, key string, act orgAction) (Organization, error) {
	scope := auth.ScopeRead
	if act != orgView {
		scope = auth.ScopeWrite
	}
	if !a.Can(scope) || a.Projects != nil { // project-limited tokens are never organization tokens
		return Organization{}, ErrForbidden
	}
	st, err := s.standing(q, a)
	if err != nil {
		return Organization{}, err
	}
	if !st.active {
		return Organization{}, ErrForbidden
	}
	o, err := loadOrg(q, a.UserID, key)
	if err != nil {
		return Organization{}, err
	}
	if st.super {
		return o, nil
	}
	if o.Role == "" || o.Status == "archived" {
		return Organization{}, ErrNotFound
	}
	if act != orgView && o.Role != "admin" {
		return Organization{}, ErrForbidden
	}
	return o, nil
}

package core

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Authorization decisions (ADR-0029). The Actor carries identity only; account status and rights are read from
// the database on every decision, so revoking or suspending applies to open sessions and tokens at once.
//
// Levels on a project: SUPERADMIN and the ADMIN of the project's organization hold every action without any
// membership; a USER needs both a membership of the organization and a project_members row and holds
// MemberActions. A project without organization (not yet migrated) is reachable by a SUPERADMIN only.
// No other file may decide on is_superadmin or the membership tables.

// MemberActions is the fixed action set of the single project role "member" (ADR-0029). Organization ADMINs and
// SUPERADMINs hold every action in Actions. context.write is included because members hold it today.
var MemberActions = []string{"view", "export", "item.create", "item.update", "comment", "import", "time.log", "context.write"}

func memberAllows(act action) bool { return slices.Contains(MemberActions, string(act)) }

// standing is the live state of the actor's account.
type standing struct{ active, super bool }

func (s *Service) standing(q queryer, a auth.Actor) (standing, error) {
	var st standing
	err := q.QueryRow(`SELECT deactivated_at IS NULL, is_superadmin = 1 FROM users WHERE id = ?`, a.UserID).Scan(&st.active, &st.super)
	if errors.Is(err, sql.ErrNoRows) {
		return standing{}, nil // unknown account: fail closed
	}
	return st, err
}

type level int

const (
	levelNone level = iota
	levelMember
	levelAdmin
)

// projectLevel is what the user may do on a project, from the tables as they are now.
func projectLevel(q queryer, userID int64, st standing, orgID sql.NullInt64, projectID int64) (level, error) {
	if !st.active {
		return levelNone, nil
	}
	if st.super {
		return levelAdmin, nil
	}
	if !orgID.Valid {
		return levelNone, nil
	}
	var orgRole string
	var member bool
	err := q.QueryRow(`SELECT COALESCE((SELECT role FROM organization_memberships WHERE organization_id = ? AND user_id = ?), ''),
		EXISTS (SELECT 1 FROM project_members WHERE project_id = ? AND user_id = ?)`, orgID.Int64, userID, projectID, userID).Scan(&orgRole, &member)
	switch {
	case err != nil:
		return levelNone, err
	case orgRole == "admin":
		return levelAdmin, nil
	case orgRole != "" && member:
		return levelMember, nil
	}
	return levelNone, nil
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

// canRead reports whether a user can see a project by key.
func (s *Service) canRead(q queryer, userID int64, projectKey string) (bool, error) {
	var id int64
	err := q.QueryRow(`SELECT id FROM projects WHERE key = ?`, projectKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return canReachProject(q, userID, id)
}

// canReachProject reports whether a user can see a project by id (assignees, notification recipients).
func canReachProject(q queryer, userID, projectID int64) (bool, error) {
	var org sql.NullInt64
	var archived bool
	err := q.QueryRow(`SELECT p.organization_id, COALESCE(o.status = 'archived', 0) FROM projects p
		LEFT JOIN organizations o ON o.id = p.organization_id WHERE p.id = ?`, projectID).Scan(&org, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var st standing
	if err := q.QueryRow(`SELECT deactivated_at IS NULL, is_superadmin = 1 FROM users WHERE id = ?`, userID).Scan(&st.active, &st.super); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if archived && !st.super {
		return false, nil
	}
	lvl, err := projectLevel(q, userID, st, org, projectID)
	return lvl != levelNone, err
}

// sharedUsersQuery selects the users that are members of a project the actor can read (token limits included).
func (s *Service) sharedUsersQuery(a auth.Actor) (string, []any, error) {
	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return "", nil, err
	}
	if len(readable) == 0 {
		return `SELECT id, email, name, is_superadmin, avatar_path IS NOT NULL FROM users WHERE 1 = 0`, nil, nil
	}
	args := make([]any, 0, len(readable))
	for id := range readable {
		args = append(args, id)
	}
	return `SELECT id, email, name, is_superadmin, avatar_path IS NOT NULL FROM users WHERE id IN (
		SELECT user_id FROM project_members WHERE project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + `)) ORDER BY name`, args, nil
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

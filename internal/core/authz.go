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
// Levels on a project: the ADMIN of the project's organization holds every action without a project membership;
// a USER needs both a membership of the organization and a project_members row and holds MemberActions. The
// SUPERADMIN runs Atlas, not the organizations (ADR-0031): it reaches a project only through its own memberships or
// through a valid support grant of the organization (read_only: view and export; full: every action), and only in
// a user session. A project without organization is reachable by nobody.
// No other file may decide on is_superadmin, the membership tables or support grants.

// MemberActions is the fixed action set of the single project role "member" (ADR-0029). Organization ADMINs hold
// every action in Actions. context.write is included because members hold it today.
var MemberActions = []string{"view", "export", "item.create", "item.update", "comment", "import", "time.log", "context.write"}

func memberAllows(act action) bool { return slices.Contains(MemberActions, string(act)) }

// standing is the live state of the actor's account.
type standing struct {
	active, super bool
	support       bool // a SUPERADMIN user session: the only actor a support grant applies to
}

func (s *Service) standing(q queryer, a auth.Actor) (standing, error) {
	var st standing
	err := q.QueryRow(`SELECT status = 'active', is_superadmin = 1 FROM users WHERE id = ?`, a.UserID).Scan(&st.active, &st.super)
	if errors.Is(err, sql.ErrNoRows) {
		return standing{}, nil // unknown account: fail closed
	}
	st.support = st.active && st.super && a.Kind == auth.KindHuman
	return st, err
}

type level int

const (
	levelNone    level = iota
	levelSupport       // read_only support grant: view and export
	levelMember
	levelAdmin
)

// projectLevel is what the user may do on a project, from the tables as they are now, and the support grant it
// rests on (0 when it rests on a membership). now is only used for grants.
func projectLevel(q queryer, userID int64, st standing, orgID sql.NullInt64, projectID, now int64) (level, int64, error) {
	if !st.active || !orgID.Valid {
		return levelNone, 0, nil
	}
	var orgRole string
	var member bool
	err := q.QueryRow(`SELECT COALESCE((SELECT role FROM organization_memberships WHERE organization_id = ? AND user_id = ?), ''),
		EXISTS (SELECT 1 FROM project_members WHERE project_id = ? AND user_id = ?)`, orgID.Int64, userID, projectID, userID).Scan(&orgRole, &member)
	if err != nil {
		return levelNone, 0, err
	}
	lvl := levelNone
	switch {
	case orgRole == "admin":
		return levelAdmin, 0, nil
	case orgRole != "" && member:
		lvl = levelMember
	}
	if st.support {
		gid, gl, err := supportGrant(q, orgID.Int64, now)
		if err != nil {
			return levelNone, 0, err
		}
		if gid != 0 && gl > lvl {
			return gl, gid, nil
		}
	}
	return lvl, 0, nil
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

// IsActive reports whether the actor's account is active right now; long-lived streams re-check it.
func (s *Service) IsActive(a auth.Actor) bool {
	st, err := s.standing(s.db.R, a)
	return err == nil && st.active
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
	if err := q.QueryRow(`SELECT status = 'active', is_superadmin = 1 FROM users WHERE id = ?`, userID).Scan(&st.active, &st.super); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if archived {
		return false, nil
	}
	lvl, _, err := projectLevel(q, userID, st, org, projectID, 0) // no actor here: no support grant applies
	return lvl != levelNone, err
}

// visibleUsersQuery selects the people the actor may see (ADR-0027: users outside shared organizations are
// invisible). Nobody, not even a SUPERADMIN, sees people outside such a relation (the accounts list for suspension is
// ListAdminUsers). A person sees themselves, the members of the
// organizations they administer, the ADMINs of the organizations they belong to (who administer their projects)
// and everyone who shares a project with them. A token sees the members of the projects it may read.
func (s *Service) visibleUsersQuery(a auth.Actor) (string, []any, error) {
	if a.Kind == auth.KindToken {
		return s.sharedUsersQuery(a)
	}
	st, err := s.standing(s.db.R, a)
	if err != nil {
		return "", nil, err
	}
	if !st.active {
		return "", nil, ErrForbidden
	}
	const cols = `SELECT id, email, name, is_superadmin, avatar_path IS NOT NULL FROM users`
	return cols + ` WHERE id = ?
		OR id IN (SELECT m.user_id FROM organization_memberships m
			JOIN organizations o ON o.id = m.organization_id AND o.status = 'active'
			WHERE m.organization_id IN (SELECT organization_id FROM organization_memberships WHERE user_id = ? AND role = 'admin'))
		OR id IN (SELECT m.user_id FROM organization_memberships m
			JOIN organizations o ON o.id = m.organization_id AND o.status = 'active'
			WHERE m.role = 'admin' AND m.organization_id IN (SELECT organization_id FROM organization_memberships WHERE user_id = ?))
		OR id IN (SELECT user_id FROM project_members WHERE project_id IN (SELECT project_id FROM project_members WHERE user_id = ?))
		ORDER BY name`, []any{a.UserID, a.UserID, a.UserID, a.UserID}, nil
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
	orgGrants                   // support grants: an explicit ADMIN of the organization, never through a grant
)

// authorizeOrg resolves an organization for the actor. Visibility comes first: an organization the actor does not
// belong to (or an archived one) is not found, never forbidden. A member without the
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
	if o.Role == "admin" && o.Status == "active" {
		return o, nil
	}
	if st.support && act != orgGrants && o.Status == "active" {
		gid, gl, err := supportGrant(q, o.ID, s.nowMs())
		if err != nil {
			return Organization{}, err
		}
		if gid != 0 && (gl == levelAdmin || act == orgView) {
			s.noteSupport(a, o.ID, gid, supportAction(act))
			return o, nil
		}
	}
	if st.super && act == orgView {
		return o, nil // the Atlas admin lists organizations: name, key, status, admins, counts
	}
	if st.support && act == orgMembers && o.NoActiveAdmin && o.Status == "active" {
		return o, nil // the Atlas admin appoints the first ADMIN of an organization that has none
	}
	if st.super && o.Role == "" {
		return Organization{}, ErrForbidden
	}
	if o.Role == "" || o.Status == "archived" {
		return Organization{}, ErrNotFound
	}
	if act != orgView && o.Role != "admin" {
		return Organization{}, ErrForbidden
	}
	return o, nil
}

// supportAction names an organization action for the support audit: reading is throttled, changing is not.
func supportAction(act orgAction) action {
	if act == orgView {
		return actView
	}
	return actMembers
}

package core

import (
	"context"

	"github.com/kees/atlas/internal/auth"
)

// Where a person's access to a project comes from (ADR-0031). A SUPERADMIN has no standing access of its own:
// support grants are listed apart (ListSupportGrants).
const (
	SourceExplicit       = "explicit"
	SourceInheritedAdmin = "inherited_org_admin"
)

// AccessEntry is one person with access to a project, and why.
type AccessEntry struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Source string `json:"source"`
}

// ProjectAccess lists everyone who can reach the project: the explicit members and the ADMINs of its organization.
// Only those who manage members may ask (ATL-462).
func (s *Service) ProjectAccess(ctx context.Context, a auth.Actor, key string) ([]AccessEntry, error) {
	p, _, err := s.access(s.db.R, a, key, actMembers)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT u.id, u.name, u.email, CASE WHEN om.role = 'admin' THEN ? ELSE ? END
		FROM users u JOIN organization_memberships om ON om.user_id = u.id AND om.organization_id = ?
		WHERE u.status = 'active' AND (om.role = 'admin' OR EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = ? AND m.user_id = u.id))
		ORDER BY u.name`, SourceInheritedAdmin, SourceExplicit, p.OrganizationID.Int64, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessEntry{}
	for rows.Next() {
		var e AccessEntry
		if err := rows.Scan(&e.UserID, &e.Name, &e.Email, &e.Source); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type ProjectAccessItem struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

type OrgAccess struct {
	Key      string              `json:"key"`
	Name     string              `json:"name"`
	Role     string              `json:"role"`
	Projects []ProjectAccessItem `json:"projects"`
}

// EffectiveAccess says what a person can reach, computed from the same tables authz reads. With an empty orgKey it
// is the caller's own access; with an organization key the ADMIN of that organization sees a member's access in it
// (a person who is not a member answers not found). ATL-486.
func (s *Service) EffectiveAccess(ctx context.Context, a auth.Actor, orgKey string, userID int64) ([]OrgAccess, error) {
	q := s.db.R
	var orgID int64
	if orgKey == "" {
		if !a.Can(auth.ScopeRead) {
			return nil, ErrForbidden
		}
		if st, err := s.standing(q, a); err != nil {
			return nil, err
		} else if !st.active {
			return nil, ErrForbidden
		}
		userID = a.UserID
	} else {
		o, err := s.authorizeOrg(q, a, orgKey, orgMembers)
		if err != nil {
			return nil, err
		}
		orgID = o.ID
		var member bool
		if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM organization_memberships m JOIN users u ON u.id = m.user_id
			WHERE m.organization_id = ? AND m.user_id = ? AND u.status = 'active')`, orgID, userID).Scan(&member); err != nil {
			return nil, err
		}
		if !member {
			return nil, ErrNotFound
		}
	}
	rows, err := q.Query(`SELECT o.id, o.key, o.name, om.role FROM organization_memberships om
		JOIN organizations o ON o.id = om.organization_id AND o.status = 'active'
		WHERE om.user_id = ? AND (? = 0 OR o.id = ?) ORDER BY o.key`, userID, orgID, orgID)
	if err != nil {
		return nil, err
	}
	type orgRow struct {
		id int64
		OrgAccess
	}
	var orgs []orgRow
	for rows.Next() {
		var r orgRow
		if err := rows.Scan(&r.id, &r.Key, &r.Name, &r.Role); err != nil {
			rows.Close()
			return nil, err
		}
		orgs = append(orgs, r)
	}
	rows.Close()
	out := []OrgAccess{}
	for _, o := range orgs {
		source, query := SourceExplicit, `SELECT p.key, p.name FROM project_members m JOIN projects p ON p.id = m.project_id
			WHERE p.organization_id = ? AND m.user_id = ? ORDER BY p.key`
		args := []any{o.id, userID}
		if o.Role == "admin" {
			source, query, args = SourceInheritedAdmin, `SELECT key, name FROM projects WHERE organization_id = ? ORDER BY key`, []any{o.id}
		}
		ps, err := q.Query(query, args...)
		if err != nil {
			return nil, err
		}
		o.Projects = []ProjectAccessItem{}
		for ps.Next() {
			var p ProjectAccessItem
			if err := ps.Scan(&p.Key, &p.Name); err != nil {
				ps.Close()
				return nil, err
			}
			if p.Source = source; orgKey != "" || a.CanProject(p.Key) { // a project-limited token sees only its projects
				o.Projects = append(o.Projects, p)
			}
		}
		ps.Close()
		out = append(out, o.OrgAccess)
	}
	return out, nil
}

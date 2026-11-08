package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Organization groups projects and people (ADR-0027). Role is the caller's own membership role ("" for none).
type Organization struct {
	ID        int64  `json:"-"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	Role      string `json:"role,omitempty"`
	// NoActiveAdmin flags an organization left without an active ADMIN (only possible through allow_orphan).
	NoActiveAdmin bool `json:"no_active_admin"`
}

type OrgMember struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

const orgSelect = `SELECT o.id, o.key, o.name, o.status, o.created_at, COALESCE(m.role, ''),
	NOT EXISTS (SELECT 1 FROM organization_memberships x JOIN users u ON u.id = x.user_id
		WHERE x.organization_id = o.id AND x.role = 'admin' AND u.deactivated_at IS NULL)
	FROM organizations o LEFT JOIN organization_memberships m ON m.organization_id = o.id AND m.user_id = ?`

func scanOrg(sc interface{ Scan(...any) error }) (Organization, error) {
	var o Organization
	err := sc.Scan(&o.ID, &o.Key, &o.Name, &o.Status, &o.CreatedAt, &o.Role, &o.NoActiveAdmin)
	return o, err
}

func loadOrg(q queryer, userID int64, key string) (Organization, error) {
	o, err := scanOrg(q.QueryRow(orgSelect+` WHERE o.key = ?`, userID, strings.ToUpper(strings.TrimSpace(key))))
	if errors.Is(err, sql.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	return o, err
}

func cleanOrg(key, name string) (string, string, error) {
	key, name = strings.ToUpper(strings.TrimSpace(key)), strings.TrimSpace(name)
	if !projectKeyRe.MatchString(key) {
		return "", "", invalid("key must be 2-10 characters: letters and digits, starting with a letter")
	}
	if name == "" {
		return "", "", invalid("name is required")
	}
	return key, name, nil
}

// CreateOrganization makes an organization with its first ADMIN (ADR-0027); adminID 0 means the creating SUPERADMIN.
func (s *Service) CreateOrganization(ctx context.Context, a auth.Actor, key, name string, adminID int64) (Organization, error) {
	if err := s.adminOnly(a); err != nil {
		return Organization{}, err
	}
	key, name, err := cleanOrg(key, name)
	if err != nil {
		return Organization{}, err
	}
	if adminID == 0 {
		adminID = a.UserID
	}
	var out Organization
	err = s.tx(func(tx *sql.Tx) error {
		var active bool
		if err := tx.QueryRow(`SELECT deactivated_at IS NULL FROM users WHERE id = ?`, adminID).Scan(&active); errors.Is(err, sql.ErrNoRows) || (err == nil && !active) {
			return invalid("the first admin must be an active user")
		} else if err != nil {
			return err
		}
		now := s.nowMs()
		res, err := tx.Exec(`INSERT INTO organizations (key, name, created_at, created_by) VALUES (?, ?, ?, ?)`, key, name, now, a.UserID)
		if isUnique(err) {
			return rule("organization %s already exists", key)
		}
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at, created_by) VALUES (?, ?, 'admin', ?, ?)`,
			id, adminID, now, a.UserID); err != nil {
			return err
		}
		if out, err = loadOrg(tx, a.UserID, key); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.create", Target: orgTarget(key), Event: "admin.updated",
			Changes: map[string]any{"name": name, "admin": adminID}})
	})
	return out, err
}

func orgTarget(key string) string { return "org:" + key }

// ListOrganizations returns every organization to a SUPERADMIN and the caller's own active ones to everyone else.
func (s *Service) ListOrganizations(ctx context.Context, a auth.Actor) ([]Organization, error) {
	if !a.Can(auth.ScopeRead) || a.Projects != nil {
		return nil, ErrForbidden
	}
	st, err := s.standing(s.db.R, a)
	if err != nil {
		return nil, err
	}
	if !st.active {
		return nil, ErrForbidden
	}
	where := ` WHERE m.user_id IS NOT NULL AND o.status = 'active'`
	if st.super {
		where = ``
	}
	rows, err := s.db.R.Query(orgSelect+where+` ORDER BY o.key`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Organization
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Service) GetOrganization(ctx context.Context, a auth.Actor, key string) (Organization, error) {
	return s.authorizeOrg(s.db.R, a, key, orgView)
}

// UpdateOrganization renames an organization (organization ADMIN or SUPERADMIN).
func (s *Service) UpdateOrganization(ctx context.Context, a auth.Actor, key, name string) (Organization, error) {
	var out Organization
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, key, orgUpdate)
		if err != nil {
			return err
		}
		_, name, err := cleanOrg(o.Key, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE organizations SET name = ? WHERE id = ?`, name, o.ID); err != nil {
			return err
		}
		o.Name = name
		out = o
		return s.journal(ctx, tx, a, change{Operation: "org.update", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"name": name}})
	})
	return out, err
}

// SetOrganizationArchived archives or restores an organization (SUPERADMIN). Its projects are hidden from everyone
// else while it is archived.
func (s *Service) SetOrganizationArchived(ctx context.Context, a auth.Actor, key string, archived bool) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		o, err := loadOrg(tx, a.UserID, key)
		if err != nil {
			return err
		}
		status, op, at := "active", "org.restore", any(nil)
		if archived {
			status, op, at = "archived", "org.archive", s.nowMs()
		}
		if _, err := tx.Exec(`UPDATE organizations SET status = ?, archived_at = ? WHERE id = ?`, status, at, o.ID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: op, Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{}})
	})
}

func (s *Service) ListOrgMembers(ctx context.Context, a auth.Actor, key string) ([]OrgMember, error) {
	o, err := s.authorizeOrg(s.db.R, a, key, orgMembers)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT u.id, u.email, u.name, m.role, u.deactivated_at IS NULL FROM organization_memberships m
		JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? ORDER BY u.name`, o.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgMember
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.Active); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetOrgMember adds a user to an organization or changes the role (admin|user). allowOrphan lets a SUPERADMIN
// demote the last active ADMIN; everyone else is refused.
func (s *Service) SetOrgMember(ctx context.Context, a auth.Actor, key string, userID int64, role string, allowOrphan bool) error {
	if role != "admin" && role != "user" {
		return invalid("role must be admin or user")
	}
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, key, orgMembers)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return invalid("unknown user")
		}
		if role != "admin" {
			if err := s.guardLastOrgAdmin(tx, a, o, userID, allowOrphan); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at, created_by) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (organization_id, user_id) DO UPDATE SET role = excluded.role`, o.ID, userID, role, s.nowMs(), a.UserID); err != nil {
			return err
		}
		if role == "user" { // a demoted ADMIN keeps only the projects they are a member of
			if err := s.revokeProjectTokensInOrg(tx, o.ID, userID, true); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "org.member.set", Target: orgTarget(o.Key), Event: "admin.updated",
			Changes: map[string]any{"user_id": userID, "role": role}})
	})
}

// RemoveOrgMember removes a user from an organization and from every project of it, in one transaction.
func (s *Service) RemoveOrgMember(ctx context.Context, a auth.Actor, key string, userID int64, allowOrphan bool) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, key, orgMembers)
		if err != nil {
			return err
		}
		if err := s.guardLastOrgAdmin(tx, a, o, userID, allowOrphan); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM organization_memberships WHERE organization_id = ? AND user_id = ?`, o.ID, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if err := s.revokeProjectTokensInOrg(tx, o.ID, userID, false); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_members WHERE user_id = ? AND project_id IN (SELECT id FROM projects WHERE organization_id = ?)`,
			userID, o.ID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.member.remove", Target: orgTarget(o.Key), Event: "admin.updated",
			Changes: map[string]any{"user_id": userID}})
	})
}

// guardLastOrgAdmin keeps every active organization with one ACTIVE ADMIN (ADR-0029). It must run in the
// transaction that changes the membership. allowOrphan is honoured only for a SUPERADMIN.
func (s *Service) guardLastOrgAdmin(tx *sql.Tx, a auth.Actor, o Organization, userID int64, allowOrphan bool) error {
	if allowOrphan {
		if st, err := s.standing(tx, a); err != nil {
			return err
		} else if !st.super || a.Kind != auth.KindHuman {
			return ErrForbidden
		}
	}
	if o.Status != "active" {
		return nil
	}
	last, err := isLastOrgAdmin(tx, o.ID, userID)
	if err != nil {
		return err
	}
	if last && !allowOrphan {
		return rule("an organization needs at least one active admin")
	}
	return nil
}

// isLastOrgAdmin reports whether the user is the only active ADMIN of the organization.
func isLastOrgAdmin(q queryer, orgID, userID int64) (bool, error) {
	var mine bool
	err := q.QueryRow(`SELECT m.role = 'admin' AND u.deactivated_at IS NULL FROM organization_memberships m
		JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.user_id = ?`, orgID, userID).Scan(&mine)
	if errors.Is(err, sql.ErrNoRows) || !mine {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var others int
	err = q.QueryRow(`SELECT COUNT(*) FROM organization_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = ? AND m.role = 'admin' AND u.deactivated_at IS NULL AND m.user_id <> ?`, orgID, userID).Scan(&others)
	return others == 0, err
}

// guardOrgAdminLoss refuses to suspend or delete an account that is the only active ADMIN of an active organization.
func guardOrgAdminLoss(q queryer, userID int64) error {
	rows, err := q.Query(`SELECT o.id FROM organization_memberships m JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = ? AND m.role = 'admin' AND o.status = 'active'`, userID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if last, err := isLastOrgAdmin(q, id, userID); err != nil {
			return err
		} else if last {
			return rule("this account is the only active admin of an organization; assign another admin first")
		}
	}
	return nil
}

// projectOrganization picks the organization a new project goes into and checks the actor may create it there.
func (s *Service) projectOrganization(tx *sql.Tx, a auth.Actor, orgKey string) (Organization, error) {
	if orgKey != "" {
		return s.authorizeOrg(tx, a, orgKey, orgUpdate)
	}
	st, err := s.standing(tx, a)
	if err != nil {
		return Organization{}, err
	}
	if !st.active || a.Projects != nil {
		return Organization{}, ErrForbidden
	}
	q := `SELECT o.key FROM organizations o WHERE o.status = 'active'`
	args := []any{}
	if !st.super {
		q += ` AND EXISTS (SELECT 1 FROM organization_memberships m WHERE m.organization_id = o.id AND m.user_id = ? AND m.role = 'admin')`
		args = append(args, a.UserID)
	}
	rows, err := tx.Query(q+` ORDER BY o.key`, args...)
	if err != nil {
		return Organization{}, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return Organization{}, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	switch len(keys) {
	case 0:
		return Organization{}, ErrForbidden // only a SUPERADMIN or an organization ADMIN creates projects
	case 1:
		return s.authorizeOrg(tx, a, keys[0], orgUpdate)
	}
	return Organization{}, invalid("several organizations: say which one the project belongs to")
}

// revokeProjectTokensInOrg revokes the user's project tokens for the projects of an organization; with onlyWithoutRow
// it spares projects the user is still an explicit member of.
func (s *Service) revokeProjectTokensInOrg(tx *sql.Tx, orgID, userID int64, onlyWithoutRow bool) error {
	q := `SELECT p.key FROM projects p WHERE p.organization_id = ?`
	if onlyWithoutRow {
		q += ` AND NOT EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = ?)`
	}
	args := []any{orgID}
	if onlyWithoutRow {
		args = append(args, userID)
	}
	rows, err := tx.Query(q, args...)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, k := range keys {
		if err := s.revokeOwnedProjectTokens(tx, userID, k); err != nil {
			return err
		}
	}
	return nil
}

// BootstrapOrganization creates the first organization of a new installation with the first SUPERADMIN as its ADMIN.
// It does nothing when an organization exists.
func (s *Service) BootstrapOrganization(ctx context.Context, superadminEmail, key, name string) error {
	var n int
	if err := s.db.W.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&n); err != nil || n > 0 {
		return err
	}
	var id int64
	if err := s.db.W.QueryRow(`SELECT id FROM users WHERE email = ? AND is_superadmin = 1`, superadminEmail).Scan(&id); err != nil {
		return err
	}
	_, err := s.CreateOrganization(ctx, auth.Actor{Kind: auth.KindHuman, UserID: id}, key, name, 0)
	return err
}

// UnmigratedReason says why this database still has to go through `atlas migrate-orgs` (ADR-0028), or "" when it
// does not. The server refuses to start on such a database: nobody would have rights.
func (s *Service) UnmigratedReason() (string, error) {
	var noOrg, users, supers int
	if err := s.db.R.QueryRow(`SELECT (SELECT COUNT(*) FROM projects WHERE organization_id IS NULL),
		(SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM users WHERE is_superadmin = 1)`).Scan(&noOrg, &users, &supers); err != nil {
		return "", err
	}
	switch {
	case noOrg > 0:
		return fmt.Sprintf("%d project(s) belong to no organization", noOrg), nil
	case users > 0 && supers == 0:
		return "no account is SUPERADMIN", nil
	}
	return "", nil
}

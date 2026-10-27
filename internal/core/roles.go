package core

import (
	"context"
	"database/sql"
	"regexp"
	"slices"

	"github.com/kees/atlas/internal/auth"
)

// action is one thing a project role may be allowed to do. Every project check names one.
type action string

const (
	actView          action = "view"
	actExport        action = "export"
	actItemCreate    action = "item.create"
	actItemUpdate    action = "item.update"
	actComment       action = "comment"
	actImport        action = "import"
	actWorkflow      action = "workflow"
	actProjectUpdate action = "project.update"
	actMembers       action = "members"
	actTokens        action = "tokens"
)

// Actions lists every action in display order.
var Actions = []string{"view", "export", "item.create", "item.update", "comment", "import", "workflow", "project.update", "members", "tokens"}

// Role is a project role and the actions it grants. admin is built in and fixed: it always holds every
// action, so a project can never be locked out. viewer and member are built in and editable.
type Role struct {
	Name    string   `json:"name"`
	Builtin bool     `json:"builtin"`
	Actions []string `json:"actions"`
}

var roleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func (s *Service) Roles(ctx context.Context, a auth.Actor) ([]Role, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	return s.roles(s.db.R)
}

func (s *Service) roles(q queryer) ([]Role, error) {
	rows, err := q.Query(`SELECT r.name, r.builtin, a.action FROM roles r LEFT JOIN role_actions a ON a.role = r.name
		ORDER BY r.builtin DESC, r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var name string
		var builtin bool
		var act sql.NullString
		if err := rows.Scan(&name, &builtin, &act); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, Role{Name: name, Builtin: builtin, Actions: []string{}})
		}
		if act.Valid {
			out[len(out)-1].Actions = append(out[len(out)-1].Actions, act.String)
		}
	}
	for i := range out {
		slices.SortFunc(out[i].Actions, func(x, y string) int { return slices.Index(Actions, x) - slices.Index(Actions, y) })
	}
	return out, rows.Err()
}

// SaveRole creates a custom role or replaces the actions of viewer, member or a custom role (admins only).
func (s *Service) SaveRole(ctx context.Context, a auth.Actor, name string, actions []string) (Role, error) {
	if err := adminOnly(a); err != nil {
		return Role{}, err
	}
	if !roleNameRe.MatchString(name) {
		return Role{}, invalid("role name: lowercase letters, digits, - and _, starting with a letter, at most 32")
	}
	if name == "admin" {
		return Role{}, rule("the admin role is fixed")
	}
	for _, act := range actions {
		if !slices.Contains(Actions, act) {
			return Role{}, invalid("unknown action %s", act)
		}
	}
	set := []string{}
	for _, act := range Actions { // canonical order, no duplicates
		if slices.Contains(actions, act) {
			set = append(set, act)
		}
	}
	if !slices.Contains(set, "view") {
		return Role{}, invalid("every role must be able to view")
	}
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO roles (name) VALUES (?)`, name); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM role_actions WHERE role = ?`, name); err != nil {
			return err
		}
		for _, act := range set {
			if _, err := tx.Exec(`INSERT INTO role_actions (role, action) VALUES (?, ?)`, name, act); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "role.save", Target: "role-" + name, Event: "admin.updated",
			Changes: map[string]any{"actions": set}})
	})
	return Role{Name: name, Actions: set}, err
}

// DeleteRole removes a custom role that nobody holds (admins only).
func (s *Service) DeleteRole(ctx context.Context, a auth.Actor, name string) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		var builtin bool
		if err := tx.QueryRow(`SELECT builtin FROM roles WHERE name = ?`, name).Scan(&builtin); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if builtin {
			return rule("built-in roles cannot be deleted")
		}
		if loadPolicy(tx).DefaultRole == name {
			return rule("role is the default role for new members")
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM project_members WHERE role = ?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return rule("role is still assigned to project members")
		}
		if _, err := tx.Exec(`DELETE FROM roles WHERE name = ?`, name); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "role.delete", Target: "role-" + name, Event: "admin.updated"})
	})
}

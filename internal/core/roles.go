package core

import (
	"context"
	"database/sql"
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
	actTimeLog       action = "time.log"
	actContextWrite  action = "context.write"
	actContextVerify action = "context.verify"
)

// Actions lists every action in display order.
var Actions = []string{"view", "export", "item.create", "item.update", "comment", "import", "workflow", "project.update", "members", "tokens", "time.log", "context.write", "context.verify"}

// Role is a project role and the actions it grants. admin is built in and fixed: it always holds every
// action, so a project can never be locked out. viewer and member are built in and editable.
type Role struct {
	Name    string   `json:"name"`
	Builtin bool     `json:"builtin"`
	Actions []string `json:"actions"`
}

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

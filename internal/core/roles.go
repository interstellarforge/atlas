package core

import (
	"context"
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

// Role describes a project role for display. There is exactly one (ADR-0029): management comes from being a
// SUPERADMIN or the ADMIN of the project's organization, not from a project role.
type Role struct {
	Name    string   `json:"name"`
	Builtin bool     `json:"builtin"`
	Actions []string `json:"actions"`
}

func (s *Service) Roles(ctx context.Context, a auth.Actor) ([]Role, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	return []Role{{Name: "member", Builtin: true, Actions: slices.Clone(MemberActions)}}, nil
}

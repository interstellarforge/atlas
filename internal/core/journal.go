package core

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/kees/atlas/internal/auth"
)

// change describes one mutation for the audit log and the sync journal.
type change struct {
	Operation string // e.g. item.update
	Target    string // item key or project key
	Event     string // e.g. item.updated
	Project   string
	ItemKey   string
	Changes   any
}

// journal writes the audit entry and the change event inside the caller's
// transaction, so a mutation and its record commit or roll back together.
// Events are only announced to clients after the commit.
func (s *Service) journal(ctx context.Context, tx *sql.Tx, a auth.Actor, c change) error {
	body, err := json.Marshal(c.Changes)
	if err != nil {
		return err
	}
	now := s.nowMs()
	if _, err := tx.Exec(`INSERT INTO audit_entries (actor, operation, target, changes, batch_id, ts)
		VALUES (?, ?, ?, ?, ?, ?)`, a.String(), c.Operation, c.Target, string(body), batchFrom(ctx), now); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO change_events (type, project, item_key, actor, ts) VALUES (?, ?, ?, ?, ?)`,
		c.Event, c.Project, c.ItemKey, a.String(), now)
	return err
}

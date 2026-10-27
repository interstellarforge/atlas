package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const (
	maxChecklistText  = 500
	maxChecklistItems = 100
)

type ChecklistItem struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

func (s *Service) ListChecklist(ctx context.Context, a auth.Actor, key string) ([]ChecklistItem, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.access(s.db.R, a, pk, roleViewer); err != nil {
		return nil, err
	}
	return checklist(s.db.R, key)
}

func checklist(q queryer, key string) ([]ChecklistItem, error) {
	rows, err := q.Query(`SELECT c.id, c.text, c.done FROM checklist_items c JOIN work_items i ON i.id = c.item_id
		WHERE i.key = ? ORDER BY c.position, c.id`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var c ChecklistItem
		if err := rows.Scan(&c.ID, &c.Text, &c.Done); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChecklistPatch changes one checklist item; nil fields stay as they are.
type ChecklistPatch struct {
	Text *string
	Done *bool
}

// AddChecklistItem appends an item. Checklist changes do not bump the work item's version: items are
// edited one by one, so two people ticking different boxes never conflict.
func (s *Service) AddChecklistItem(ctx context.Context, a auth.Actor, key, text string) (ChecklistItem, error) {
	text, err := checklistText(text)
	if err != nil {
		return ChecklistItem{}, err
	}
	var out ChecklistItem
	err = s.checklistTx(ctx, a, key, "checklist.add", func(tx *sql.Tx, itemID int64) (any, error) {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM checklist_items WHERE item_id = ?`, itemID).Scan(&n); err != nil {
			return nil, err
		}
		if n >= maxChecklistItems {
			return nil, rule("a checklist holds at most %d items", maxChecklistItems)
		}
		res, err := tx.Exec(`INSERT INTO checklist_items (item_id, text, done, position)
			VALUES (?, ?, 0, COALESCE((SELECT MAX(position) FROM checklist_items WHERE item_id = ?), 0) + 1)`, itemID, text, itemID)
		if err != nil {
			return nil, err
		}
		out = ChecklistItem{Text: text}
		out.ID, _ = res.LastInsertId()
		return map[string]any{"id": out.ID, "text": text}, nil
	})
	return out, err
}

func (s *Service) UpdateChecklistItem(ctx context.Context, a auth.Actor, key string, id int64, p ChecklistPatch) (ChecklistItem, error) {
	if p.Text == nil && p.Done == nil {
		return ChecklistItem{}, invalid("no fields to update")
	}
	var out ChecklistItem
	err := s.checklistTx(ctx, a, key, "checklist.update", func(tx *sql.Tx, itemID int64) (any, error) {
		err := tx.QueryRow(`SELECT id, text, done FROM checklist_items WHERE id = ? AND item_id = ?`, id, itemID).Scan(&out.ID, &out.Text, &out.Done)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if p.Text != nil {
			t, err := checklistText(*p.Text)
			if err != nil {
				return nil, err
			}
			out.Text = t
		}
		if p.Done != nil {
			out.Done = *p.Done
		}
		if _, err := tx.Exec(`UPDATE checklist_items SET text = ?, done = ? WHERE id = ?`, out.Text, out.Done, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "text": out.Text, "done": out.Done}, nil
	})
	return out, err
}

func (s *Service) RemoveChecklistItem(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.checklistTx(ctx, a, key, "checklist.remove", func(tx *sql.Tx, itemID int64) (any, error) {
		res, err := tx.Exec(`DELETE FROM checklist_items WHERE id = ? AND item_id = ?`, id, itemID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
		return map[string]any{"id": id}, nil
	})
}

// checklistTx runs one checklist mutation with access check, audit entry and change event.
func (s *Service) checklistTx(ctx context.Context, a auth.Actor, key, op string, f func(tx *sql.Tx, itemID int64) (any, error)) error {
	pk, err := projectOfKey(key)
	if err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if _, _, err := s.access(tx, a, pk, roleMember); err != nil {
			return err
		}
		ref, err := refOf(tx, key)
		if err != nil {
			return err
		}
		changes, err := f(tx, ref.id)
		if err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: op, Target: key, Event: "checklist.changed", Project: pk, ItemKey: key, Changes: changes})
	})
}

func checklistText(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" || len(t) > maxChecklistText {
		return "", invalid("checklist text is required (max %d characters)", maxChecklistText)
	}
	return t, nil
}

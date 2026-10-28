package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/kees/atlas/internal/auth"
)

// Undo of agent batches (ATL-74, ADR-0024). Batch records, per applied operation, the fields it changed with
// their values before and after. UndoBatch restores the "before" values, field by field, but only where the
// item still holds the "after" value: a later edit is a conflict and is left alone unless force is set.
// shortcut: board rank is not restored (a moved item returns to the old status at the end of its column), and
// subitems of a created tree are archived without a conflict check; upgrade when that matters.

// snapshot is the set of item fields an undo can restore.
func snapshot(it Item) map[string]any {
	labels := slices.Clone(it.Labels)
	slices.Sort(labels)
	return map[string]any{
		"title": it.Title, "description": it.Description, "status": it.Status, "priority": it.Priority,
		"assignee": it.Assignee, "due_at": it.DueAt, "labels": labels, "parent": it.Parent, "custom_data": it.CustomData,
		"milestone": it.Milestone, "sprint": it.Sprint, "archived": it.ArchivedAt != nil,
	}
}

func jsonEq(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// recordInverse stores what an applied batch operation changed. before is the item before an update; the
// zero Item for a create.
func (s *Service) recordInverse(tx *sql.Tx, a auth.Actor, batchID, kind string, before, after Item) error {
	b, af := map[string]any{}, map[string]any{}
	if kind == "update" {
		bs, as := snapshot(before), snapshot(after)
		for k, v := range as {
			if !jsonEq(bs[k], v) {
				b[k], af[k] = bs[k], v
			}
		}
		if len(af) == 0 {
			return nil // a move inside a column changes only the rank
		}
	}
	bj, _ := json.Marshal(b)
	aj, _ := json.Marshal(af)
	sub, _ := json.Marshal(append([]string{}, after.Subitems...))
	_, err := tx.Exec(`INSERT INTO batch_inverses (batch_id, project, item_key, kind, before, after, subitems, after_version, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, batchID, after.Project, after.Key, kind, string(bj), string(aj), string(sub), after.Version, a.String(), s.nowMs())
	return err
}

type BatchInfo struct {
	BatchID   string `json:"batch_id"`
	Actor     string `json:"actor"`
	CreatedAt int64  `json:"created_at"`
	Items     int    `json:"items"`
	Undone    bool   `json:"undone"`
}

// ListBatches lists the latest undoable batches of a project (project admin).
func (s *Service) ListBatches(ctx context.Context, a auth.Actor, key string, limit int) ([]BatchInfo, error) {
	if _, _, err := s.access(s.db.R, a, key, actProjectUpdate); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.R.Query(`SELECT i.batch_id, MIN(i.actor), MIN(i.created_at), COUNT(*), EXISTS (SELECT 1 FROM batch_undos u WHERE u.batch_id = i.batch_id)
		FROM batch_inverses i WHERE i.project = ? GROUP BY i.batch_id ORDER BY MAX(i.id) DESC LIMIT ?`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchInfo{}
	for rows.Next() {
		var b BatchInfo
		if err := rows.Scan(&b.BatchID, &b.Actor, &b.CreatedAt, &b.Items, &b.Undone); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type UndoConflict struct {
	Item   string   `json:"item"`
	Fields []string `json:"fields"` // "*" = the whole created item
}

type UndoResult struct {
	BatchID   string         `json:"batch_id"`
	Restored  []string       `json:"restored"`  // item keys that were changed back
	Conflicts []UndoConflict `json:"conflicts"` // left as they are: edited after the batch (use force to override)
}

// PruneBatchInverses drops undo records older than the cutoff (unix ms).
func (s *Service) PruneBatchInverses(cutoffMs int64) {
	for _, q := range []string{`DELETE FROM batch_inverses WHERE created_at < ?`, `DELETE FROM batch_undos WHERE undone_at < ?`} {
		if _, err := s.db.W.Exec(q, cutoffMs); err != nil {
			slog.Error("prune batch undo records", "error", err)
		}
	}
}

// UndoBatch reverses a batch in one transaction, newest operation first, as the calling project admin.
// A batch can be undone once.
func (s *Service) UndoBatch(ctx context.Context, a auth.Actor, batchID string, force bool) (UndoResult, error) {
	res := UndoResult{BatchID: batchID, Restored: []string{}, Conflicts: []UndoConflict{}}
	_, err := s.Idempotent(ctx, a, "", nil, nil, func(ctx context.Context, bs *Service) error {
		tx := bs.outer
		rows, err := tx.Query(`SELECT project, item_key, kind, before, after, subitems, after_version FROM batch_inverses WHERE batch_id = ? ORDER BY id DESC`, batchID)
		if err != nil {
			return err
		}
		type rec struct {
			project, key, kind, before, after, sub string
			version                                int64
		}
		var recs []rec
		for rows.Next() {
			var r rec
			if err := rows.Scan(&r.project, &r.key, &r.kind, &r.before, &r.after, &r.sub, &r.version); err != nil {
				rows.Close()
				return err
			}
			recs = append(recs, r)
		}
		rows.Close()
		if len(recs) == 0 {
			return ErrNotFound
		}
		checked := map[string]bool{}
		for _, r := range recs {
			if !checked[r.project] {
				if _, _, err := bs.access(tx, a, r.project, actProjectUpdate); err != nil {
					return err
				}
				checked[r.project] = true
			}
		}
		if _, err := tx.Exec(`INSERT INTO batch_undos (batch_id, undone_at, undone_by) VALUES (?, ?, ?)`, batchID, bs.nowMs(), a.String()); err != nil {
			return rule("batch %s was already undone", batchID)
		}
		ctx = WithBatch(ctx, "undo-"+batchID)
		for _, r := range recs {
			cur, err := loadRow(tx, r.key)
			if err != nil {
				return err
			}
			if r.kind == "create" {
				if cur.ArchivedAt != nil {
					continue
				}
				if cur.Version != r.version && !force {
					res.Conflicts = append(res.Conflicts, UndoConflict{Item: r.key, Fields: []string{"*"}})
					continue
				}
				var sub []string
				_ = json.Unmarshal([]byte(r.sub), &sub)
				for _, k := range append([]string{r.key}, sub...) {
					c, err := loadRow(tx, k)
					if err != nil {
						return err
					}
					if c.ArchivedAt != nil {
						continue
					}
					if _, err := bs.UpdateItem(ctx, a, k, c.Version, map[string]any{"archived": true}); err != nil {
						return err
					}
					res.Restored = append(res.Restored, k)
				}
				continue
			}
			var before, after map[string]any
			_ = json.Unmarshal([]byte(r.before), &before)
			_ = json.Unmarshal([]byte(r.after), &after)
			now := snapshot(cur.Item)
			patch := map[string]any{}
			var conflicted []string
			for f, want := range after {
				if !force && !jsonEq(now[f], want) {
					conflicted = append(conflicted, f)
					continue
				}
				patch[f] = restoreValue(f, before[f], now[f])
			}
			if len(conflicted) > 0 {
				slices.Sort(conflicted)
				res.Conflicts = append(res.Conflicts, UndoConflict{Item: r.key, Fields: conflicted})
			}
			if len(patch) == 0 {
				continue
			}
			if _, err := bs.UpdateItem(ctx, a, r.key, cur.Version, patch); err != nil {
				return err
			}
			res.Restored = append(res.Restored, r.key)
		}
		return nil
	})
	return res, err
}

// restoreValue turns a stored "before" value into the form UpdateItem takes. custom_data is merged by
// UpdateItem, so keys the batch added are removed with null.
func restoreValue(field string, before, current any) any {
	if field != "custom_data" {
		return before
	}
	out := map[string]any{}
	if cur, ok := current.(map[string]any); ok {
		for k := range cur {
			out[k] = nil
		}
	}
	if b, ok := before.(map[string]any); ok {
		for k, v := range b {
			out[k] = v
		}
	}
	return out
}

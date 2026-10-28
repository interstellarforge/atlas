package core

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// setRequirements replaces the statuses an item must have been in before it may enter statusID.
func setRequirements(tx *sql.Tx, projectID, statusID int64, v any) error {
	raw, ok := v.([]any)
	if !ok {
		if names, isStrings := v.([]string); isStrings {
			for _, n := range names {
				raw = append(raw, n)
			}
		} else {
			return invalid("requires must be an array of status names")
		}
	}
	ids := make([]int64, 0, len(raw))
	for _, x := range raw {
		name, ok := x.(string)
		if !ok {
			return invalid("requires must be an array of status names")
		}
		id, err := resolveStatus(tx, projectID, name)
		if err != nil {
			return err
		}
		if id == statusID {
			return invalid("a status cannot require itself")
		}
		ids = append(ids, id)
	}
	if _, err := tx.Exec(`DELETE FROM status_requirements WHERE status_id = ?`, statusID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO status_requirements (status_id, required_status_id) VALUES (?, ?)`, statusID, id); err != nil {
			return err
		}
	}
	return nil
}

// missingSteps lists the required statuses of target that the item has neither visited nor is in now.
// itemID 0 means the item does not exist yet: nothing is visited.
func missingSteps(q queryer, itemID, currentStatusID, targetStatusID int64) ([]string, error) {
	rows, err := q.Query(`SELECT s.name FROM status_requirements r JOIN project_statuses s ON s.id = r.required_status_id
		WHERE r.status_id = ? AND r.required_status_id <> ?
		AND NOT EXISTS (SELECT 1 FROM item_status_visits v WHERE v.item_id = ? AND v.status_id = r.required_status_id)
		ORDER BY s.position, s.id`, targetStatusID, currentStatusID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// checkRequiredSteps refuses a status change that skips a required step. Creating (itemID 0) counts as skipping all.
func checkRequiredSteps(q queryer, itemID, currentStatusID, targetStatusID int64) error {
	if itemID != 0 && currentStatusID == targetStatusID {
		return nil
	}
	missing, err := missingSteps(q, itemID, currentStatusID, targetStatusID)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		var targetName string
		if err := q.QueryRow(`SELECT name FROM project_statuses WHERE id = ?`, targetStatusID).Scan(&targetName); err != nil {
			return err
		}
		return rule("status %q requires the item to have been in %s first", targetName, strings.Join(missing, ", "))
	}
	return nil
}

func recordVisit(tx *sql.Tx, itemID, statusID int64) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO item_status_visits (item_id, status_id) VALUES (?, ?)`, itemID, statusID)
	return err
}

// allCategories are the status categories, in board order.
var allCategories = []string{"todo", "active", "review", "completed"}

// SetMergeColumns merges every category (on) or none (ADR-0011).
func (s *Service) SetMergeColumns(ctx context.Context, a auth.Actor, key string, on bool) error {
	if on {
		return s.SetMergeCategories(ctx, a, key, allCategories)
	}
	return s.SetMergeCategories(ctx, a, key, nil)
}

// SetMergeCategories chooses the status categories whose statuses share one board column (ADR-0020).
func (s *Service) SetMergeCategories(ctx context.Context, a auth.Actor, key string, categories []string) error {
	for _, c := range categories {
		if !slices.Contains(allCategories, c) {
			return invalid("unknown category %q", c)
		}
	}
	// Stored in board order so the same choice always reads the same.
	var keep []string
	for _, c := range allCategories {
		if slices.Contains(categories, c) {
			keep = append(keep, c)
		}
	}
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE projects SET merge_categories = ?, merge_columns = ? WHERE id = ?`, strings.Join(keep, ","), len(keep) > 0, p.ID); err != nil {
			return err
		}
		if keep == nil {
			keep = []string{}
		}
		// project.updated makes clients refetch the workflow and the board.
		return s.journal(ctx, tx, a, change{Operation: "workflow.merge_columns", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"merge_categories": keep}})
	})
}

package core

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// subtreeRange gives the half-open range [lo, hi) that covers a path and all descendants:
// '/' is followed by '0' in ASCII, so every descendant sorts below prefix-with-'0'.
// A range scan can use the path index; SQLite's default case-insensitive LIKE cannot.
func subtreeRange(path string) (lo, hi string) {
	return path, strings.TrimSuffix(path, "/") + "0"
}

// reparent moves an item (and implicitly its subtree) under a new parent key, or to the root when
// parent is nil. Descendant paths are rewritten in the caller's transaction.
func (s *Service) reparent(tx *sql.Tx, cur itemRow, parent any) error {
	newBase := "/"
	var newParentID any
	if parent != nil {
		pk, ok := parent.(string)
		if !ok {
			return invalid("parent must be an item key or null")
		}
		par, err := loadRow(tx, pk)
		if err != nil || par.projectID != cur.projectID {
			return invalid("parent %s not found in project %s", pk, cur.Project)
		}
		if !slices.Contains(allowedParents[cur.Type], par.Type) {
			return rule("a %s cannot be placed under a %s", cur.Type, par.Type)
		}
		if strings.HasPrefix(par.path, cur.path) {
			return rule("an item cannot be moved under itself or its descendants")
		}
		newBase, newParentID = par.path, par.id
	}

	lo, hi := subtreeRange(cur.path)
	var deepest int
	if err := tx.QueryRow(`SELECT MAX(length(path) - length(replace(path, '/', ''))) FROM work_items WHERE path >= ? AND path < ?`,
		lo, hi).Scan(&deepest); err != nil {
		return err
	}
	height := deepest - strings.Count(cur.path, "/") // levels below the moved item
	if strings.Count(newBase, "/")+height > maxDepth {
		return rule("hierarchy deeper than %d levels", maxDepth)
	}

	newPath := fmt.Sprintf("%s%d/", newBase, cur.id)
	if _, err := tx.Exec(`UPDATE work_items SET path = ? || substr(path, ?) WHERE path >= ? AND path < ?`,
		newPath, len(cur.path)+1, lo, hi); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE work_items SET parent_id = ? WHERE id = ?`, newParentID, cur.id)
	return err
}

type PathMismatch struct {
	Key      string `json:"key"`
	Stored   string `json:"stored"`
	Expected string `json:"expected"`
}

type PathReport struct {
	Checked     int            `json:"checked"`
	Mismatches  []PathMismatch `json:"mismatches"`
	Unreachable int            `json:"unreachable"` // items not reachable from a root (parent cycle)
	Fixed       int            `json:"fixed"`
}

// CheckPaths rebuilds every path from parent_id and compares it with the stored one.
// With fix, mismatching paths are rewritten. This is an operator tool, not tied to a project.
func (s *Service) CheckPaths(ctx context.Context, fix bool) (PathReport, error) {
	rep := PathReport{Mismatches: []PathMismatch{}}
	const expected = `WITH RECURSIVE t(id, path) AS (
		SELECT id, '/' || id || '/' FROM work_items WHERE parent_id IS NULL
		UNION ALL SELECT w.id, t.path || w.id || '/' FROM work_items w JOIN t ON w.parent_id = t.id)`
	err := s.tx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&rep.Checked); err != nil {
			return err
		}
		rows, err := tx.Query(expected + ` SELECT w.id, w.key, w.path, t.path FROM work_items w JOIN t ON t.id = w.id WHERE w.path <> t.path`)
		if err != nil {
			return err
		}
		type fixup struct {
			id   int64
			path string
		}
		var fixes []fixup
		for rows.Next() {
			var id int64
			var m PathMismatch
			if err := rows.Scan(&id, &m.Key, &m.Stored, &m.Expected); err != nil {
				rows.Close()
				return err
			}
			rep.Mismatches = append(rep.Mismatches, m)
			fixes = append(fixes, fixup{id, m.Expected})
		}
		rows.Close()
		var reachable int
		if err := tx.QueryRow(expected + ` SELECT COUNT(*) FROM t`).Scan(&reachable); err != nil {
			return err
		}
		rep.Unreachable = rep.Checked - reachable
		if fix {
			for _, f := range fixes {
				if _, err := tx.Exec(`UPDATE work_items SET path = ? WHERE id = ?`, f.path, f.id); err != nil {
					return err
				}
			}
			rep.Fixed = len(fixes)
		}
		return nil
	})
	return rep, err
}

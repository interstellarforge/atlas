package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const maxMilestoneName = 100

type Milestone struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	DueAt      *int64 `json:"due_at"`
	ReleasedAt *int64 `json:"released_at"`
	Total      int    `json:"total"` // active (not archived) items
	Done       int    `json:"done"`  // of those, in a completed status
}

func milestones(q queryer, projectID int64) ([]Milestone, error) {
	rows, err := q.Query(`SELECT m.id, m.name, m.due_at, m.released_at,
		COUNT(i.id), COALESCE(SUM(st.category = 'completed'), 0)
		FROM milestones m
		LEFT JOIN work_items i ON i.milestone_id = m.id AND i.archived_at IS NULL
		LEFT JOIN project_statuses st ON st.id = i.status_id
		WHERE m.project_id = ? GROUP BY m.id ORDER BY m.released_at IS NOT NULL, m.due_at IS NULL, m.due_at, m.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Milestone{}
	for rows.Next() {
		var m Milestone
		var due, rel sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Name, &due, &rel, &m.Total, &m.Done); err != nil {
			return nil, err
		}
		if due.Valid {
			m.DueAt = &due.Int64
		}
		if rel.Valid {
			m.ReleasedAt = &rel.Int64
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) ListMilestones(ctx context.Context, a auth.Actor, key string) ([]Milestone, error) {
	p, _, err := s.access(s.db.R, a, key, roleViewer)
	if err != nil {
		return nil, err
	}
	return milestones(s.db.R, p.ID)
}

func milestoneName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > maxMilestoneName {
		return "", invalid("milestone name is required (max %d characters)", maxMilestoneName)
	}
	return v, nil
}

// resolveMilestone maps a milestone name to its id within the project; the empty name means none.
func resolveMilestone(q queryer, projectID int64, name string) (any, error) {
	if name == "" {
		return nil, nil
	}
	var id int64
	err := q.QueryRow(`SELECT id FROM milestones WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalid("unknown milestone %q", name)
	}
	return id, err
}

// CreateMilestone and the other milestone changes need a project admin.
func (s *Service) CreateMilestone(ctx context.Context, a auth.Actor, key, name string, dueAt *int64) (Milestone, error) {
	name, err := milestoneName(name)
	if err != nil {
		return Milestone{}, err
	}
	m := Milestone{Name: name, DueAt: dueAt}
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, roleAdmin)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO milestones (project_id, name, due_at, created_at) VALUES (?, ?, ?, ?)`, p.ID, name, dueAt, s.nowMs())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return rule("milestone %q already exists", name)
			}
			return err
		}
		m.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "milestone.create", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": m.ID, "name": name}})
	})
	return m, err
}

// UpdateMilestone patches name, due_at (ms or null) and released (bool: stamps or clears released_at).
func (s *Service) UpdateMilestone(ctx context.Context, a auth.Actor, key string, id int64, patch map[string]any) (Milestone, error) {
	if len(patch) == 0 {
		return Milestone{}, invalid("no fields to update")
	}
	var out Milestone
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, roleAdmin)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM milestones WHERE id = ? AND project_id = ?`, id, p.ID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		for k, v := range patch {
			var q string
			var arg any
			switch k {
			case "name":
				str, _ := v.(string)
				n, err := milestoneName(str)
				if err != nil {
					return err
				}
				q, arg = `UPDATE milestones SET name = ? WHERE id = ?`, n
			case "due_at":
				if v != nil {
					n, ok := asInt(v)
					if !ok {
						return invalid("due_at must be unix milliseconds or null")
					}
					arg = int64(n)
				}
				q = `UPDATE milestones SET due_at = ? WHERE id = ?`
			case "released":
				rel, ok := v.(bool)
				if !ok {
					return invalid("released must be a boolean")
				}
				if rel {
					arg = s.nowMs()
				}
				q = `UPDATE milestones SET released_at = ? WHERE id = ?`
			default:
				return invalid("unknown field %q", k)
			}
			if _, err := tx.Exec(q, arg, id); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return rule("milestone name already exists")
				}
				return err
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "milestone.update", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id, "fields": patch}}); err != nil {
			return err
		}
		list, err := milestones(tx, p.ID)
		if err != nil {
			return err
		}
		for _, m := range list {
			if m.ID == id {
				out = m
			}
		}
		return nil
	})
	return out, err
}

// DeleteMilestone removes an unused milestone; one that still has items is refused, like a status.
func (s *Service) DeleteMilestone(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, roleAdmin)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE milestone_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return rule("milestone is still used by %d items", n)
		}
		res, err := tx.Exec(`DELETE FROM milestones WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "milestone.delete", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id}})
	})
}

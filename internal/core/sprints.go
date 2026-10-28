package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const maxSprintName = 100

// Sprint is a time-boxed group of items (ATL-73). Items outside every sprint are the backlog.
type Sprint struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	StartAt  int64  `json:"start_at"`
	EndAt    int64  `json:"end_at"`
	ClosedAt *int64 `json:"closed_at"`
	Total    int    `json:"total"` // active (not archived) items
	Done     int    `json:"done"`  // of those, in a completed status
}

func sprints(q queryer, projectID int64) ([]Sprint, error) {
	rows, err := q.Query(`SELECT sp.id, sp.name, sp.start_at, sp.end_at, sp.closed_at,
		COUNT(i.id), COALESCE(SUM(st.category = 'completed'), 0)
		FROM sprints sp
		LEFT JOIN work_items i ON i.sprint_id = sp.id AND i.archived_at IS NULL
		LEFT JOIN project_statuses st ON st.id = i.status_id
		WHERE sp.project_id = ? GROUP BY sp.id ORDER BY sp.closed_at IS NOT NULL, sp.start_at, sp.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sprint{}
	for rows.Next() {
		var sp Sprint
		var closed sql.NullInt64
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.StartAt, &sp.EndAt, &closed, &sp.Total, &sp.Done); err != nil {
			return nil, err
		}
		if closed.Valid {
			sp.ClosedAt = &closed.Int64
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Service) ListSprints(ctx context.Context, a auth.Actor, key string) ([]Sprint, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, err
	}
	return sprints(s.db.R, p.ID)
}

func sprintName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > maxSprintName {
		return "", invalid("sprint name is required (max %d characters)", maxSprintName)
	}
	return v, nil
}

// resolveSprint maps a sprint name to its id within the project; the empty name means none. A closed sprint
// takes no new items.
func resolveSprint(q queryer, projectID int64, name string) (any, error) {
	if name == "" {
		return nil, nil
	}
	var id int64
	var closed sql.NullInt64
	err := q.QueryRow(`SELECT id, closed_at FROM sprints WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name).Scan(&id, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalid("unknown sprint %q", name)
	}
	if err == nil && closed.Valid {
		return nil, rule("sprint %q is closed", name)
	}
	return id, err
}

// CreateSprint and the other sprint changes need a project admin.
func (s *Service) CreateSprint(ctx context.Context, a auth.Actor, key, name string, startAt, endAt int64) (Sprint, error) {
	name, err := sprintName(name)
	if err != nil {
		return Sprint{}, err
	}
	if endAt <= startAt {
		return Sprint{}, invalid("end_at must be after start_at")
	}
	sp := Sprint{Name: name, StartAt: startAt, EndAt: endAt}
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO sprints (project_id, name, start_at, end_at, created_at) VALUES (?, ?, ?, ?, ?)`, p.ID, name, startAt, endAt, s.nowMs())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return rule("sprint %q already exists", name)
			}
			return err
		}
		sp.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "sprint.create", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": sp.ID, "name": name}})
	})
	return sp, err
}

// UpdateSprint patches name, start_at, end_at (unix ms) and closed (bool: stamps or clears closed_at).
func (s *Service) UpdateSprint(ctx context.Context, a auth.Actor, key string, id int64, patch map[string]any) (Sprint, error) {
	if len(patch) == 0 {
		return Sprint{}, invalid("no fields to update")
	}
	var out Sprint
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		var start, end int64
		if err := tx.QueryRow(`SELECT start_at, end_at FROM sprints WHERE id = ? AND project_id = ?`, id, p.ID).Scan(&start, &end); err != nil {
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
				n, err := sprintName(str)
				if err != nil {
					return err
				}
				q, arg = `UPDATE sprints SET name = ? WHERE id = ?`, n
			case "start_at", "end_at":
				n, ok := asInt(v)
				if !ok {
					return invalid("%s must be unix milliseconds", k)
				}
				if k == "start_at" {
					start = int64(n)
				} else {
					end = int64(n)
				}
				if end <= start {
					return invalid("end_at must be after start_at")
				}
				q, arg = `UPDATE sprints SET `+k+` = ? WHERE id = ?`, int64(n)
			case "closed":
				closed, ok := v.(bool)
				if !ok {
					return invalid("closed must be a boolean")
				}
				if closed {
					arg = s.nowMs()
				}
				q = `UPDATE sprints SET closed_at = ? WHERE id = ?`
			default:
				return invalid("unknown field %q", k)
			}
			if _, err := tx.Exec(q, arg, id); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return rule("sprint name already exists")
				}
				return err
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "sprint.update", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id, "fields": patch}}); err != nil {
			return err
		}
		list, err := sprints(tx, p.ID)
		if err != nil {
			return err
		}
		for _, sp := range list {
			if sp.ID == id {
				out = sp
			}
		}
		return nil
	})
	return out, err
}

// DeleteSprint removes an unused sprint; one that still has items is refused, like a milestone.
func (s *Service) DeleteSprint(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE sprint_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return rule("sprint is still used by %d items", n)
		}
		res, err := tx.Exec(`DELETE FROM sprints WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "sprint.delete", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id}})
	})
}

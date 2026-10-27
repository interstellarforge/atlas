package core

import (
	"context"

	"github.com/kees/atlas/internal/auth"
)

// StatusCount is the number of active items in one status.
type StatusCount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// Progress counts a project's active (not archived) items by status, category, priority and type (ADR-0014).
type Progress struct {
	Total      int            `json:"total"`
	Categories map[string]int `json:"categories"` // todo, active, review, completed
	Statuses   []StatusCount  `json:"statuses"`   // workflow order, empty statuses included
	Priorities map[string]int `json:"priorities"`
	Types      map[string]int `json:"types"`
}

// ProjectProgress answers from one grouped query, so its cost does not grow with the number of statuses.
func (s *Service) ProjectProgress(ctx context.Context, a auth.Actor, key string) (Progress, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return Progress{}, err
	}
	sts, err := statuses(s.db.R, p.ID)
	if err != nil {
		return Progress{}, err
	}
	out := Progress{Categories: map[string]int{"todo": 0, "active": 0, "review": 0, "completed": 0},
		Statuses: make([]StatusCount, len(sts)), Priorities: map[string]int{}, Types: map[string]int{}}
	index := make(map[int64]int, len(sts))
	for i, st := range sts {
		index[st.ID] = i
		out.Statuses[i] = StatusCount{ID: st.ID, Name: st.Name, Category: st.Category}
	}
	rows, err := s.db.R.Query(`SELECT status_id, priority, type, COUNT(*) FROM work_items
		WHERE project_id = ? AND archived_at IS NULL GROUP BY status_id, priority, type`, p.ID)
	if err != nil {
		return Progress{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status int64
		var priority, typ string
		var n int
		if err := rows.Scan(&status, &priority, &typ, &n); err != nil {
			return Progress{}, err
		}
		i := index[status]
		out.Statuses[i].Count += n
		out.Categories[out.Statuses[i].Category] += n
		out.Priorities[priority] += n
		out.Types[typ] += n
		out.Total += n
	}
	return out, rows.Err()
}

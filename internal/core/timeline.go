package core

import (
	"context"

	"github.com/kees/atlas/internal/auth"
)

const (
	maxTimelineRange = int64(5 * 366 * 24 * 3600 * 1000) // five years, in ms
	timelineLimit    = 200
	timelineLimitMax = 500
	activityLimit    = 50
	activityLimitMax = 200
)

// TimelineItem is an item with a due date: a bar from its creation to its due date.
type TimelineItem struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Category string `json:"category"`
	StartAt  int64  `json:"start_at"`
	DueAt    int64  `json:"due_at"`
}

// Timeline holds what falls in a time range: milestones with a due date and items with a due date.
type Timeline struct {
	Milestones []Milestone    `json:"milestones"`
	Items      []TimelineItem `json:"items"`
	// Truncated is true when more items match than the limit allows; narrow the range.
	Truncated bool `json:"truncated"`
}

// ProjectTimeline returns the milestones and items that overlap [from, to] (unix ms). Only that range is
// loaded, so the view stays cheap on large projects (ADR-0014).
func (s *Service) ProjectTimeline(ctx context.Context, a auth.Actor, key string, from, to int64, limit int) (Timeline, error) {
	if to <= from || to-from > maxTimelineRange {
		return Timeline{}, invalid("from and to must be unix milliseconds, to after from, at most five years apart")
	}
	if limit <= 0 {
		limit = timelineLimit
	}
	limit = min(limit, timelineLimitMax)
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return Timeline{}, err
	}
	all, err := milestones(s.db.R, p.ID)
	if err != nil {
		return Timeline{}, err
	}
	out := Timeline{Milestones: []Milestone{}, Items: []TimelineItem{}}
	for _, m := range all {
		if m.DueAt != nil && *m.DueAt >= from && *m.DueAt <= to {
			out.Milestones = append(out.Milestones, m)
		}
	}
	rows, err := s.db.R.Query(`SELECT i.key, i.type, i.title, st.name, st.category, MIN(i.created_at, i.due_at), i.due_at
		FROM work_items i JOIN project_statuses st ON st.id = i.status_id
		WHERE i.project_id = ? AND i.archived_at IS NULL AND i.due_at IS NOT NULL
		  AND i.due_at >= ? AND MIN(i.created_at, i.due_at) <= ?
		ORDER BY i.due_at, i.id LIMIT ?`, p.ID, from, to, limit+1)
	if err != nil {
		return Timeline{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var it TimelineItem
		if err := rows.Scan(&it.Key, &it.Type, &it.Title, &it.Status, &it.Category, &it.StartAt, &it.DueAt); err != nil {
			return Timeline{}, err
		}
		out.Items = append(out.Items, it)
	}
	if len(out.Items) > limit {
		out.Items, out.Truncated = out.Items[:limit], true
	}
	return out, rows.Err()
}

// ActivityEntry is one line of a project's activity journal.
type ActivityEntry struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Changes   string `json:"changes"` // JSON as written to the audit log
	At        int64  `json:"at"`
}

// ProjectActivity pages the audit log of a project's items and milestones, newest first. before is the id of
// the last entry already seen (0 = from the newest); a full page means there may be more.
func (s *Service) ProjectActivity(ctx context.Context, a auth.Actor, key string, before int64, limit int) ([]ActivityEntry, error) {
	if limit <= 0 {
		limit = activityLimit
	}
	limit = min(limit, activityLimitMax)
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, err
	}
	if before <= 0 {
		before = 1<<62 - 1
	}
	// The project key is validated as [A-Z][A-Z0-9]+, so it holds no GLOB metacharacters.
	rows, err := s.db.R.Query(`SELECT id, actor, operation, target, changes, ts FROM audit_entries
		WHERE (target = ? OR target GLOB ?) AND id < ?
		  AND (operation GLOB 'item.*' OR operation GLOB 'comment.*' OR operation GLOB 'milestone.*'
		    OR operation GLOB 'attachment.*' OR operation GLOB 'time.*')
		ORDER BY id DESC LIMIT ?`, p.Key, p.Key+"-[0-9]*", before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Operation, &e.Target, &e.Changes, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

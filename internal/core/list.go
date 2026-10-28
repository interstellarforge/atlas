package core

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

var statusCategoriesAll = []string{"todo", "active", "review", "completed"}

// filterProjects resolves the project scope of a filter to ids: one project, a list, or everything readable.
func (s *Service) filterProjects(a auth.Actor, f ItemFilter) ([]int64, error) {
	keys := f.Projects
	if f.Project != "" {
		keys = []string{f.Project}
	}
	var ids []int64
	if len(keys) > 0 {
		for _, k := range keys {
			p, _, err := s.access(s.db.R, a, k, actView)
			if err != nil {
				return nil, err
			}
			ids = append(ids, p.ID)
		}
		return ids, nil
	}
	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return nil, err
	}
	for id := range readable {
		ids = append(ids, id)
	}
	return ids, nil
}

// filterConds builds the WHERE conditions shared by lists and boards (aliases i = work_items, st = status).
func (s *Service) filterConds(f ItemFilter, projectIDs []int64) ([]string, []any, error) {
	conds := []string{"i.project_id IN (" + placeholders(len(projectIDs)) + ")"}
	args := make([]any, 0, len(projectIDs)+8)
	for _, id := range projectIDs {
		args = append(args, id)
	}
	add := func(cond string, a ...any) { conds = append(conds, cond); args = append(args, a...) }

	if f.Type != "" {
		add("i.type = ?", f.Type)
	}
	if f.Status != "" {
		add("st.name = ? COLLATE NOCASE", f.Status)
	}
	if f.Category != "" {
		if !slices.Contains(statusCategoriesAll, f.Category) {
			return nil, nil, invalid("category must be todo, active, review or completed")
		}
		add("st.category = ?", f.Category)
	}
	if f.Priority != "" {
		add("i.priority = ?", f.Priority)
	}
	if f.Assignee != nil {
		add("i.assignee_id = ?", *f.Assignee)
	}
	if f.Label != "" {
		add(`EXISTS (SELECT 1 FROM item_labels il JOIN labels l ON l.id = il.label_id WHERE il.item_id = i.id AND l.name = ?)`, f.Label)
	}
	if f.Milestone != "" {
		add("i.milestone_id = (SELECT id FROM milestones WHERE project_id = i.project_id AND name = ? COLLATE NOCASE)", f.Milestone)
	}
	if f.Parent != "" {
		add("i.parent_id = (SELECT id FROM work_items WHERE key = ?)", f.Parent)
	}
	if f.Under != "" {
		var path string
		err := s.db.R.QueryRow(`SELECT path FROM work_items WHERE key = ?`, f.Under).Scan(&path)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, invalid("unknown item %q", f.Under)
		}
		if err != nil {
			return nil, nil, err
		}
		lo, hi := subtreeRange(path)
		add("i.path >= ? AND i.path < ?", lo, hi)
	}
	if f.UpdatedSince != nil {
		add("i.updated_at >= ?", *f.UpdatedSince)
	}
	if f.Q != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Q)
		add(`i.title LIKE ? ESCAPE '\'`, "%"+esc+"%")
	}
	if f.DoneBefore != nil {
		add("NOT (st.category = 'completed' AND i.updated_at < ?)", *f.DoneBefore)
	}
	if !f.IncludeArchived {
		conds = append(conds, "i.archived_at IS NULL")
	}
	return conds, args, nil
}

// sortColumns maps a sort name to its ORDER BY in default and reversed (leading "-") direction.
// Missing due dates and assignees always sort last.
var sortColumns = map[string][2]string{
	"key":      {"i.id", "i.id DESC"},
	"title":    {"i.title COLLATE NOCASE, i.id", "i.title COLLATE NOCASE DESC, i.id"},
	"type":     {"i.type, i.id", "i.type DESC, i.id"},
	"status":   {"st.position, st.name, i.id", "st.position DESC, st.name DESC, i.id"},
	"priority": {"CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END, i.id", "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END DESC, i.id"},
	"assignee": {"i.assignee_id IS NULL, (SELECT name FROM users WHERE id = i.assignee_id) COLLATE NOCASE, i.id", "i.assignee_id IS NULL, (SELECT name FROM users WHERE id = i.assignee_id) COLLATE NOCASE DESC, i.id"},
	"due":      {"i.due_at IS NULL, i.due_at, i.id", "i.due_at IS NULL, i.due_at DESC, i.id"},
	"updated":  {"i.updated_at DESC, i.id DESC", "i.updated_at, i.id"},
	"rank":     {"i.rank, i.id", "i.rank DESC, i.id"},
}

// sortClause maps a sort name (optionally prefixed with "-" to reverse) to ORDER BY. Only the default
// (creation order) supports keyset paging; other sorts page by offset, which is fine at the target
// size of 10.000 items.
func sortClause(sort string) (order string, keyset bool, err error) {
	if sort == "" {
		sort = "key"
	}
	name, rev := strings.CutPrefix(sort, "-")
	cols, ok := sortColumns[name]
	if !ok {
		return "", false, invalid("sort must be key, title, type, status, priority, assignee, due, updated or rank, optionally prefixed with -")
	}
	if rev {
		return cols[1], false, nil
	}
	return cols[0], name == "key", nil
}

func encodeIDCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeIDCursor(c string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, invalid("invalid cursor")
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, invalid("invalid cursor")
	}
	return id, nil
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodeOffsetCursor(c string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || !strings.HasPrefix(string(raw), "o:") {
		return 0, invalid("invalid cursor")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
	if err != nil || n < 0 {
		return 0, invalid("invalid cursor")
	}
	return n, nil
}

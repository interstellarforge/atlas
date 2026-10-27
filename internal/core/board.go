package core

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const defaultColumnPage = 50

// boardFields are the card fields of a board when the client does not ask for others.
var boardFields = []string{"key", "type", "title", "priority", "assignee", "due_at", "parent", "root", "blocked", "version", "labels", "archived_at"}

type BoardFilter struct {
	Type, Priority, Label, Milestone, Q string
	Assignee                            *int64
	IncludeArchived                     bool
	Fields                              []string
	Limit                               int // cards per column in the first page
}

type Column struct {
	Status Status `json:"status"`
	// Statuses lists every status of a merged column (ADR-0011), in order; Status is the first. Empty otherwise.
	Statuses   []Status         `json:"statuses,omitempty"`
	Count      int              `json:"count"`
	OverWIP    bool             `json:"over_wip"` // WIP limits are warnings, not blocks; in a merged column: any member over its own limit
	Items      []map[string]any `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

type Board struct {
	Project string   `json:"project,omitempty"`
	Seq     int64    `json:"seq"` // change-journal position this snapshot is at; subscribe from here
	Columns []Column `json:"columns"`
}

func (f BoardFilter) item() ItemFilter {
	return ItemFilter{Type: f.Type, Priority: f.Priority, Label: f.Label, Milestone: f.Milestone, Q: f.Q, Assignee: f.Assignee,
		IncludeArchived: f.IncludeArchived, Fields: f.fields()}
}

func (f BoardFilter) fields() []string {
	if len(f.Fields) == 0 {
		return boardFields
	}
	return f.Fields
}

func (f BoardFilter) limit() int {
	if f.Limit <= 0 {
		return defaultColumnPage
	}
	return min(f.Limit, maxPage)
}

func (s *Service) currentSeq(q queryer) (int64, error) {
	var seq int64
	err := q.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM change_events`).Scan(&seq)
	return seq, err
}

// Board returns every status column of a project with its first page of cards.
func (s *Service) Board(ctx context.Context, a auth.Actor, key string, f BoardFilter) (Board, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return Board{}, err
	}
	for _, fld := range f.fields() {
		if !knownFields[fld] {
			return Board{}, invalid("unknown field %q", fld)
		}
	}
	// Read the journal position first: anything that happens during the reads below is replayed to the client.
	seq, err := s.currentSeq(s.db.R)
	if err != nil {
		return Board{}, err
	}
	sts, err := statuses(s.db.R, p.ID)
	if err != nil {
		return Board{}, err
	}
	groups := groupStatuses(sts, p.MergeColumns)
	b := Board{Project: p.Key, Seq: seq, Columns: make([]Column, 0, len(groups))}
	for _, g := range groups {
		col, err := s.column(p.ID, g, f, "")
		if err != nil {
			return Board{}, err
		}
		b.Columns = append(b.Columns, col)
	}
	return b, nil
}

// BoardColumn returns further cards of one column, continuing from a cursor.
func (s *Service) BoardColumn(ctx context.Context, a auth.Actor, key string, statusID int64, f BoardFilter, cursor string) (Column, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return Column{}, err
	}
	sts, err := statuses(s.db.R, p.ID)
	if err != nil {
		return Column{}, err
	}
	for _, g := range groupStatuses(sts, p.MergeColumns) {
		if g[0].ID == statusID {
			return s.column(p.ID, g, f, cursor)
		}
	}
	return Column{}, ErrNotFound
}

// groupStatuses returns the board columns as groups of statuses: one status each, or with merged columns
// every status of a category together, ordered by the first status of each category.
func groupStatuses(sts []Status, merge bool) [][]Status {
	groups := make([][]Status, 0, len(sts))
	byCategory := map[string]int{}
	for _, st := range sts {
		i, seen := byCategory[st.Category]
		if !merge || !seen {
			byCategory[st.Category] = len(groups)
			groups = append(groups, []Status{st})
			continue
		}
		groups[i] = append(groups[i], st)
	}
	return groups
}

func (s *Service) column(projectID int64, group []Status, f BoardFilter, cursor string) (Column, error) {
	col := Column{Status: group[0], Items: []map[string]any{}}
	if len(group) > 1 {
		col.Statuses = group
		if !slices.Contains(f.fields(), "status") { // cards in a merged column show which status they are in
			f.Fields = append(slices.Clone(f.fields()), "status")
		}
	}
	itemFilter := f.item()
	conds, args, err := s.filterConds(itemFilter, []int64{projectID})
	if err != nil {
		return col, err
	}
	ids := make([]int64, len(group))
	for i, st := range group {
		ids[i] = st.ID
	}
	in, inArgs := inList(ids)
	conds, args = append(conds, "i.status_id IN ("+in+")"), append(args, inArgs...)

	crows, err := s.db.R.Query(`SELECT i.status_id, COUNT(*) FROM work_items i JOIN project_statuses st ON st.id = i.status_id WHERE `+
		strings.Join(conds, " AND ")+` GROUP BY i.status_id`, args...)
	if err != nil {
		return col, err
	}
	counts := map[int64]int{}
	for crows.Next() {
		var id int64
		var n int
		if err := crows.Scan(&id, &n); err != nil {
			crows.Close()
			return col, err
		}
		counts[id] = n
		col.Count += n
	}
	crows.Close()
	for _, st := range group {
		col.OverWIP = col.OverWIP || (st.WIPLimit != nil && counts[st.ID] > *st.WIPLimit)
	}

	if cursor != "" {
		rank, id, err := decodeRankCursor(cursor)
		if err != nil {
			return col, err
		}
		conds, args = append(conds, "(i.rank, i.id) > (?, ?)"), append(args, rank, id)
	}
	limit := f.limit()
	rows, err := queryItems(s.db.R, slices.Contains(f.fields(), "description"),
		"WHERE "+strings.Join(conds, " AND ")+" ORDER BY i.rank, i.id LIMIT ?", append(args, limit+1)...)
	if err != nil {
		return col, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		var rank string
		if err := s.db.R.QueryRow(`SELECT rank FROM work_items WHERE id = ?`, last.id).Scan(&rank); err != nil {
			return col, err
		}
		c := encodeRankCursor(rank, last.id)
		col.NextCursor = &c
	}
	if slices.Contains(f.fields(), "labels") {
		if err := loadLabels(s.db.R, rows); err != nil {
			return col, err
		}
	}
	for _, r := range rows {
		m, err := Select(r.Item, f.fields())
		if err != nil {
			return col, err
		}
		col.Items = append(col.Items, m)
	}
	return col, nil
}

// CrossBoard groups several projects by status category. Projects need not share status names, and their
// WIP limits are deliberately not merged, so columns carry no limit. Cards add their project key.
func (s *Service) CrossBoard(ctx context.Context, a auth.Actor, projects []string, f BoardFilter) (Board, error) {
	fields := f.fields()
	if !slices.Contains(fields, "project") {
		fields = append(slices.Clone(fields), "project")
	}
	seq, err := s.currentSeq(s.db.R)
	if err != nil {
		return Board{}, err
	}
	b := Board{Seq: seq}
	for _, cat := range statusCategoriesAll {
		itemFilter := f.item()
		itemFilter.Projects, itemFilter.Category, itemFilter.Fields, itemFilter.Limit = projects, cat, fields, f.limit()
		page, err := s.ListItems(ctx, a, itemFilter)
		if err != nil {
			return Board{}, err
		}
		ids, err := s.filterProjects(a, itemFilter)
		if err != nil {
			return Board{}, err
		}
		col := Column{Status: Status{Name: cat, Category: cat}, Items: page.Items, NextCursor: page.NextCursor}
		if len(ids) > 0 {
			conds, args, err := s.filterConds(itemFilter, ids)
			if err != nil {
				return Board{}, err
			}
			if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM work_items i JOIN project_statuses st ON st.id = i.status_id WHERE `+
				strings.Join(conds, " AND "), args...).Scan(&col.Count); err != nil {
				return Board{}, err
			}
		}
		b.Columns = append(b.Columns, col)
	}
	return b, nil
}

// ---- moving ----

// MoveSpec places an item in a column. Status empty keeps the current status. After/Before are the keys of
// the neighbours the item should end up between; with neither it goes to the end of the column.
type MoveSpec struct {
	Status        string
	After, Before string
}

// MoveItem changes status and/or position with one rank write. It needs the item's current version, so
// two people dragging the same card cannot silently overwrite each other.
func (s *Service) MoveItem(ctx context.Context, a auth.Actor, key string, version int64, m MoveSpec) (Item, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return Item{}, err
	}
	var out Item
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, pk, actItemUpdate)
		if err != nil {
			return err
		}
		cur, err := loadRow(tx, key)
		if err != nil {
			return err
		}
		if cur.Version != version {
			c, _ := Select(cur.Item, append(slices.Clone(defaultFields), "labels"))
			return &ConflictError{Msg: fmt.Sprintf("version mismatch: item is at version %d", cur.Version), Current: c}
		}

		target := cur.statusID
		if m.Status != "" {
			if target, err = resolveStatus(tx, p.ID, m.Status); err != nil {
				return err
			}
			allowed, err := transitionAllowed(tx, p.ID, cur.statusID, target)
			if err != nil {
				return err
			}
			if !allowed {
				return rule("workflow does not allow %s -> %s", cur.Status, m.Status)
			}
			if err := checkRequiredSteps(tx, cur.id, cur.statusID, target); err != nil {
				return err
			}
		}

		col := columnOf(tx, p, target)
		lo, hi, err := columnBounds(tx, p.ID, col, cur.id, m)
		if err != nil {
			return err
		}
		if hi != "" && lo >= hi { // equal or inverted keys: space the column out, then look again
			if err := s.rebalance(ctx, tx, a, p.Key, p.ID, col); err != nil {
				return err
			}
			if lo, hi, err = columnBounds(tx, p.ID, col, cur.id, m); err != nil {
				return err
			}
		}
		rank, err := rankBetween(lo, hi)
		if err != nil {
			return rule("could not place the item between those neighbours")
		}

		res, err := tx.Exec(`UPDATE work_items SET status_id = ?, rank = ?, version = version + 1, updated_at = ?
			WHERE id = ? AND version = ?`, target, rank, s.nowMs(), cur.id, version)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return &ConflictError{Msg: "version mismatch"}
		}
		if len(rank) > maxRankLen {
			if err := s.rebalance(ctx, tx, a, p.Key, p.ID, col); err != nil {
				return err
			}
		}
		diff := map[string]any{}
		if target != cur.statusID {
			diff["status"] = map[string]any{"from": cur.Status, "to": m.Status}
			if err := recordVisit(tx, cur.id, target); err != nil {
				return err
			}
		}
		var notices []Notice
		if target != cur.statusID && cur.StatusCategory != "completed" {
			cat, err := statusCategory(tx, target)
			if err != nil {
				return err
			}
			if cat == "completed" {
				if notices, err = completedNotices(tx, cur.id, key); err != nil {
					return err
				}
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "item.move", Target: key, Event: "item.moved",
			Project: p.Key, ItemKey: key, Changes: diff, Notify: notices}); err != nil {
			return err
		}
		r, err := loadRow(tx, key)
		out = r.Item
		return err
	})
	return out, err
}

// columnBounds finds the keys of the items the moved item must end up between.
func columnBounds(tx *sql.Tx, projectID int64, column []int64, selfID int64, m MoveSpec) (lo, hi string, err error) {
	in, inArgs := inList(column)
	col := ` FROM work_items WHERE project_id = ? AND status_id IN (` + in + `) AND archived_at IS NULL AND id <> ?`
	args := func(extra ...any) []any {
		return append(append([]any{projectID}, inArgs...), append([]any{selfID}, extra...)...)
	}
	neighbour := func(k string) (rank string, id int64, err error) {
		err = tx.QueryRow(`SELECT rank, id`+col+` AND key = ?`, args(k)...).Scan(&rank, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, rule("neighbour %s is not in the target column", k)
		}
		return rank, id, err
	}
	edge := func(query string, args ...any) (string, error) {
		var r string
		err := tx.QueryRow(query, args...).Scan(&r)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return r, err
	}

	switch {
	case m.After != "":
		rank, id, err := neighbour(m.After)
		if err != nil {
			return "", "", err
		}
		lo = rank
		if m.Before != "" {
			if hi, _, err = neighbour(m.Before); err != nil {
				return "", "", err
			}
			return lo, hi, nil
		}
		hi, err = edge(`SELECT rank`+col+` AND (rank, id) > (?, ?) ORDER BY rank, id LIMIT 1`, args(rank, id)...)
		return lo, hi, err
	case m.Before != "":
		rank, id, err := neighbour(m.Before)
		if err != nil {
			return "", "", err
		}
		hi = rank
		lo, err = edge(`SELECT rank`+col+` AND (rank, id) < (?, ?) ORDER BY rank DESC, id DESC LIMIT 1`, args(rank, id)...)
		return lo, hi, err
	}
	lo, err = edge(`SELECT rank`+col+` ORDER BY rank DESC, id DESC LIMIT 1`, args()...)
	return lo, "", err
}

// inList renders status ids as SQL placeholders and arguments.
func inList(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// columnOf returns the status ids that share a board column with statusID: just itself, or with merged
// columns on, every status of the same category (ADR-0011). Ranks are kept per column, so all rank
// queries use this set.
func columnOf(q queryer, p Project, statusID int64) []int64 {
	if !p.MergeColumns {
		return []int64{statusID}
	}
	rows, err := q.Query(`SELECT id FROM project_statuses WHERE project_id = ?
		AND category = (SELECT category FROM project_statuses WHERE id = ?) ORDER BY position, id`, p.ID, statusID)
	if err != nil {
		return []int64{statusID}
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []int64{statusID}
	}
	return ids
}

// appendRank returns a key after the last item of a column (excluding one item id).
func appendRank(q queryer, projectID int64, column []int64, excludeID int64) (string, error) {
	var last sql.NullString
	in, inArgs := inList(column)
	err := q.QueryRow(`SELECT rank FROM work_items WHERE project_id = ? AND status_id IN (`+in+`) AND archived_at IS NULL AND id <> ?
		ORDER BY rank DESC, id DESC LIMIT 1`, append(append([]any{projectID}, inArgs...), excludeID)...).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return rankBetween(last.String, "")
}

// rebalance rewrites the keys of one column as short, evenly spaced keys, keeping the order.
func (s *Service) rebalance(ctx context.Context, tx *sql.Tx, a auth.Actor, projectKey string, projectID int64, column []int64) error {
	in, inArgs := inList(column)
	rows, err := tx.Query(`SELECT id FROM work_items WHERE project_id = ? AND status_id IN (`+in+`) AND archived_at IS NULL ORDER BY rank, id`,
		append([]any{projectID}, inArgs...)...)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, k := range evenKeys(len(ids)) {
		if _, err := tx.Exec(`UPDATE work_items SET rank = ? WHERE id = ?`, k, ids[i]); err != nil {
			return err
		}
	}
	// Other clients only know the old keys; they refetch the board on this event.
	return s.journal(ctx, tx, a, change{Operation: "board.rebalance", Target: projectKey, Event: "board.rebalanced",
		Project: projectKey, Changes: map[string]any{"status_id": column[0], "items": len(ids)}})
}

func encodeRankCursor(rank string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(rank + "|" + strconv.FormatInt(id, 10)))
}

func decodeRankCursor(c string) (string, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", 0, invalid("invalid cursor")
	}
	rank, idStr, ok := strings.Cut(string(raw), "|")
	id, perr := strconv.ParseInt(idStr, 10, 64)
	if !ok || perr != nil {
		return "", 0, invalid("invalid cursor")
	}
	return rank, id, nil
}

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

var (
	itemKeyRe  = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-[0-9]+$`)
	priorities = []string{"low", "normal", "high", "urgent"}
)

const (
	maxTitleLen    = 500
	maxLabelLen    = 50
	defaultPage    = 50
	maxPage        = 200
	itemSelectBase = `SELECT i.id, i.key, p.key, i.type, i.title, %s, par.key, rt.key, st.name, st.category, i.priority,
		i.assignee_id, i.due_at, i.custom_data, i.version, i.archived_at, i.created_at, i.updated_at,
		i.project_id, i.status_id, i.parent_id, i.path,
		EXISTS (SELECT 1 FROM item_links l
			JOIN work_items b ON b.id = l.from_id AND b.archived_at IS NULL
			JOIN project_statuses bs ON bs.id = b.status_id AND bs.category <> 'completed'
			WHERE l.to_id = i.id AND l.type = 'blocks'),
		ms.name
		FROM work_items i
		JOIN projects p ON p.id = i.project_id
		JOIN project_statuses st ON st.id = i.status_id
		LEFT JOIN milestones ms ON ms.id = i.milestone_id
		LEFT JOIN work_items par ON par.id = i.parent_id
		LEFT JOIN work_items rt ON rt.id = CAST(substr(i.path, 2, instr(substr(i.path, 2), '/') - 1) AS INTEGER)`
)

type Item struct {
	Key            string         `json:"key"`
	Project        string         `json:"project"`
	Type           string         `json:"type"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Parent         *string        `json:"parent"`
	Root           string         `json:"root"` // key of the top-level ancestor (the item itself for a root)
	Status         string         `json:"status"`
	StatusCategory string         `json:"status_category"`
	Priority       string         `json:"priority"`
	Assignee       *int64         `json:"assignee"`
	DueAt          *int64         `json:"due_at"`
	Milestone      *string        `json:"milestone"`
	Labels         []string       `json:"labels"`
	CustomData     map[string]any `json:"custom_data"`
	Blocked        bool           `json:"blocked"`
	Version        int64          `json:"version"`
	ArchivedAt     *int64         `json:"archived_at"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
}

// itemRow is an Item plus the internal ids the services need.
type itemRow struct {
	Item
	id, projectID, statusID int64
	parentID                sql.NullInt64
	path                    string
}

var defaultFields = []string{"key", "type", "title", "status", "priority", "assignee", "parent", "blocked", "version"}

var knownFields = func() map[string]bool {
	b, _ := json.Marshal(Item{})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}()

// Select returns only the requested fields (default: a compact set), keeping responses small.
func Select(it Item, fields []string) (map[string]any, error) {
	if len(fields) == 0 {
		fields = defaultFields
	}
	b, err := json.Marshal(it)
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if !knownFields[f] {
			return nil, invalid("unknown field %q", f)
		}
		out[f] = all[f]
	}
	return out, nil
}

func scanItem(rows *sql.Rows) (itemRow, error) {
	var r itemRow
	var parent, root, milestone sql.NullString
	var assignee, due, archived sql.NullInt64
	var custom string
	err := rows.Scan(&r.id, &r.Key, &r.Project, &r.Type, &r.Title, &r.Description, &parent, &root, &r.Status, &r.StatusCategory,
		&r.Priority, &assignee, &due, &custom, &r.Version, &archived, &r.CreatedAt, &r.UpdatedAt,
		&r.projectID, &r.statusID, &r.parentID, &r.path, &r.Blocked, &milestone)
	if err != nil {
		return r, err
	}
	if parent.Valid {
		r.Parent = &parent.String
	}
	r.Root = root.String
	if milestone.Valid {
		r.Milestone = &milestone.String
	}
	if assignee.Valid {
		r.Assignee = &assignee.Int64
	}
	if due.Valid {
		r.DueAt = &due.Int64
	}
	if archived.Valid {
		r.ArchivedAt = &archived.Int64
	}
	r.CustomData = map[string]any{}
	_ = json.Unmarshal([]byte(custom), &r.CustomData)
	r.Labels = []string{}
	return r, nil
}

func queryItems(q queryer, withDescription bool, where string, args ...any) ([]itemRow, error) {
	desc := "''"
	if withDescription {
		desc = "i.description"
	}
	rows, err := q.Query(fmt.Sprintf(itemSelectBase, desc)+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemRow
	for rows.Next() {
		r, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadLabels(q queryer, items []itemRow) error {
	if len(items) == 0 {
		return nil
	}
	idx := map[int64]*itemRow{}
	args := make([]any, len(items))
	for i := range items {
		idx[items[i].id] = &items[i]
		args[i] = items[i].id
	}
	rows, err := q.Query(`SELECT il.item_id, l.name FROM item_labels il JOIN labels l ON l.id = il.label_id
		WHERE il.item_id IN (`+placeholders(len(items))+`) ORDER BY l.name`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		idx[id].Labels = append(idx[id].Labels, name)
	}
	return rows.Err()
}

// loadRow loads one fully populated item by key.
func loadRow(q queryer, key string) (itemRow, error) {
	rows, err := queryItems(q, true, `WHERE i.key = ?`, key)
	if err != nil {
		return itemRow{}, err
	}
	if len(rows) == 0 {
		return itemRow{}, ErrNotFound
	}
	if err := loadLabels(q, rows); err != nil {
		return itemRow{}, err
	}
	return rows[0], nil
}

func projectOfKey(key string) (string, error) {
	m := itemKeyRe.FindStringSubmatch(key)
	if m == nil {
		return "", invalid("invalid item key %q", key)
	}
	return m[1], nil
}

func (s *Service) GetItem(ctx context.Context, a auth.Actor, key string) (Item, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return Item{}, err
	}
	if _, _, err := s.access(s.db.R, a, pk, roleViewer); err != nil {
		return Item{}, err
	}
	r, err := loadRow(s.db.R, key)
	return r.Item, err
}

// ---- create ----

type CreateItem struct {
	Project     string
	Type        string
	Title       string
	Description string
	Parent      string
	Status      string
	Priority    string
	Assignee    *int64
	DueAt       *int64
	Labels      []string
	CustomData  map[string]any
	Milestone   string
}

func (s *Service) CreateItem(ctx context.Context, a auth.Actor, in CreateItem) (Item, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > maxTitleLen {
		return Item{}, invalid("title is required (max %d characters)", maxTitleLen)
	}
	if !slices.Contains(itemTypes, in.Type) {
		return Item{}, invalid("type must be one of %s", strings.Join(itemTypes, ", "))
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !slices.Contains(priorities, in.Priority) {
		return Item{}, invalid("priority must be one of %s", strings.Join(priorities, ", "))
	}

	var out Item
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, in.Project, roleMember)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return rule("project is archived")
		}

		var parentID any
		parentPath := "/"
		if in.Parent != "" {
			par, err := loadRow(tx, in.Parent)
			if errors.Is(err, ErrNotFound) || (err == nil && par.projectID != p.ID) {
				return invalid("parent %s not found in project %s", in.Parent, p.Key)
			}
			if err != nil {
				return err
			}
			if !slices.Contains(allowedParents[in.Type], par.Type) {
				return rule("a %s cannot be placed under a %s", in.Type, par.Type)
			}
			if strings.Count(par.path, "/") > maxDepth { // the new item sits one level below the parent
				return rule("hierarchy deeper than %d levels", maxDepth)
			}
			parentID, parentPath = par.id, par.path
		}

		statusID, err := resolveStatus(tx, p.ID, in.Status)
		if err != nil {
			return err
		}
		milestoneID, err := resolveMilestone(tx, p.ID, in.Milestone)
		if err != nil {
			return err
		}
		if err := checkAssignee(tx, p.ID, in.Assignee); err != nil {
			return err
		}
		defs, err := s.customFields(tx, p.ID)
		if err != nil {
			return err
		}
		custom, err := mergeCustomData(defs, nil, in.CustomData, true)
		if err != nil {
			return err
		}
		customJSON, _ := json.Marshal(custom)

		var number int64
		if err := tx.QueryRow(`SELECT next_item_number FROM projects WHERE id = ?`, p.ID).Scan(&number); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE projects SET next_item_number = next_item_number + 1 WHERE id = ?`, p.ID); err != nil {
			return err
		}
		key := fmt.Sprintf("%s-%d", p.Key, number)
		now := s.nowMs()
		rank, err := appendRank(tx, p.ID, statusID, 0)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO work_items (key, project_id, parent_id, path, type, title, description, status_id, priority,
			assignee_id, due_at, milestone_id, rank, custom_data, version, created_at, updated_at)
			VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			key, p.ID, parentID, in.Type, in.Title, in.Description, statusID, in.Priority,
			in.Assignee, in.DueAt, milestoneID, rank, string(customJSON), now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec(`UPDATE work_items SET path = ? WHERE id = ?`, fmt.Sprintf("%s%d/", parentPath, id), id); err != nil {
			return err
		}
		if err := setLabels(tx, p.ID, id, in.Labels); err != nil {
			return err
		}
		if len(rank) > maxRankLen {
			if err := s.rebalance(ctx, tx, a, p.Key, p.ID, statusID); err != nil {
				return err
			}
		}
		var notify []Notice
		if in.Assignee != nil {
			notify = append(notify, Notice{User: *in.Assignee, Type: NotifyAssigned})
		}
		if err := s.journal(ctx, tx, a, change{Operation: "item.create", Target: key, Event: "item.created",
			Project: p.Key, ItemKey: key, Changes: map[string]any{"type": in.Type, "title": in.Title}, Notify: notify}); err != nil {
			return err
		}
		r, err := loadRow(tx, key)
		out = r.Item
		return err
	})
	return out, err
}

func resolveStatus(q queryer, projectID int64, name string) (int64, error) {
	var id int64
	var err error
	if name == "" {
		err = q.QueryRow(`SELECT id FROM project_statuses WHERE project_id = ? ORDER BY position, id LIMIT 1`, projectID).Scan(&id)
	} else {
		err = q.QueryRow(`SELECT id FROM project_statuses WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalid("unknown status %q", name)
	}
	return id, err
}

func checkAssignee(q queryer, projectID int64, assignee *int64) error {
	if assignee == nil {
		return nil
	}
	var ok int
	err := q.QueryRow(`SELECT 1 FROM users u WHERE u.id = ? AND (u.is_admin = 1 OR
		EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = ? AND m.user_id = u.id))`, *assignee, projectID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("assignee %d is not a member of the project", *assignee)
	}
	return err
}

func setLabels(tx *sql.Tx, projectID, itemID int64, names []string) error {
	seen := map[string]bool{}
	var clean []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || len(n) > maxLabelLen {
			return invalid("labels must be 1-%d characters", maxLabelLen)
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			clean = append(clean, n)
		}
	}
	if _, err := tx.Exec(`DELETE FROM item_labels WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, n := range clean {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO labels (project_id, name) VALUES (?, ?)`, projectID, n); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO item_labels (item_id, label_id)
			SELECT ?, id FROM labels WHERE project_id = ? AND name = ?`, itemID, projectID, n); err != nil {
			return err
		}
	}
	return nil
}

// ---- update ----

// UpdateItem applies a patch if version matches the stored version. Fields: title, description, status,
// priority, assignee (id or null), due_at (ms or null), labels, parent (key or null), custom_data
// (merged; null removes a key) and archived.
func (s *Service) UpdateItem(ctx context.Context, a auth.Actor, key string, version int64, patch map[string]any) (Item, error) {
	if len(patch) == 0 {
		return Item{}, invalid("no fields to update")
	}
	pk, err := projectOfKey(key)
	if err != nil {
		return Item{}, err
	}
	var out Item
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, pk, roleMember)
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

		sets := []string{}
		args := []any{}
		diff := map[string]any{}
		set := func(col, field string, from, to any) {
			sets = append(sets, col+" = ?")
			args = append(args, to)
			diff[field] = map[string]any{"from": from, "to": to}
		}
		event := "item.updated"
		var labels *[]string
		var rebalanceTo int64 // status whose column needs new keys after this update
		var notices []Notice
		var newStatus int64

		for k, v := range patch {
			switch k {
			case "title":
				t, ok := v.(string)
				if !ok || strings.TrimSpace(t) == "" || len(t) > maxTitleLen {
					return invalid("title must be a non-empty string (max %d characters)", maxTitleLen)
				}
				set("title", k, cur.Title, strings.TrimSpace(t))
			case "description":
				d, ok := v.(string)
				if !ok {
					return invalid("description must be a string")
				}
				set("description", k, nil, d) // old text omitted from the audit entry to keep it small
			case "status":
				name, ok := v.(string)
				if !ok {
					return invalid("status must be a status name")
				}
				id, err := resolveStatus(tx, p.ID, name)
				if err != nil {
					return err
				}
				allowed, err := transitionAllowed(tx, p.ID, cur.statusID, id)
				if err != nil {
					return err
				}
				if !allowed {
					return rule("workflow does not allow %s -> %s", cur.Status, name)
				}
				set("status_id", k, cur.Status, name)
				args[len(args)-1] = id
				newStatus = id
				if id != cur.statusID { // a changed status puts the item at the end of its new column
					rk, err := appendRank(tx, p.ID, id, cur.id)
					if err != nil {
						return err
					}
					sets, args = append(sets, "rank = ?"), append(args, rk)
					if len(rk) > maxRankLen {
						rebalanceTo = id
					}
				}
			case "priority":
				pr, ok := v.(string)
				if !ok || !slices.Contains(priorities, pr) {
					return invalid("priority must be one of %s", strings.Join(priorities, ", "))
				}
				set("priority", k, cur.Priority, pr)
			case "assignee":
				var id *int64
				if v != nil {
					n, ok := asInt(v)
					if !ok {
						return invalid("assignee must be a user id or null")
					}
					n64 := int64(n)
					id = &n64
				}
				if err := checkAssignee(tx, p.ID, id); err != nil {
					return err
				}
				set("assignee_id", k, cur.Assignee, id)
				if id != nil && (cur.Assignee == nil || *cur.Assignee != *id) {
					notices = append(notices, Notice{User: *id, Type: NotifyAssigned})
				}
			case "due_at":
				var due *int64
				if v != nil {
					n, ok := asInt(v)
					if !ok {
						return invalid("due_at must be unix milliseconds or null")
					}
					n64 := int64(n)
					due = &n64
				}
				set("due_at", k, cur.DueAt, due)
			case "labels":
				raw, ok := v.([]any)
				if !ok {
					return invalid("labels must be an array of strings")
				}
				names := make([]string, 0, len(raw))
				for _, x := range raw {
					str, ok := x.(string)
					if !ok {
						return invalid("labels must be an array of strings")
					}
					names = append(names, str)
				}
				labels = &names
				diff[k] = map[string]any{"from": cur.Labels, "to": names}
			case "custom_data":
				m, ok := v.(map[string]any)
				if !ok {
					return invalid("custom_data must be an object")
				}
				defs, err := s.customFields(tx, p.ID)
				if err != nil {
					return err
				}
				merged, err := mergeCustomData(defs, cur.CustomData, m, false)
				if err != nil {
					return err
				}
				b, _ := json.Marshal(merged)
				set("custom_data", k, cur.CustomData, string(b))
			case "milestone":
				name := ""
				if v != nil {
					str, ok := v.(string)
					if !ok {
						return invalid("milestone must be a milestone name or null")
					}
					name = str
				}
				id, err := resolveMilestone(tx, p.ID, name)
				if err != nil {
					return err
				}
				var from any
				if cur.Milestone != nil {
					from = *cur.Milestone
				}
				set("milestone_id", k, from, v)
				args[len(args)-1] = id
			case "archived":
				arch, ok := v.(bool)
				if !ok {
					return invalid("archived must be a boolean")
				}
				var at any
				if arch {
					at = s.nowMs()
					event = "item.archived"
				}
				set("archived_at", k, cur.ArchivedAt != nil, at)
			case "parent":
				if err := s.reparent(tx, cur, v); err != nil {
					return err
				}
				diff[k] = map[string]any{"from": cur.Parent, "to": v}
			default:
				return invalid("unknown field %q", k)
			}
		}

		sets = append(sets, "version = version + 1", "updated_at = ?")
		// version is checked again in SQL as a second line of defence behind the serialized writer.
		args = append(args, s.nowMs(), cur.id, version)
		res, err := tx.Exec(`UPDATE work_items SET `+strings.Join(sets, ", ")+` WHERE id = ? AND version = ?`, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return &ConflictError{Msg: "version mismatch"}
		}
		if labels != nil {
			if err := setLabels(tx, p.ID, cur.id, *labels); err != nil {
				return err
			}
		}
		if rebalanceTo != 0 {
			if err := s.rebalance(ctx, tx, a, p.Key, p.ID, rebalanceTo); err != nil {
				return err
			}
		}
		if newStatus != 0 && newStatus != cur.statusID && cur.StatusCategory != "completed" {
			cat, err := statusCategory(tx, newStatus)
			if err != nil {
				return err
			}
			if cat == "completed" {
				ns, err := completedNotices(tx, cur.id, key)
				if err != nil {
					return err
				}
				notices = append(notices, ns...)
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "item.update", Target: key, Event: event,
			Project: p.Key, ItemKey: key, Changes: diff, Notify: notices}); err != nil {
			return err
		}
		r, err := loadRow(tx, key)
		out = r.Item
		return err
	})
	return out, err
}

// ---- list ----

type ItemFilter struct {
	Project         string
	Projects        []string // several projects; ignored when Project is set
	Type            string
	Status          string
	Priority        string
	Label           string
	Milestone       string
	Parent          string
	Under           string // item key: that item and all its descendants
	Q               string // case-insensitive title substring
	Category        string // status category: todo, active or completed
	Sort            string // key (default), updated, priority, due or rank
	Assignee        *int64
	UpdatedSince    *int64
	IncludeArchived bool
	Cursor          string
	Limit           int
	Fields          []string
}

type ItemPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func (s *Service) ListItems(ctx context.Context, a auth.Actor, f ItemFilter) (ItemPage, error) {
	page := ItemPage{Items: []map[string]any{}}
	for _, fld := range f.Fields {
		if !knownFields[fld] {
			return page, invalid("unknown field %q", fld)
		}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultPage
	}
	limit = min(limit, maxPage)

	projectIDs, err := s.filterProjects(a, f)
	if err != nil {
		return page, err
	}
	if len(projectIDs) == 0 {
		return page, nil
	}

	conds, args, err := s.filterConds(f, projectIDs)
	if err != nil {
		return page, err
	}
	order, keyset, err := sortClause(f.Sort)
	if err != nil {
		return page, err
	}
	offset := 0
	if f.Cursor != "" {
		if keyset {
			id, err := decodeIDCursor(f.Cursor)
			if err != nil {
				return page, err
			}
			conds, args = append(conds, "i.id > ?"), append(args, id)
		} else if offset, err = decodeOffsetCursor(f.Cursor); err != nil {
			return page, err
		}
	}

	withDesc := slices.Contains(f.Fields, "description")
	rows, err := queryItems(s.db.R, withDesc, "WHERE "+strings.Join(conds, " AND ")+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(args, limit+1, offset)...)
	if err != nil {
		return page, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		c := encodeOffsetCursor(offset + limit)
		if keyset {
			c = encodeIDCursor(rows[limit-1].id)
		}
		page.NextCursor = &c
	}
	if slices.Contains(f.Fields, "labels") {
		if err := loadLabels(s.db.R, rows); err != nil {
			return page, err
		}
	}
	for _, r := range rows {
		m, err := Select(r.Item, f.Fields)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, m)
	}
	return page, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// Hierarchy rules: allowed parent types per item type. An item may also have no parent.
var allowedParents = map[string][]string{
	"epic":  {},
	"story": {"epic"},
	"task":  {"story"},
	"bug":   {"epic", "story"},
}

const maxDepth = 5

var itemTypes = []string{"epic", "story", "task", "bug"}

type Status struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Category string `json:"category"`
	WIPLimit *int   `json:"wip_limit"`
	// AllowCreate false blocks creating items directly in this status; moving into it stays allowed.
	AllowCreate bool `json:"allow_create"`
}

type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type CustomField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Default  any      `json:"default,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type Workflow struct {
	Project      string              `json:"project"`
	Types        []string            `json:"types"`
	Hierarchy    map[string][]string `json:"hierarchy"`
	Statuses     []Status            `json:"statuses"`
	Transitions  []Transition        `json:"transitions"`
	Labels       []string            `json:"labels"`
	Milestones   []Milestone         `json:"milestones"`
	CustomFields []CustomField       `json:"custom_fields"`
	// ItemTemplates are the names of the project's item templates (see /item-templates for their content).
	ItemTemplates []string `json:"item_templates"`
}

func (s *Service) GetWorkflow(ctx context.Context, a auth.Actor, key string) (Workflow, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return Workflow{}, err
	}
	w := Workflow{Project: p.Key, Types: itemTypes, Hierarchy: allowedParents, Transitions: []Transition{}, Labels: []string{}, Milestones: []Milestone{}}

	if w.Statuses, err = statuses(s.db.R, p.ID); err != nil {
		return w, err
	}
	rows, err := s.db.R.Query(`SELECT f.name, t.name FROM workflow_transitions w
		JOIN project_statuses f ON f.id = w.from_status_id JOIN project_statuses t ON t.id = w.to_status_id
		WHERE w.project_id = ? ORDER BY f.position, t.position`, p.ID)
	if err != nil {
		return w, err
	}
	for rows.Next() {
		var t Transition
		if err := rows.Scan(&t.From, &t.To); err != nil {
			rows.Close()
			return w, err
		}
		w.Transitions = append(w.Transitions, t)
	}
	rows.Close()

	lrows, err := s.db.R.Query(`SELECT name FROM labels WHERE project_id = ? ORDER BY name`, p.ID)
	if err != nil {
		return w, err
	}
	for lrows.Next() {
		var n string
		if err := lrows.Scan(&n); err != nil {
			lrows.Close()
			return w, err
		}
		w.Labels = append(w.Labels, n)
	}
	lrows.Close()

	if w.Milestones, err = milestones(s.db.R, p.ID); err != nil {
		return w, err
	}
	if w.CustomFields, err = s.customFields(s.db.R, p.ID); err != nil {
		return w, err
	}
	w.ItemTemplates, err = itemTemplateNames(s.db.R, p.ID)
	return w, err
}

func statuses(q queryer, projectID int64) ([]Status, error) {
	rows, err := q.Query(`SELECT id, name, position, category, wip_limit, allow_create FROM project_statuses WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		var st Status
		var wip sql.NullInt64
		if err := rows.Scan(&st.ID, &st.Name, &st.Position, &st.Category, &wip, &st.AllowCreate); err != nil {
			return nil, err
		}
		if wip.Valid {
			n := int(wip.Int64)
			st.WIPLimit = &n
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

var statusCategories = []string{"todo", "active", "completed"}

func (s *Service) AddStatus(ctx context.Context, a auth.Actor, key, name, category string, wip *int) (Status, error) {
	name = strings.TrimSpace(name)
	if name == "" || !slices.Contains(statusCategories, category) {
		return Status{}, invalid("name and a category (todo, active, completed) are required")
	}
	if wip != nil && *wip < 1 {
		return Status{}, invalid("wip_limit must be at least 1")
	}
	var st Status
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		var pos int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position) + 1, 0) FROM project_statuses WHERE project_id = ?`, p.ID).Scan(&pos); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO project_statuses (project_id, name, position, category, wip_limit) VALUES (?, ?, ?, ?, ?)`,
			p.ID, name, pos, category, wip)
		if err != nil {
			return err
		}
		st = Status{Name: name, Position: pos, Category: category, WIPLimit: wip, AllowCreate: true}
		st.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "status.create", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"status": name}})
	})
	return st, err
}

// UpdateStatus accepts name, category, position and wip_limit (null clears it).
func (s *Service) UpdateStatus(ctx context.Context, a auth.Actor, key string, id int64, patch map[string]any) (Status, error) {
	var out Status
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM project_statuses WHERE id = ? AND project_id = ?`, id, p.ID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		for k, v := range patch {
			var col string
			var val any
			switch k {
			case "name":
				n, ok := v.(string)
				if !ok || strings.TrimSpace(n) == "" {
					return invalid("name must be a non-empty string")
				}
				col, val = "name", strings.TrimSpace(n)
			case "category":
				c, ok := v.(string)
				if !ok || !slices.Contains(statusCategories, c) {
					return invalid("category must be todo, active or completed")
				}
				col, val = "category", c
			case "position":
				n, ok := asInt(v)
				if !ok {
					return invalid("position must be an integer")
				}
				col, val = "position", n
			case "allow_create":
				b, ok := v.(bool)
				if !ok {
					return invalid("allow_create must be true or false")
				}
				if !b {
					var others int
					if err := tx.QueryRow(`SELECT COUNT(*) FROM project_statuses WHERE project_id = ? AND id <> ? AND allow_create = 1`, p.ID, id).Scan(&others); err != nil {
						return err
					}
					if others == 0 {
						return rule("at least one status must allow creating items")
					}
				}
				col, val = "allow_create", b
			case "wip_limit":
				if v == nil {
					col, val = "wip_limit", nil
					break
				}
				n, ok := asInt(v)
				if !ok || n < 1 {
					return invalid("wip_limit must be a positive integer or null")
				}
				col, val = "wip_limit", n
			default:
				return invalid("unknown field %q", k)
			}
			if _, err := tx.Exec(`UPDATE project_statuses SET `+col+` = ? WHERE id = ?`, val, id); err != nil {
				return err
			}
		}
		if len(patch) == 0 {
			return invalid("no fields to update")
		}
		sts, err := statuses(tx, p.ID)
		if err != nil {
			return err
		}
		for _, st := range sts {
			if st.ID == id {
				out = st
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "status.update", Target: key, Event: "project.updated",
			Project: key, Changes: patch})
	})
	return out, err
}

func (s *Service) DeleteStatus(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		var inUse, total int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE status_id = ?`, id).Scan(&inUse); err != nil {
			return err
		}
		if inUse > 0 {
			return rule("status is used by %d items; move them first", inUse)
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM project_statuses WHERE project_id = ?`, p.ID).Scan(&total); err != nil {
			return err
		}
		if total <= 1 {
			return rule("a project needs at least one status")
		}
		res, err := tx.Exec(`DELETE FROM project_statuses WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "status.delete", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"status_id": id}})
	})
}

// SetTransitions replaces the transition rules. An empty list allows every status change.
func (s *Service) SetTransitions(ctx context.Context, a auth.Actor, key string, ts []Transition) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		sts, err := statuses(tx, p.ID)
		if err != nil {
			return err
		}
		ids := map[string]int64{}
		for _, st := range sts {
			ids[strings.ToLower(st.Name)] = st.ID
		}
		if _, err := tx.Exec(`DELETE FROM workflow_transitions WHERE project_id = ?`, p.ID); err != nil {
			return err
		}
		for _, t := range ts {
			from, ok1 := ids[strings.ToLower(t.From)]
			to, ok2 := ids[strings.ToLower(t.To)]
			if !ok1 || !ok2 {
				return invalid("unknown status in transition %q -> %q", t.From, t.To)
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO workflow_transitions (project_id, from_status_id, to_status_id) VALUES (?, ?, ?)`,
				p.ID, from, to); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "workflow.set", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"transitions": len(ts)}})
	})
}

// transitionAllowed reports whether moving from one status to another is permitted.
func transitionAllowed(tx *sql.Tx, projectID, from, to int64) (bool, error) {
	if from == to {
		return true, nil
	}
	var rules int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM workflow_transitions WHERE project_id = ?`, projectID).Scan(&rules); err != nil {
		return false, err
	}
	if rules == 0 {
		return true, nil
	}
	var ok int
	err := tx.QueryRow(`SELECT 1 FROM workflow_transitions WHERE project_id = ? AND from_status_id = ? AND to_status_id = ?`,
		projectID, from, to).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ---- custom fields ----

var fieldTypes = []string{"text", "number", "bool", "date", "select"}

func (s *Service) customFields(q queryer, projectID int64) ([]CustomField, error) {
	rows, err := q.Query(`SELECT name, type, required, default_value, options FROM custom_field_definitions WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomField{}
	for rows.Next() {
		var f CustomField
		var def, opts sql.NullString
		if err := rows.Scan(&f.Name, &f.Type, &f.Required, &def, &opts); err != nil {
			return nil, err
		}
		if def.Valid {
			_ = json.Unmarshal([]byte(def.String), &f.Default)
		}
		if opts.Valid {
			_ = json.Unmarshal([]byte(opts.String), &f.Options)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) AddCustomField(ctx context.Context, a auth.Actor, key string, f CustomField) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || !slices.Contains(fieldTypes, f.Type) {
		return invalid("name and type (text, number, bool, date, select) are required")
	}
	if f.Type == "select" && len(f.Options) == 0 {
		return invalid("select fields need options")
	}
	if f.Default != nil {
		if _, err := validateValue(f, f.Default); err != nil {
			return invalid("default: %v", err)
		}
	}
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		var def, opts any
		if f.Default != nil {
			b, _ := json.Marshal(f.Default)
			def = string(b)
		}
		if len(f.Options) > 0 {
			b, _ := json.Marshal(f.Options)
			opts = string(b)
		}
		if _, err := tx.Exec(`INSERT INTO custom_field_definitions (project_id, name, type, required, default_value, options)
			VALUES (?, ?, ?, ?, ?, ?)`, p.ID, f.Name, f.Type, f.Required, def, opts); err != nil {
			if isUnique(err) {
				return rule("custom field %q already exists", f.Name)
			}
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "field.create", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"field": f.Name}})
	})
}

// DeleteCustomField removes the definition; values already stored on items are left untouched.
func (s *Service) DeleteCustomField(ctx context.Context, a auth.Actor, key, name string) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM custom_field_definitions WHERE project_id = ? AND name = ?`, p.ID, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "field.delete", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"field": name}})
	})
}

// mergeCustomData validates values against the definitions and merges them into current.
// A nil value removes a field. On create, defaults are applied and required fields enforced.
func mergeCustomData(defs []CustomField, current, patch map[string]any, create bool) (map[string]any, error) {
	byName := map[string]CustomField{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	out := map[string]any{}
	for k, v := range current {
		out[k] = v
	}
	for k, v := range patch {
		d, ok := byName[k]
		if !ok {
			return nil, invalid("unknown custom field %q", k)
		}
		if v == nil {
			delete(out, k)
			continue
		}
		nv, err := validateValue(d, v)
		if err != nil {
			return nil, invalid("custom field %q: %v", k, err)
		}
		out[k] = nv
	}
	if create {
		for _, d := range defs {
			if _, ok := out[d.Name]; !ok && d.Default != nil {
				out[d.Name] = d.Default
			}
		}
	}
	for _, d := range defs {
		if _, ok := out[d.Name]; d.Required && !ok {
			return nil, invalid("custom field %q is required", d.Name)
		}
	}
	return out, nil
}

func validateValue(f CustomField, v any) (any, error) {
	switch f.Type {
	case "text":
		if s, ok := v.(string); ok {
			return s, nil
		}
	case "number":
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		case int64:
			return float64(n), nil
		}
	case "bool":
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case "date":
		if s, ok := v.(string); ok {
			if _, err := time.Parse("2006-01-02", s); err == nil {
				return s, nil
			}
			return nil, errors.New("expected a date as YYYY-MM-DD")
		}
	case "select":
		if s, ok := v.(string); ok {
			if slices.Contains(f.Options, s) {
				return s, nil
			}
			return nil, fmt.Errorf("must be one of %s", strings.Join(f.Options, ", "))
		}
	}
	return nil, fmt.Errorf("expected %s", f.Type)
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

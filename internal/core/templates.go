package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// ---- item templates (per project) ----

// ItemTemplate pre-fills new items. Only fixed placeholders exist in the text fields:
// {date} (today, YYYY-MM-DD) and {user} (the name of the creator); there are no scripts.
type ItemTemplate struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	TitlePrefix string   `json:"title_prefix"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	Status      string   `json:"status"`
	Checklist   []string `json:"checklist"`
	// Subitems are created under the item, in the same transaction, as a tree of up to maxSubitemDepth levels.
	Subitems []SubItem `json:"subitems"`
	// Scope is "project" or "instance" (an instance-wide template, managed by global admins, usable in every project).
	Scope string `json:"scope"`
}

type SubItem struct {
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Priority    string    `json:"priority,omitempty"`
	Children    []SubItem `json:"children,omitempty"`
}

const (
	maxSubitems      = 50
	maxSubitemDepth  = 3
	maxTemplates     = 50
	maxTemplateText  = 20000
	maxTemplateLists = 100
)

func itemTemplateNames(q queryer, projectID int64) ([]string, error) {
	rows, err := q.Query(`SELECT DISTINCT name FROM item_templates WHERE project_id = ? OR project_id IS NULL ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanItemTemplate(row interface{ Scan(...any) error }) (ItemTemplate, error) {
	var t ItemTemplate
	var labels, checklist, subitems string
	var project sql.NullInt64
	if err := row.Scan(&t.ID, &t.Name, &t.Type, &t.TitlePrefix, &t.Description, &t.Priority, &labels, &t.Status, &checklist, &subitems, &project); err != nil {
		return t, err
	}
	t.Scope = "project"
	if !project.Valid {
		t.Scope = "instance"
	}
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	_ = json.Unmarshal([]byte(checklist), &t.Checklist)
	_ = json.Unmarshal([]byte(subitems), &t.Subitems)
	if t.Subitems == nil {
		t.Subitems = []SubItem{}
	}
	if t.Labels == nil {
		t.Labels = []string{}
	}
	if t.Checklist == nil {
		t.Checklist = []string{}
	}
	return t, nil
}

const itemTemplateCols = `id, name, type, title_prefix, description, priority, labels, status, checklist, subitems, project_id`

func (s *Service) ListItemTemplates(ctx context.Context, a auth.Actor, key string) ([]ItemTemplate, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT `+itemTemplateCols+` FROM item_templates WHERE project_id = ? OR project_id IS NULL ORDER BY project_id IS NULL, name`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemTemplate{}
	for rows.Next() {
		t, err := scanItemTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func validateItemTemplate(t *ItemTemplate) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 60 {
		return invalid("a template needs a name (max 60 characters)")
	}
	if !slices.Contains(itemTypes, t.Type) {
		return invalid("type must be one of %s", strings.Join(itemTypes, ", "))
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if !slices.Contains(priorities, t.Priority) {
		return invalid("priority must be one of %s", strings.Join(priorities, ", "))
	}
	if len(t.TitlePrefix) > 100 || len(t.Description) > maxTemplateText || len(t.Labels) > maxTemplateLists || len(t.Checklist) > maxChecklistItems {
		return invalid("template is too large")
	}
	for _, l := range t.Labels {
		if n := strings.TrimSpace(l); n == "" || len(n) > maxLabelLen {
			return invalid("labels must be 1-%d characters", maxLabelLen)
		}
	}
	clean := t.Checklist[:0]
	for _, c := range t.Checklist {
		if c = strings.TrimSpace(c); c != "" {
			clean = append(clean, c)
		}
	}
	t.Checklist = clean
	count := 0
	if err := validateSubitems(t.Type, t.Subitems, 1, &count); err != nil {
		return err
	}
	if t.Subitems == nil {
		t.Subitems = []SubItem{}
	}
	if t.Labels == nil {
		t.Labels = []string{}
	}
	if t.Checklist == nil {
		t.Checklist = []string{}
	}
	return nil
}

func validateSubitems(parentType string, list []SubItem, depth int, count *int) error {
	if depth > maxSubitemDepth {
		return invalid("subitems go at most %d levels deep", maxSubitemDepth)
	}
	for i := range list {
		sub := &list[i]
		*count++
		if *count > maxSubitems {
			return invalid("a template holds at most %d subitems", maxSubitems)
		}
		sub.Title = strings.TrimSpace(sub.Title)
		if sub.Title == "" || len(sub.Title) > maxTitleLen || len(sub.Description) > maxTemplateText {
			return invalid("every subitem needs a title (max %d characters)", maxTitleLen)
		}
		if !slices.Contains(itemTypes, sub.Type) || !slices.Contains(allowedParents[sub.Type], parentType) {
			return rule("a %s cannot be placed under a %s", sub.Type, parentType)
		}
		if sub.Priority != "" && !slices.Contains(priorities, sub.Priority) {
			return invalid("priority must be one of %s", strings.Join(priorities, ", "))
		}
		if err := validateSubitems(sub.Type, sub.Children, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

// SaveItemTemplate creates or replaces (by name) a template of the project; project admins only.
func (s *Service) SaveItemTemplate(ctx context.Context, a auth.Actor, key string, t ItemTemplate) (ItemTemplate, error) {
	if err := validateItemTemplate(&t); err != nil {
		return ItemTemplate{}, err
	}
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		if t.ID, err = s.upsertItemTemplate(tx, &p.ID, t); err != nil {
			return err
		}
		t.Scope = "project"
		return s.journal(ctx, tx, a, change{Operation: "item_template.save", Target: key, Event: "project.updated", Project: key, Changes: map[string]any{"name": t.Name}})
	})
	return t, err
}

// SaveInstanceItemTemplate stores a template usable in every project; global admins only. A project
// template with the same name takes precedence in its project.
func (s *Service) SaveInstanceItemTemplate(ctx context.Context, a auth.Actor, t ItemTemplate) (ItemTemplate, error) {
	if err := adminOnly(a); err != nil {
		return ItemTemplate{}, err
	}
	if err := validateItemTemplate(&t); err != nil {
		return ItemTemplate{}, err
	}
	err := s.tx(func(tx *sql.Tx) error {
		var err error
		if t.ID, err = s.upsertItemTemplate(tx, nil, t); err != nil {
			return err
		}
		t.Scope = "instance"
		return s.journal(ctx, tx, a, change{Operation: "item_template.save", Target: "instance", Event: "admin.updated", Changes: map[string]any{"name": t.Name}})
	})
	return t, err
}

func (s *Service) DeleteInstanceItemTemplate(ctx context.Context, a auth.Actor, id int64) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM item_templates WHERE id = ? AND project_id IS NULL`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "item_template.delete", Target: "instance", Event: "admin.updated", Changes: map[string]any{"id": id}})
	})
}

func (s *Service) ListInstanceItemTemplates(ctx context.Context, a auth.Actor) ([]ItemTemplate, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	rows, err := s.db.R.Query(`SELECT ` + itemTemplateCols + ` FROM item_templates WHERE project_id IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemTemplate{}
	for rows.Next() {
		t, err := scanItemTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// upsertItemTemplate writes a validated template for a project (nil = instance wide) and returns its id.
func (s *Service) upsertItemTemplate(tx *sql.Tx, project *int64, t ItemTemplate) (int64, error) {
	labels, _ := json.Marshal(t.Labels)
	checklist, _ := json.Marshal(t.Checklist)
	subitems, _ := json.Marshal(t.Subitems)
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM item_templates WHERE project_id IS ? AND name <> ?`, project, t.Name).Scan(&n); err != nil {
		return 0, err
	}
	if n >= maxTemplates {
		return 0, rule("at most %d item templates", maxTemplates)
	}
	if _, err := tx.Exec(`INSERT INTO item_templates (project_id, name, type, title_prefix, description, priority, labels, status, checklist, subitems, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (COALESCE(project_id, 0), name) DO UPDATE SET type = excluded.type, title_prefix = excluded.title_prefix,
		description = excluded.description, priority = excluded.priority, labels = excluded.labels, status = excluded.status,
		checklist = excluded.checklist, subitems = excluded.subitems`,
		project, t.Name, t.Type, t.TitlePrefix, t.Description, t.Priority, string(labels), t.Status, string(checklist), string(subitems), s.nowMs()); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM item_templates WHERE project_id IS ? AND name = ?`, project, t.Name).Scan(&id)
	return id, err
}

func (s *Service) DeleteItemTemplate(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM item_templates WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "item_template.delete", Target: key, Event: "project.updated", Project: key, Changes: map[string]any{"id": id}})
	})
}

// appliedTemplate is a template resolved against one create call.
type appliedTemplate struct {
	in        CreateItem
	checklist []string
	subitems  []SubItem
	warnings  []string
	expand    func(string) string
}

// applyItemTemplate fills the input from its template (project first, then instance wide) and collects
// warnings for parts that no longer fit (a removed or blocked status): those are skipped, the create itself
// never fails on them.
func (s *Service) applyItemTemplate(a auth.Actor, in CreateItem) (appliedTemplate, error) {
	out := appliedTemplate{in: in}
	p, _, err := s.access(s.db.R, a, in.Project, actItemCreate)
	if err != nil {
		return out, err
	}
	t, err := scanItemTemplate(s.db.R.QueryRow(`SELECT `+itemTemplateCols+` FROM item_templates
		WHERE (project_id = ? OR project_id IS NULL) AND name = ? ORDER BY project_id IS NULL LIMIT 1`, p.ID, in.Template))
	if errors.Is(err, sql.ErrNoRows) {
		return out, invalid("unknown item template %q", in.Template)
	}
	if err != nil {
		return out, err
	}
	var user string
	_ = s.db.R.QueryRow(`SELECT name FROM users WHERE id = ?`, a.UserID).Scan(&user)
	out.expand = strings.NewReplacer("{date}", s.now().Format("2006-01-02"), "{user}", user).Replace

	if in.Type == "" {
		in.Type = t.Type
	}
	if in.Priority == "" {
		in.Priority = t.Priority
	}
	if in.Description == "" {
		in.Description = out.expand(t.Description)
	}
	in.Title = out.expand(t.TitlePrefix) + in.Title
	in.Labels = append(slices.Clone(t.Labels), in.Labels...)
	if in.Status == "" && t.Status != "" {
		sts, err := statuses(s.db.R, p.ID)
		if err != nil {
			return out, err
		}
		i := slices.IndexFunc(sts, func(st Status) bool { return strings.EqualFold(st.Name, t.Status) })
		switch {
		case i < 0:
			out.warnings = append(out.warnings, fmt.Sprintf("template status %q no longer exists; the default status was used", t.Status))
		case !sts[i].AllowCreate:
			out.warnings = append(out.warnings, fmt.Sprintf("template status %q no longer allows creating items; the default status was used", t.Status))
		default:
			in.Status = sts[i].Name
		}
	}
	// Subitems only fit when the item ends up the type the template's tree was validated for.
	if in.Type == t.Type {
		out.subitems = t.Subitems
	} else if len(t.Subitems) > 0 {
		out.warnings = append(out.warnings, "the type was changed, so the template's subitems were skipped")
	}
	out.in, out.checklist = in, t.Checklist
	return out, nil
}

// createSubitems creates the template's subitem tree below parent (all inside the caller's transaction).
func (s *Service) createSubitems(ctx context.Context, a auth.Actor, project, parent string, list []SubItem, expand func(string) string, keys *[]string) error {
	for _, sub := range list {
		it, err := s.createItem(ctx, a, CreateItem{Project: project, Type: sub.Type, Title: expand(sub.Title), Description: expand(sub.Description),
			Priority: sub.Priority, Parent: parent}, nil)
		if err != nil {
			return err
		}
		*keys = append(*keys, it.Key)
		if err := s.createSubitems(ctx, a, project, it.Key, sub.Children, expand, keys); err != nil {
			return err
		}
	}
	return nil
}

// ---- project templates (instance wide) ----

type TemplateStatus struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	WIPLimit    *int   `json:"wip_limit,omitempty"`
	AllowCreate bool   `json:"allow_create"`
	// Requires names statuses an item must have been in before it may enter this one.
	Requires []string `json:"requires,omitempty"`
}

// ProjectTemplateData is the JSON schema (version 1) of a project template: workflow, labels, custom
// fields and item templates. Never members, tokens, items or other user data.
type ProjectTemplateData struct {
	Version       int              `json:"version"`
	MergeColumns  bool             `json:"merge_columns,omitempty"`
	Statuses      []TemplateStatus `json:"statuses"`
	Transitions   []Transition     `json:"transitions"`
	Labels        []string         `json:"labels"`
	CustomFields  []CustomField    `json:"custom_fields"`
	ItemTemplates []ItemTemplate   `json:"item_templates"`
}

type ProjectTemplate struct {
	ID          int64               `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Data        ProjectTemplateData `json:"data"`
}

func (d *ProjectTemplateData) validate() error {
	if d.Version != 1 {
		return invalid("template version must be 1")
	}
	if len(d.Statuses) == 0 || len(d.Statuses) > 30 {
		return invalid("a template needs 1-30 statuses")
	}
	names := map[string]bool{}
	creatable := false
	for _, st := range d.Statuses {
		n := strings.ToLower(strings.TrimSpace(st.Name))
		if n == "" || len(st.Name) > 60 || names[n] {
			return invalid("status names must be unique and 1-60 characters")
		}
		names[n] = true
		if !slices.Contains(statusCategories, st.Category) {
			return invalid("status %q needs a category (todo, active, review, completed)", st.Name)
		}
		if st.WIPLimit != nil && *st.WIPLimit < 1 {
			return invalid("wip_limit of %q must be at least 1", st.Name)
		}
		creatable = creatable || st.AllowCreate
	}
	if !creatable {
		return invalid("at least one status must allow creating items")
	}
	for _, st := range d.Statuses {
		for _, r := range st.Requires {
			if !names[strings.ToLower(r)] || strings.EqualFold(r, st.Name) {
				return invalid("status %q requires %q, which is not another status of the template", st.Name, r)
			}
		}
	}
	for _, t := range d.Transitions {
		if !names[strings.ToLower(t.From)] || !names[strings.ToLower(t.To)] {
			return invalid("transition %q -> %q uses an unknown status", t.From, t.To)
		}
	}
	if len(d.Labels) > 200 || len(d.CustomFields) > 50 || len(d.ItemTemplates) > maxTemplates {
		return invalid("template is too large")
	}
	for _, f := range d.CustomFields {
		if strings.TrimSpace(f.Name) == "" || !slices.Contains(fieldTypes, f.Type) || (f.Type == "select" && len(f.Options) == 0) {
			return invalid("custom field %q is not valid", f.Name)
		}
	}
	for i := range d.ItemTemplates {
		if err := validateItemTemplate(&d.ItemTemplates[i]); err != nil {
			return err
		}
	}
	return nil
}

func scanProjectTemplate(row interface{ Scan(...any) error }) (ProjectTemplate, error) {
	var t ProjectTemplate
	var data string
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &data); err != nil {
		return t, err
	}
	return t, json.Unmarshal([]byte(data), &t.Data)
}

func (s *Service) ListProjectTemplates(ctx context.Context, a auth.Actor) ([]ProjectTemplate, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	rows, err := s.db.R.Query(`SELECT id, name, description, data FROM project_templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectTemplate{}
	for rows.Next() {
		t, err := scanProjectTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveProjectTemplate stores a template (global admins only), validated like an import.
func (s *Service) SaveProjectTemplate(ctx context.Context, a auth.Actor, name, description string, data ProjectTemplateData) (ProjectTemplate, error) {
	if err := adminOnly(a); err != nil {
		return ProjectTemplate{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 || len(description) > 500 {
		return ProjectTemplate{}, invalid("a template needs a name (max 60 characters)")
	}
	data.Version = 1
	if err := data.validate(); err != nil {
		return ProjectTemplate{}, err
	}
	body, _ := json.Marshal(data)
	out := ProjectTemplate{Name: name, Description: description, Data: data}
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO project_templates (name, description, data, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			name, description, string(body), a.UserID, s.nowMs())
		if err != nil {
			if isUnique(err) {
				return rule("a template named %q already exists", name)
			}
			return err
		}
		out.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "project_template.save", Target: name, Event: "admin.updated", Changes: map[string]any{"name": name}})
	})
	return out, err
}

// SaveProjectTemplateFromProject stores a template captured from an existing project (global admins only).
func (s *Service) SaveProjectTemplateFromProject(ctx context.Context, a auth.Actor, name, description, key string) (ProjectTemplate, error) {
	if err := adminOnly(a); err != nil {
		return ProjectTemplate{}, err
	}
	d, err := s.projectToTemplate(ctx, a, key)
	if err != nil {
		return ProjectTemplate{}, err
	}
	return s.SaveProjectTemplate(ctx, a, name, description, d)
}

// projectToTemplate captures the workflow, labels, custom fields and item templates of a project.
func (s *Service) projectToTemplate(ctx context.Context, a auth.Actor, key string) (ProjectTemplateData, error) {
	w, err := s.GetWorkflow(ctx, a, key)
	if err != nil {
		return ProjectTemplateData{}, err
	}
	d := ProjectTemplateData{Version: 1, MergeColumns: w.MergeColumns, Transitions: w.Transitions, Labels: w.Labels, CustomFields: w.CustomFields}
	for _, st := range w.Statuses {
		d.Statuses = append(d.Statuses, TemplateStatus{Name: st.Name, Category: st.Category, WIPLimit: st.WIPLimit, AllowCreate: st.AllowCreate, Requires: st.Requires})
	}
	its, err := s.ListItemTemplates(ctx, a, key)
	if err != nil {
		return d, err
	}
	for _, t := range its {
		t.ID = 0
		d.ItemTemplates = append(d.ItemTemplates, t)
	}
	if d.ItemTemplates == nil {
		d.ItemTemplates = []ItemTemplate{}
	}
	return d, nil
}

func (s *Service) DeleteProjectTemplate(ctx context.Context, a auth.Actor, id int64) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRow(`SELECT name FROM project_templates WHERE id = ?`, id).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_templates WHERE id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "project_template.delete", Target: name, Event: "admin.updated", Changes: map[string]any{"name": name}})
	})
}

// CreateProjectFromTemplate creates the project and applies the template in one transaction: either the
// whole project exists, with the template's workflow, or nothing was created.
func (s *Service) CreateProjectFromTemplate(ctx context.Context, a auth.Actor, key, name string, templateID int64) (Project, error) {
	t, err := scanProjectTemplate(s.db.R.QueryRow(`SELECT id, name, description, data FROM project_templates WHERE id = ?`, templateID))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, invalid("unknown project template")
	}
	if err != nil {
		return Project{}, err
	}
	if err := t.Data.validate(); err != nil {
		return Project{}, err
	}
	tx, err := s.db.W.Begin()
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback()
	bs := *s // shares the database; its tx() joins this transaction
	bs.outer = tx
	p, err := bs.CreateProject(ctx, a, key, name)
	if err != nil {
		return Project{}, err
	}
	if _, err := tx.Exec(`DELETE FROM project_statuses WHERE project_id = ?`, p.ID); err != nil {
		return Project{}, err
	}
	for i, st := range t.Data.Statuses {
		if _, err := tx.Exec(`INSERT INTO project_statuses (project_id, name, position, category, wip_limit, allow_create) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, strings.TrimSpace(st.Name), i, st.Category, st.WIPLimit, st.AllowCreate); err != nil {
			return Project{}, err
		}
	}
	for _, st := range t.Data.Statuses {
		if len(st.Requires) == 0 {
			continue
		}
		id, err := resolveStatus(tx, p.ID, st.Name)
		if err != nil {
			return Project{}, err
		}
		if err := setRequirements(tx, p.ID, id, st.Requires); err != nil {
			return Project{}, err
		}
	}
	if t.Data.MergeColumns {
		if err := bs.SetMergeColumns(ctx, a, p.Key, true); err != nil {
			return Project{}, err
		}
	}
	if len(t.Data.Transitions) > 0 {
		if err := bs.SetTransitions(ctx, a, p.Key, t.Data.Transitions); err != nil {
			return Project{}, err
		}
	}
	for _, l := range t.Data.Labels {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO labels (project_id, name) VALUES (?, ?)`, p.ID, strings.TrimSpace(l)); err != nil {
			return Project{}, err
		}
	}
	for _, f := range t.Data.CustomFields {
		if err := bs.AddCustomField(ctx, a, p.Key, f); err != nil {
			return Project{}, err
		}
	}
	for _, it := range t.Data.ItemTemplates {
		if _, err := bs.SaveItemTemplate(ctx, a, p.Key, it); err != nil {
			return Project{}, err
		}
	}
	if err := bs.journal(ctx, tx, a, change{Operation: "project.from_template", Target: p.Key, Event: "project.updated", Project: p.Key,
		Changes: map[string]any{"template": t.Name}}); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, err
	}
	if s.notify != nil {
		s.notify()
	}
	return p, nil
}

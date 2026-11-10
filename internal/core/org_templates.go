package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Company-scoped templates (ADR-0031 point 9): the organization ADMIN manages item and project templates that only
// that organization sees. Instance templates stay the defaults; an organization item template overrides an instance
// one of the same name in the organization's projects, and a project template overrides both in its project.

// OrgTemplates is the ADMIN's view of the templates of one organization.
type OrgTemplates struct {
	Project []ProjectTemplate `json:"project"`
	Item    []ItemTemplate    `json:"item"`
}

func (s *Service) ListOrgTemplates(ctx context.Context, a auth.Actor, orgKey string) (OrgTemplates, error) {
	o, err := s.adminOfOrg(s.db.R, a, orgKey)
	if err != nil {
		return OrgTemplates{}, err
	}
	out := OrgTemplates{Project: []ProjectTemplate{}, Item: []ItemTemplate{}}
	rows, err := s.db.R.Query(`SELECT `+projectTemplateCols+` FROM project_templates WHERE organization_id = ? ORDER BY name`, o.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		t, err := scanProjectTemplate(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Project = append(out.Project, t)
	}
	rows.Close()
	irows, err := s.db.R.Query(`SELECT `+itemTemplateCols+` FROM item_templates WHERE project_id IS NULL AND organization_id = ? ORDER BY name`, o.ID)
	if err != nil {
		return out, err
	}
	defer irows.Close()
	for irows.Next() {
		t, err := scanItemTemplate(irows)
		if err != nil {
			return out, err
		}
		out.Item = append(out.Item, t)
	}
	return out, irows.Err()
}

// SaveOrgItemTemplate creates or replaces (by name) an item template of the organization.
func (s *Service) SaveOrgItemTemplate(ctx context.Context, a auth.Actor, orgKey string, t ItemTemplate) (ItemTemplate, error) {
	if err := validateItemTemplate(&t); err != nil {
		return ItemTemplate{}, err
	}
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		if t.ID, err = s.upsertItemTemplate(tx, nil, &o.ID, t); err != nil {
			return err
		}
		t.Scope = "organization"
		return s.journal(ctx, tx, a, change{Operation: "item_template.save", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"name": t.Name}})
	})
	return t, err
}

func (s *Service) DeleteOrgItemTemplate(ctx context.Context, a auth.Actor, orgKey string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM item_templates WHERE id = ? AND project_id IS NULL AND organization_id = ?`, id, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "item_template.delete", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"id": id}})
	})
}

// SaveOrgProjectTemplate stores a project template of the organization, validated like an import. With fromProject
// (a project of the same organization) the workflow, labels, fields and item templates are captured from it.
func (s *Service) SaveOrgProjectTemplate(ctx context.Context, a auth.Actor, orgKey, name, description string, data *ProjectTemplateData, fromProject string) (ProjectTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 || len(description) > 500 {
		return ProjectTemplate{}, invalid("a template needs a name (max 60 characters)")
	}
	if _, err := s.adminOfOrg(s.db.R, a, orgKey); err != nil { // before anything is read on the caller's behalf
		return ProjectTemplate{}, err
	}
	if (data == nil) == (fromProject == "") {
		return ProjectTemplate{}, invalid("give either data or from_project")
	}
	var d ProjectTemplateData
	if fromProject != "" {
		var err error
		if d, err = s.projectToTemplate(ctx, a, strings.ToUpper(strings.TrimSpace(fromProject))); err != nil {
			return ProjectTemplate{}, err
		}
	} else {
		d = *data
	}
	d.Version = 1
	if err := d.validate(); err != nil {
		return ProjectTemplate{}, err
	}
	body, _ := json.Marshal(d)
	out := ProjectTemplate{Name: name, Description: description, Data: d}
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		if fromProject != "" {
			var same bool
			if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM projects WHERE key = ? AND organization_id = ?)`, strings.ToUpper(strings.TrimSpace(fromProject)), o.ID).Scan(&same); err != nil {
				return err
			}
			if !same {
				return ErrNotFound
			}
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM project_templates WHERE organization_id = ?`, o.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxTemplates {
			return rule("at most %d project templates", maxTemplates)
		}
		res, err := tx.Exec(`INSERT INTO project_templates (name, description, data, organization_id, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			name, description, string(body), o.ID, a.UserID, s.nowMs())
		if err != nil {
			if isUnique(err) {
				return rule("a template named %q already exists", name)
			}
			return err
		}
		out.ID, _ = res.LastInsertId()
		out.Organization = o.Key
		return s.journal(ctx, tx, a, change{Operation: "project_template.save", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"name": name}})
	})
	return out, err
}

func (s *Service) DeleteOrgProjectTemplate(ctx context.Context, a auth.Actor, orgKey string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		var name string
		if err := tx.QueryRow(`SELECT name FROM project_templates WHERE id = ? AND organization_id = ?`, id, o.ID).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_templates WHERE id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "project_template.delete", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"name": name}})
	})
}

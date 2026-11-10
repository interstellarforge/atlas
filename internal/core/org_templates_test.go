package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

// ADR-0031 point 9: company-scoped templates: only that organization's ADMIN manages them, only its members see and
// use them, no leak into another organization's projects, instance templates stay untouched.
func TestOrganizationTemplates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	e.project("MAINP")
	if _, err := e.svc.CreateProjectIn(ctx, e.carol, "OTHER", "OTHP", "Other project"); err != nil {
		t.Fatal(err)
	}
	e.orgUser(e.aliceID)

	// who may manage them
	tpl := core.ItemTemplate{Name: "Incident", Type: "bug", Description: "## What happened"}
	if _, err := e.svc.SaveOrgItemTemplate(ctx, e.alice, "MAIN", tpl); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER saved an organization template: %v", err)
	}
	if _, err := e.svc.SaveOrgItemTemplate(ctx, e.carol, "MAIN", tpl); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization saved one: %v", err)
	}
	if _, err := e.svc.ListOrgTemplates(ctx, e.carol, "MAIN"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another ADMIN listed the templates: %v", err)
	}

	// item templates: the organization's own projects see and use it, other organizations' projects do not
	saved, err := e.svc.SaveOrgItemTemplate(ctx, e.admin, "MAIN", tpl)
	if err != nil || saved.Scope != "organization" {
		t.Fatalf("save: %v %+v", err, saved)
	}
	if _, err := e.svc.SaveInstanceItemTemplate(ctx, e.admin, core.ItemTemplate{Name: "Incident", Type: "task", Description: "instance version"}); err != nil {
		t.Fatalf("an instance template with the same name collided: %v", err)
	}
	names := func(project string) []string {
		w, err := e.svc.GetWorkflow(ctx, e.admin, project)
		if err != nil {
			t.Fatal(err)
		}
		return w.ItemTemplates
	}
	if !slices.Contains(names("MAINP"), "Incident") {
		t.Fatalf("the organization's project does not see its template: %v", names("MAINP"))
	}
	otherList, err := e.svc.ListItemTemplates(ctx, e.carol, "OTHP")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range otherList {
		if it.Scope == "organization" || it.Description == "## What happened" {
			t.Fatalf("an organization template leaked into another organization's project: %+v", it)
		}
	}
	made := e.item(e.admin, core.CreateItem{Project: "MAINP", Title: "outage", Template: "Incident"})
	if made.Type != "bug" {
		t.Fatalf("the organization template did not win over the instance one: %q", made.Type)
	}
	other, err := e.svc.CreateItem(ctx, e.carol, core.CreateItem{Project: "OTHP", Title: "y", Template: "Incident"})
	if err != nil || other.Type != "task" {
		t.Fatalf("another organization must get the instance template, not the organization's: %v %+v", err, other.Type)
	}
	if err := e.svc.DeleteOrgItemTemplate(ctx, e.admin, "MAIN", saved.ID); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names("MAINP"), "Incident") && e.count(`SELECT COUNT(*) FROM item_templates WHERE organization_id IS NOT NULL`) != 0 {
		t.Fatal("the organization template was not deleted")
	}

	// project templates: visible to members only, always create the project in their organization
	data := &core.ProjectTemplateData{Statuses: []core.TemplateStatus{{Name: "Open", Category: "todo", AllowCreate: true}, {Name: "Closed", Category: "completed", AllowCreate: true}}}
	pt, err := e.svc.SaveOrgProjectTemplate(ctx, e.admin, "MAIN", "Support", "", data, "")
	if err != nil || pt.Organization != "MAIN" {
		t.Fatalf("project template: %v %+v", err, pt)
	}
	if _, err := e.svc.SaveOrgProjectTemplate(ctx, e.carol, "OTHER", "Support", "", data, ""); err != nil {
		t.Fatalf("the same name in another organization must be allowed: %v", err)
	}
	if _, err := e.svc.SaveOrgProjectTemplate(ctx, e.admin, "MAIN", "Support", "", data, ""); !isRule(err) {
		t.Fatalf("a duplicate name in the same organization: %v", err)
	}
	has := func(a core.ProjectTemplate, list []core.ProjectTemplate) bool {
		return slices.ContainsFunc(list, func(x core.ProjectTemplate) bool { return x.ID == a.ID })
	}
	asAlice, _ := e.svc.ListProjectTemplates(ctx, e.alice)
	asBob, _ := e.svc.ListProjectTemplates(ctx, e.bob)
	asSuper, _ := e.svc.ListProjectTemplates(ctx, e.admin)
	if !has(pt, asAlice) {
		t.Fatal("a member does not see the organization's template")
	}
	if has(pt, asBob) {
		t.Fatalf("an outsider sees the organization's template: %+v", asBob)
	}
	if !has(pt, asSuper) { // e.admin is also an explicit ADMIN of MAIN in this fixture
		t.Fatal("the organization's ADMIN does not see it")
	}
	if _, err := e.svc.CreateProjectFromTemplateIn(ctx, e.bob, "", "BOBP", "Bob", pt.ID); !isInvalid(err) {
		t.Fatalf("an outsider used the organization's template: %v", err)
	}
	if _, err := e.svc.CreateProjectFromTemplateIn(ctx, e.admin, "OTHER", "XX1", "Wrong org", pt.ID); !isRule(err) {
		t.Fatalf("a template used for another organization: %v", err)
	}
	p, err := e.svc.CreateProjectFromTemplateIn(ctx, e.admin, "", "SUP", "Support", pt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM projects p JOIN organizations o ON o.id = p.organization_id WHERE p.key = ? AND o.key = 'MAIN'`, p.Key) != 1 {
		t.Fatal("the project was not created in the template's organization")
	}
	if err := e.svc.DeleteProjectTemplate(ctx, e.admin, pt.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the instance delete reached an organization template: %v", err)
	}
	if err := e.svc.DeleteOrgProjectTemplate(ctx, e.carol, "MAIN", pt.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another ADMIN deleted it: %v", err)
	}
	if err := e.svc.DeleteOrgProjectTemplate(ctx, e.admin, "MAIN", pt.ID); err != nil {
		t.Fatal(err)
	}
}

// ADR-0031 point 9: an organization's done_visible_days overrides the instance value for its own projects only.
func TestOrganizationDoneVisibility(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	e.project("MAINP")
	if _, err := e.svc.CreateProjectIn(ctx, e.carol, "OTHER", "OTHP", "Other"); err != nil {
		t.Fatal(err)
	}
	e.item(e.admin, core.CreateItem{Project: "MAINP", Type: "task", Title: "old done main", Status: "Done"})
	e.item(e.carol, core.CreateItem{Project: "OTHP", Type: "task", Title: "old done other", Status: "Done"})
	week := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	if _, err := e.db.W.Exec(`UPDATE work_items SET updated_at = ?`, week); err != nil {
		t.Fatal(err)
	}
	shown := func(a core.ItemFilter) int {
		a.Fields = []string{"title"}
		page, err := e.svc.ListItems(ctx, e.admin, a)
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	cutoff := e.svc.DoneCutoff() // instance default: 14 days
	if shown(core.ItemFilter{Project: "MAINP", DoneBefore: cutoff}) != 1 {
		t.Fatal("a week-old completed item must still show with the instance default of 14 days")
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"done_visible_days": 3.0}); err != nil {
		t.Fatal(err)
	}
	if shown(core.ItemFilter{Project: "MAINP", DoneBefore: cutoff}) != 0 {
		t.Fatal("the organization's 3 days did not hide the week-old completed item")
	}
	if got, _ := e.svc.ListItems(ctx, e.carol, core.ItemFilter{Project: "OTHP", DoneBefore: cutoff, Fields: []string{"title"}}); len(got.Items) != 1 {
		t.Fatal("the rule of one organization leaked into another")
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"done_visible_days": 0.0}); err != nil {
		t.Fatal(err)
	}
	if shown(core.ItemFilter{Project: "MAINP", DoneBefore: cutoff}) != 1 {
		t.Fatal("0 days must never hide completed items")
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"done_visible_days": 4000.0}); !isInvalid(err) {
		t.Fatalf("an out-of-range value: %v", err)
	}
}

// ADR-0031 point 9: the organization's default workflow is applied to every project created in it, also when the
// creator is a plain member; a chosen template replaces it; another organization is unaffected.
func TestOrganizationDefaultWorkflow(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	e.orgUser(e.aliceID)
	data := &core.ProjectTemplateData{Statuses: []core.TemplateStatus{{Name: "Open", Category: "todo", AllowCreate: true}, {Name: "Closed", Category: "completed", AllowCreate: true}},
		Labels: []string{"support"}}
	pt, err := e.svc.SaveOrgProjectTemplate(ctx, e.admin, "MAIN", "Support", "", data, "")
	if err != nil {
		t.Fatal(err)
	}
	otherTpl, err := e.svc.SaveOrgProjectTemplate(ctx, e.carol, "OTHER", "Theirs", "", data, "")
	if err != nil {
		t.Fatal(err)
	}
	names := func(project string) []string {
		w, err := e.svc.GetWorkflow(ctx, e.admin, project)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, st := range w.Statuses {
			out = append(out, st.Name)
		}
		return out
	}

	// only a template the organization may use can be the default
	if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"default_project_template": float64(otherTpl.ID)}); !isInvalid(err) {
		t.Fatalf("another organization's template became the default: %v", err)
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.alice, "MAIN", map[string]any{"default_project_template": float64(pt.ID)}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER set the default workflow: %v", err)
	}
	st, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"default_project_template": float64(pt.ID), "project_creation": "members"})
	if err != nil || st.DefaultTemplate == nil || *st.DefaultTemplate != int(pt.ID) {
		t.Fatalf("set default: %v %+v", err, st)
	}

	// applied for the ADMIN and for a plain member who has no workflow rights
	if _, err := e.svc.CreateProjectIn(ctx, e.admin, "MAIN", "DEF1", "By admin"); err != nil {
		t.Fatal(err)
	}
	if got := names("DEF1"); !slices.Equal(got, []string{"Open", "Closed"}) {
		t.Fatalf("the default workflow was not applied for the ADMIN: %v", got)
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.alice, "MAIN", "DEF2", "By member"); err != nil {
		t.Fatal(err)
	}
	if got := names("DEF2"); !slices.Equal(got, []string{"Open", "Closed"}) {
		t.Fatalf("the default workflow was not applied for a member: %v", got)
	}
	if e.count(`SELECT COUNT(*) FROM labels l JOIN projects p ON p.id = l.project_id WHERE p.key = 'DEF2' AND l.name = 'support'`) != 1 {
		t.Fatal("the labels of the default workflow were not applied")
	}

	// an explicitly chosen template replaces the default; another organization is unaffected
	var bug int64
	if err := e.db.R.QueryRow(`SELECT id FROM project_templates WHERE name = 'Bug tracking' AND organization_id IS NULL`).Scan(&bug); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateProjectFromTemplateIn(ctx, e.admin, "MAIN", "BUG1", "Bugs", bug); err != nil {
		t.Fatal(err)
	}
	if got := names("BUG1"); slices.Contains(got, "Open") || !slices.Contains(got, "Triage") {
		t.Fatalf("the chosen template did not replace the default: %v", got)
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.carol, "OTHER", "OTH1", "Other"); err != nil {
		t.Fatal(err)
	}
	if w, _ := e.svc.GetWorkflow(ctx, e.carol, "OTH1"); len(w.Statuses) != 5 || w.Statuses[0].Name != "Backlog" {
		t.Fatalf("the default of one organization leaked into another: %+v", w.Statuses)
	}

	// deleting the template drops the default quietly; clearing works
	if err := e.svc.DeleteOrgProjectTemplate(ctx, e.admin, "MAIN", pt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.admin, "MAIN", "DEF3", "After delete"); err != nil {
		t.Fatal(err)
	}
	if got := names("DEF3"); len(got) != 5 {
		t.Fatalf("a deleted default workflow still applied: %v", got)
	}
}

package api_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
)

func TestItemTemplates(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	mem, _ := e.auth.CreateUser("mem@example.com", "Mia", "password-123", false)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(mem.ID, 10), map[string]any{"role": "member"}, kees)
	memS := loginAs(e, t, "mem@example.com", "password-123")

	tpl := map[string]any{"name": "Bug", "type": "bug", "title_prefix": "[{date}] ", "description": "Reported by {user}", "priority": "high",
		"labels": []string{"regression"}, "status": "Todo", "checklist": []string{"Reproduce", "Fix"}}
	if w := e.do("POST", "/api/v1/projects/EXO/item-templates", tpl, memS); w.Code != 403 {
		t.Fatalf("member managed templates: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/item-templates", map[string]any{"name": "x", "type": "nope"}, kees); w.Code != 400 {
		t.Fatalf("invalid template: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/item-templates", tpl, kees); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}

	// A member uses it: unset fields come from the template, the caller's own values win.
	w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "Crash", "template": "Bug"}, memS)
	if w.Code != 201 {
		t.Fatalf("create from template: %d %s", w.Code, w.Body)
	}
	it := decodeBody[map[string]any](t, w)
	if it["type"] != "bug" || it["priority"] != "high" || it["status"] != "Todo" || !strings.Contains(it["title"].(string), "] Crash") || it["description"] != "Reported by Mia" {
		t.Fatalf("template not applied: %v", it)
	}
	if labels, _ := it["labels"].([]any); len(labels) != 1 || labels[0] != "regression" {
		t.Fatalf("labels: %v", it["labels"])
	}
	if cl := e.do("GET", "/api/v1/items/"+it["key"].(string)+"/checklist", nil, kees).Body.String(); !strings.Contains(cl, "Reproduce") || !strings.Contains(cl, "Fix") {
		t.Fatalf("checklist: %s", cl)
	}
	own := decodeBody[map[string]any](t, e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "T", "template": "Bug", "priority": "low", "type": "task"}, memS))
	if own["priority"] != "low" || own["type"] != "task" {
		t.Fatalf("caller values were overridden: %v", own)
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "T", "template": "Missing"}, memS); w.Code != 400 {
		t.Fatalf("unknown template: %d", w.Code)
	}

	// The template's status disappears: no error, a warning, the default status.
	wf := decodeBody[struct {
		Statuses []struct {
			ID   int64
			Name string
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/workflow", nil, kees))
	for _, s := range wf.Statuses {
		if s.Name == "Todo" {
			if c := e.do("PATCH", "/api/v1/projects/EXO/statuses/"+strconv.FormatInt(s.ID, 10), map[string]any{"name": "To do"}, kees).Code; c != 200 {
				t.Fatalf("rename status: %d", c)
			}
		}
	}
	w = e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "After", "template": "Bug"}, memS)
	after := decodeBody[map[string]any](t, w)
	if w.Code != 201 || after["status"] != "Backlog" || len(after["warnings"].([]any)) != 1 {
		t.Fatalf("stale template status: %d %v", w.Code, after)
	}

	// MCP uses the same template by name.
	_, tok, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	r := mcpCall(e, tok, "items_write", map[string]any{"operations": []any{map[string]any{"op": "create", "project": "EXO", "title": "Via MCP", "template": "Bug"}}})
	if !strings.Contains(r.Body.String(), `bug`) || strings.Contains(r.Body.String(), `"isError":true`) {
		t.Fatalf("mcp create from template: %s", r.Body)
	}
}

func TestProjectTemplates(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	usr, _ := e.auth.CreateUser("u@example.com", "U", "password-123", false)
	_ = usr
	userS := loginAs(e, t, "u@example.com", "password-123")

	// Build a project worth templating.
	e.do("POST", "/api/v1/projects", map[string]any{"key": "SRC", "name": "Source"}, kees)
	e.do("POST", "/api/v1/projects/SRC/statuses", map[string]any{"name": "QA", "category": "active", "wip_limit": 2}, kees)
	e.do("POST", "/api/v1/projects/SRC/custom-fields", map[string]any{"name": "Points", "type": "number"}, kees)
	e.do("POST", "/api/v1/projects/SRC/item-templates", map[string]any{"name": "Story", "type": "story", "status": "Backlog"}, kees)

	if w := e.do("POST", "/api/v1/project-templates", map[string]any{"name": "mine", "from_project": "SRC"}, userS); w.Code != 403 {
		t.Fatalf("non-admin saved a template: %d", w.Code)
	}
	w := e.do("POST", "/api/v1/project-templates", map[string]any{"name": "House style", "from_project": "SRC"}, kees)
	if w.Code != 201 {
		t.Fatalf("save template: %d %s", w.Code, w.Body)
	}
	id := decodeBody[struct{ ID int64 }](t, w).ID

	// Anyone who may create projects can start from it; the workflow comes out identical.
	if w := e.do("POST", "/api/v1/projects", map[string]any{"key": "NEW", "name": "New", "template": id}, userS); w.Code != 201 {
		t.Fatalf("create from template: %d %s", w.Code, w.Body)
	}
	flow := func(key string) string {
		wf := decodeBody[map[string]any](t, e.do("GET", "/api/v1/projects/"+key+"/workflow", nil, kees))
		var parts []string
		for _, s := range wf["statuses"].([]any) {
			m := s.(map[string]any)
			parts = append(parts, m["name"].(string)+"/"+m["category"].(string)+"/"+strconv.Itoa(int(func() float64 { f, _ := m["wip_limit"].(float64); return f }())))
		}
		fields, _ := json.Marshal(wf["custom_fields"])
		tpls, _ := json.Marshal(wf["item_templates"])
		return strings.Join(parts, ",") + string(fields) + string(tpls)
	}
	if flow("NEW") != flow("SRC") {
		t.Fatalf("workflow differs:\n%s\n%s", flow("NEW"), flow("SRC"))
	}

	// A rejected template or a clash leaves nothing behind.
	bad := map[string]any{"version": 1, "statuses": []any{map[string]any{"name": "A", "category": "todo", "allow_create": true}, map[string]any{"name": "a", "category": "todo", "allow_create": true}}}
	if w := e.do("POST", "/api/v1/project-templates", map[string]any{"name": "dup", "data": bad}, kees); w.Code != 400 {
		t.Fatalf("duplicate statuses accepted: %d", w.Code)
	}
	noCreate := map[string]any{"version": 1, "statuses": []any{map[string]any{"name": "A", "category": "todo", "allow_create": false}}}
	if w := e.do("POST", "/api/v1/project-templates", map[string]any{"name": "blocked", "data": noCreate}, kees); w.Code != 400 {
		t.Fatalf("template without a creatable status accepted: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects", map[string]any{"key": "SRC", "name": "Again", "template": id}, userS); w.Code != 422 {
		t.Fatalf("key clash: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/projects/SRC/workflow", nil, kees); !strings.Contains(w.Body.String(), `"QA"`) {
		t.Fatalf("existing project damaged: %s", w.Body)
	}

	// The built-in templates are usable.
	list := decodeBody[struct {
		Items []struct {
			ID   int64
			Name string
		}
	}](t, e.do("GET", "/api/v1/project-templates", nil, userS))
	var bug int64
	for _, it := range list.Items {
		if it.Name == "Bug tracking" {
			bug = it.ID
		}
	}
	if bug == 0 {
		t.Fatalf("built-in templates missing: %+v", list)
	}
	if w := e.do("POST", "/api/v1/projects", map[string]any{"key": "BUG", "name": "Bugs", "template": bug}, userS); w.Code != 201 {
		t.Fatalf("built-in template: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "BUG", "title": "boom", "template": "Bug report"}, userS); w.Code != 201 {
		t.Fatalf("item template from built-in: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", "/api/v1/project-templates/"+strconv.FormatInt(id, 10), nil, userS); w.Code != 403 {
		t.Fatalf("non-admin deleted a template: %d", w.Code)
	}
	var _ = http.StatusOK
}

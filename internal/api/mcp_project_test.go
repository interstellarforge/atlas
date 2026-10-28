package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
)

func TestMCPProjectCreate(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	_, all, _ := e.auth.CreateToken(1, "all", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	_, scoped, _ := e.auth.CreateToken(1, "scoped", []string{auth.ScopeRead, auth.ScopeWrite}, []string{"EXO"}, nil)
	_, ro, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, nil, nil)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	create := func(token, key string, extra ...string) (*sdk.CallToolResult, error) {
		args := map[string]any{"key": key, "name": key + " project"}
		if len(extra) > 0 {
			args["template"] = extra[0]
		}
		return connect(t, srv, token).CallTool(context.Background(), &sdk.CallToolParams{Name: "project_create", Arguments: args})
	}

	if res, err := create(all, "NEW"); failed(res, err) || !strings.Contains(text(res), `"key":"NEW"`) {
		t.Fatalf("project_create: %v %s", err, text(res))
	}
	if res, err := create(all, "NEW"); !failed(res, err) {
		t.Fatal("duplicate key was accepted")
	}
	if res, err := create(all, "1bad"); !failed(res, err) {
		t.Fatal("invalid key was accepted")
	}
	if res, err := create(scoped, "OTH"); !failed(res, err) {
		t.Fatal("project-limited token created a project")
	}
	if res, err := create(ro, "RO"); !failed(res, err) {
		t.Fatal("read-only token created a project")
	}
	e.do("POST", "/api/v1/projects", map[string]any{"key": "SRC", "name": "src"}, asUser)
	if w := e.do("POST", "/api/v1/project-templates", map[string]any{"name": "Lean", "from_project": "SRC"}, asUser); w.Code != 201 {
		t.Fatalf("save template: %d %s", w.Code, w.Body)
	}
	if res, err := create(all, "TPL", "missing"); !failed(res, err) {
		t.Fatal("unknown template was accepted")
	}
	if res, err := create(all, "TPL", "lean"); failed(res, err) {
		t.Fatalf("project_create with template: %v %s", err, text(res))
	}
	if res, err := connect(t, srv, all).CallTool(context.Background(), &sdk.CallToolParams{Name: "project_templates_list"}); failed(res, err) || !strings.Contains(text(res), `"name":"Lean"`) {
		t.Fatalf("project_templates_list: %v %s", err, text(res))
	}
	if n := countAudit(t, e, "project.create"); n != 3 { // NEW, SRC (API), TPL
		t.Errorf("%d project.create audit entries", n)
	}
}

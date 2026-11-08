package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// publicRoutes answer without a session or token. A new unprotected route must be added here on purpose; the
// harness fails otherwise (ATL-446).
var publicRoutes = []string{
	"GET /api/v1/health",
	"POST /api/v1/auth/login",
	"POST /api/v1/auth/register",
	"GET /api/v1/auth/policy",
	"GET /.well-known/oauth-protected-resource",
	"GET /.well-known/oauth-protected-resource/mcp",
	"GET /.well-known/oauth-authorization-server",
	"POST /oauth/register",
	"GET /oauth/authorize",
	"POST /oauth/authorize",
	"POST /oauth/token",
}

// streaming routes never finish in a recorder; they are only checked without credentials.
var streamingRoutes = []string{"GET /api/v1/events"}

var routeRe = regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+) ([^"]+)",\s*(protect\()?`)

type route struct {
	method, path string
	protected    bool
}

func (r route) String() string { return r.method + " " + r.path }

// routesFromSource reads every route registered in internal/api, so a new route cannot escape the matrix.
func routesFromSource(t *testing.T) []route {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	var out []route
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeRe.FindAllStringSubmatch(string(b), -1) {
			out = append(out, route{m[1], m[2], m[3] != ""})
		}
	}
	if len(out) < 100 {
		t.Fatalf("found only %d routes; has the registration style changed?", len(out))
	}
	return out
}

var placeholderRe = regexp.MustCompile(`\{[a-z]+\}`)

// concrete fills the path placeholders: project and item keys of the fixture, anything else a dummy.
func (r route) concrete(project, item string) string {
	p := strings.NewReplacer("/items/{key}", "/items/"+item, "/projects/{key}", "/projects/"+project).Replace(r.path)
	return placeholderRe.ReplaceAllString(p, "1")
}

func isAdminRoute(r route) bool {
	return strings.HasPrefix(r.path, "/api/v1/admin/") || r.path == "/api/v1/mcp/stats"
}

// TestAuthorizationMatrix runs every registered route as the actors of the permission model (ADR-0029):
// anonymous and suspended get 401, a signed-in user without rights gets 403 on admin routes and the same 404 for
// a project they cannot see as for one that does not exist.
func TestAuthorizationMatrix(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, kees)
	mk := func(email string) (func(*http.Request), string) {
		u, err := e.auth.CreateUser(email, email, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		return loginAs(e, t, email, "password-123"), strconv.FormatInt(u.ID, 10)
	}
	plain, _ := mk("plain@example.com")
	_, memberID := mk("member@example.com")
	suspended, suspendedID := mk("gone@example.com")
	if c := e.do("PUT", "/api/v1/projects/EXO/members/"+memberID, map[string]any{"role": "member"}, kees).Code; c != 204 {
		t.Fatalf("add member: HTTP %d", c)
	}
	// suspend after login: the open session must stop working at once
	if c := e.do("PATCH", "/api/v1/admin/users/"+suspendedID, map[string]any{"active": false}, kees).Code; c != 204 {
		t.Fatalf("suspend: HTTP %d", c)
	}

	routes := routesFromSource(t)
	for _, r := range routes {
		name := r.String()
		if !r.protected {
			if !slices.Contains(publicRoutes, name) {
				t.Errorf("%s is not protected and not listed as public", name)
			}
			continue
		}
		path := r.concrete("EXO", "EXO-1")
		call := func(as func(*http.Request)) *httptest.ResponseRecorder { return e.do(r.method, path, nil, as) }
		if c := call(nil).Code; c != 401 {
			t.Errorf("%s anonymous: HTTP %d, want 401", name, c)
		}
		if slices.Contains(streamingRoutes, name) {
			continue
		}
		if c := call(suspended).Code; c != 401 {
			t.Errorf("%s suspended account: HTTP %d, want 401", name, c)
		}
		if isAdminRoute(r) {
			if c := call(plain).Code; c != 403 {
				t.Errorf("%s non-admin: HTTP %d, want 403", name, c)
			}
			if r.method == "GET" {
				if c := call(kees).Code; c == 401 || c == 403 {
					t.Errorf("%s superadmin: HTTP %d", name, c)
				}
			}
			continue
		}
		if r.method == "GET" && (strings.Contains(r.path, "/projects/{key}") || strings.Contains(r.path, "/items/{key}")) {
			hidden := call(plain)
			ghost := e.do(r.method, r.concrete("NOPE", "NOPE-1"), nil, plain)
			if hidden.Code != ghost.Code || hidden.Body.String() != ghost.Body.String() {
				t.Errorf("%s: a project the caller cannot see answers differently (HTTP %d %q) from one that does not exist (HTTP %d %q)",
					name, hidden.Code, hidden.Body.String(), ghost.Code, ghost.Body.String())
			}
		}
	}
}

// TestUserListingIsScoped: a person without a shared project sees neither the others' accounts nor their pictures
// (ATL-493); a missing user and a hidden one look the same.
func TestUserListingIsScoped(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	mk := func(email string) (func(*http.Request), int64) {
		u, err := e.auth.CreateUser(email, email, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		return loginAs(e, t, email, "password-123"), u.ID
	}
	plain, _ := mk("plain@example.com")
	_, memberID := mk("member@example.com")
	list := func(as func(*http.Request)) string { return e.do("GET", "/api/v1/users", nil, as).Body.String() }
	if body := list(plain); strings.Contains(body, "member@example.com") {
		t.Fatalf("a stranger lists another account: %s", body)
	}
	if body := list(kees); !strings.Contains(body, "member@example.com") || !strings.Contains(body, "plain@example.com") {
		t.Fatalf("a SUPERADMIN must see everyone: %s", body)
	}
	hidden := e.do("GET", "/api/v1/users/"+strconv.FormatInt(memberID, 10)+"/avatar", nil, plain)
	ghost := e.do("GET", "/api/v1/users/99999/avatar", nil, plain)
	if hidden.Code != ghost.Code || hidden.Body.String() != ghost.Body.String() {
		t.Fatalf("hidden user answers %d %q, missing user %d %q", hidden.Code, hidden.Body.String(), ghost.Code, ghost.Body.String())
	}
}

// mcpToolKinds classifies every MCP tool. A new tool must be added here on purpose (ATL-446).
var mcpToolKinds = map[string]string{
	"projects_list": "read", "project_create": "write", "project_templates_list": "read", "workflow_get": "read",
	"items_search": "read", "issues_search": "read", "issue_create": "write", "issue_update": "write", "item_get": "read",
	"board_get": "read", "changes_since": "read", "items_write": "write", "comment_add": "write", "project_progress": "read",
	"time_log": "write", "time_summary": "read", "context_get": "read", "context_write": "write", "checklist_write": "write",
	"attachments": "read", "milestone_write": "write", "sprint_write": "write", "batch_undo": "write", "attachment_add": "write",
}

// TestMCPToolMatrix: every tool is classified, and a user without access to a project gets the same answer for
// it as for a project that does not exist.
func TestMCPToolMatrix(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, kees)
	u, err := e.auth.CreateUser("plain@example.com", "plain", "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := e.auth.CreateToken(u.ID, "plain", []string{"read", "write"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	s := connect(t, srv, token)

	listed, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if _, ok := mcpToolKinds[tool.Name]; !ok {
			t.Errorf("MCP tool %s is not classified in mcpToolKinds", tool.Name)
		}
	}
	for name := range mcpToolKinds {
		if !slices.ContainsFunc(listed.Tools, func(x *sdk.Tool) bool { return x.Name == name }) {
			t.Errorf("mcpToolKinds lists %s, which no longer exists", name)
		}
	}
	reads := map[string]func(project string) map[string]any{
		"items_search":     func(p string) map[string]any { return map[string]any{"project": p} },
		"board_get":        func(p string) map[string]any { return map[string]any{"project": p} },
		"workflow_get":     func(p string) map[string]any { return map[string]any{"project": p} },
		"project_progress": func(p string) map[string]any { return map[string]any{"project": p} },
		"context_get":      func(p string) map[string]any { return map[string]any{"project": p} },
		"item_get":         func(p string) map[string]any { return map[string]any{"key": p + "-1"} },
	}
	for tool, args := range reads {
		hidden, hErr := s.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args("EXO")})
		ghost, gErr := s.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args("NOPE")})
		got := func(r *sdk.CallToolResult, err error) string {
			if err != nil {
				return "error: " + err.Error()
			}
			return fmt.Sprint(r.IsError, text(r))
		}
		h, g := strings.ReplaceAll(got(hidden, hErr), "EXO", "KEY"), strings.ReplaceAll(got(ghost, gErr), "NOPE", "KEY")
		if h != g {
			t.Errorf("%s: a project the caller cannot see answers %q, a missing one %q", tool, h, g)
		}
	}
}

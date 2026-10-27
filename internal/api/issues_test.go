package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/repo"
)

type fakeRepo struct{}

func (fakeRepo) Readme(context.Context, repo.Repo) (string, error) {
	return "# Atlas\n![shot](docs/shot.svg)\n", nil
}

func (fakeRepo) Get(context.Context, string, int64) ([]byte, string, error) {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "text/plain", nil
}

func connect(t *testing.T, srv *httptest.Server, token string) *sdk.ClientSession {
	t.Helper()
	s, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// failed reports whether a tool call was refused, as a protocol error or as a tool error.
func failed(res *sdk.CallToolResult, err error) bool { return err != nil || res.IsError }

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestMCPIssueTools(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, asUser)
	}
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "bug", "title": "crash on save"}, asUser) // EXO-1
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "not an issue"}, asUser) // EXO-2
	e.do("POST", "/api/v1/items", map[string]any{"project": "OTH", "type": "bug", "title": "elsewhere"}, asUser)     // OTH-1
	_, rw, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, []string{"EXO"}, nil)
	_, ro, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, []string{"EXO"}, nil)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx := context.Background()
	call := func(s *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, error) {
		return s.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	}
	agent := connect(t, srv, rw)

	// Search lists bugs only.
	res, err := call(agent, "issues_search", map[string]any{"project": "EXO"})
	if failed(res, err) || !strings.Contains(text(res), "crash on save") || strings.Contains(text(res), "not an issue") {
		t.Fatalf("issues_search: %v %s", err, text(res))
	}
	// Create makes a bug; the key comes back with its version.
	res, err = call(agent, "issue_create", map[string]any{"project": "EXO", "title": "login loops", "priority": "high", "labels": []any{"auth"}})
	if failed(res, err) || !strings.Contains(text(res), `"key":"EXO-3"`) || !strings.Contains(text(res), `"priority":"high"`) {
		t.Fatalf("issue_create: %v %s", err, text(res))
	}
	if res, err = call(agent, "item_get", map[string]any{"key": "EXO-3", "fields": []any{"type"}}); failed(res, err) || !strings.Contains(text(res), `"type":"bug"`) {
		t.Fatalf("created type: %v %s", err, text(res))
	}
	// Update moves it through the workflow; a stale version conflicts; a task is refused.
	res, err = call(agent, "issue_update", map[string]any{"key": "EXO-3", "version": 1, "status": "In Progress", "assignee": 1})
	if failed(res, err) || !strings.Contains(text(res), `"version":2`) {
		t.Fatalf("issue_update: %v %s", err, text(res))
	}
	if res, err = call(agent, "issue_update", map[string]any{"key": "EXO-3", "version": 1, "title": "stale"}); !failed(res, err) {
		t.Fatal("stale version was accepted")
	}
	if res, err = call(agent, "issue_update", map[string]any{"key": "EXO-2", "version": 1, "title": "x"}); !failed(res, err) {
		t.Fatal("a task was updated through the issue tool")
	}
	if res, err = call(agent, "issue_update", map[string]any{"key": "EXO-3", "version": 2, "status": "In Progress", "clear": []any{"assignee"}}); failed(res, err) {
		t.Fatalf("clear assignee: %v %s", err, text(res))
	}
	// Comments use the shared comment tool.
	if res, err = call(agent, "comment_add", map[string]any{"key": "EXO-3", "body": "reproduced"}); failed(res, err) {
		t.Fatalf("comment_add on an issue: %v", err)
	}
	// Scope and project limits: a read-only token cannot write, a project token cannot reach another project.
	reader := connect(t, srv, ro)
	if res, err = call(reader, "issue_create", map[string]any{"project": "EXO", "title": "nope"}); !failed(res, err) {
		t.Fatal("read-only token created an issue")
	}
	if res, err = call(reader, "issue_update", map[string]any{"key": "EXO-1", "version": 1, "title": "nope"}); !failed(res, err) {
		t.Fatal("read-only token updated an issue")
	}
	if res, err = call(reader, "issues_search", map[string]any{"project": "EXO"}); failed(res, err) {
		t.Fatalf("read-only search: %v", err)
	}
	for name, args := range map[string]map[string]any{
		"issue_create":  {"project": "OTH", "title": "x"},
		"issue_update":  {"key": "OTH-1", "version": 1, "title": "x"},
		"issues_search": {"project": "OTH"},
	} {
		if res, err = call(agent, name, args); !failed(res, err) {
			t.Errorf("%s reached a project outside the token", name)
		}
	}
	if n := countAudit(t, e, "item.create"); n != 4 { // three seeded by the test plus issue_create
		t.Errorf("%d item.create audit entries", n)
	}
}

func countAudit(t *testing.T, e env, op string) int {
	t.Helper()
	cookie, csrf := e.login(t)
	w := e.do("GET", "/api/v1/admin/audit?limit=200", nil, func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) })
	return strings.Count(w.Body.String(), `"operation":"`+op+`"`)
}

func TestReadmeEndpoints(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)

	if w := e.do("GET", "/api/v1/projects/EXO/readme", nil, asUser); !strings.Contains(w.Body.String(), `"state":"unlinked"`) {
		t.Fatalf("unlinked: %s", w.Body)
	}
	if w := e.do("PATCH", "/api/v1/projects/EXO", map[string]any{"repository": map[string]any{"url": "https://user:pw@github.com/acme/atlas"}}, asUser); w.Code != http.StatusBadRequest {
		t.Fatalf("credentials in url: %d %s", w.Code, w.Body)
	}
	w := e.do("PATCH", "/api/v1/projects/EXO", map[string]any{"repository": map[string]any{"url": "https://github.com/acme/atlas"}}, asUser)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"provider":"github"`) {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	w = e.do("GET", "/api/v1/projects/EXO/readme", nil, asUser)
	if !strings.Contains(w.Body.String(), `"state":"ok"`) || !strings.Contains(w.Body.String(), "readme/asset?u=") {
		t.Fatalf("readme: %s", w.Body)
	}

	// The proxied SVG carries a script, so it must be sandboxed and never sniffed.
	w = e.do("GET", "/api/v1/projects/EXO/readme/asset?u=https%3A%2F%2Fraw.githubusercontent.com%2Facme%2Fatlas%2Fmain%2Fdocs%2Fshot.svg", nil, asUser)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/svg+xml" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	if w = e.do("GET", "/api/v1/projects/EXO/readme/asset?u=https%3A%2F%2Fevil.example.com%2Fx.png", nil, asUser); w.Code != http.StatusNotFound {
		t.Fatalf("foreign host: %d", w.Code)
	}
	// Without a session nothing is served.
	if w = e.do("GET", "/api/v1/projects/EXO/readme", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", w.Code)
	}
}

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// TestMCPWithSDKClient drives /mcp with the official client, the same protocol path Claude and Codex use.
func TestMCPWithSDKClient(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, asUser)
		e.do("POST", "/api/v1/items", map[string]any{"project": k, "type": "task", "title": k + " task"}, asUser)
	}
	_, write, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, []string{"EXO"}, nil)
	srv := httptest.NewServer(e.h)
	defer srv.Close()

	ctx := context.Background()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{write}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := json.Marshal(tools.Tools)
	t.Logf("tool catalog: %d tools, %d bytes", len(tools.Tools), len(catalog))
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"projects_list", "workflow_get", "board_get", "items_search", "item_get", "items_write", "comment_add", "changes_since", "attachments"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}

	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "items_write", Arguments: map[string]any{
		"operations": []any{map[string]any{"op": "create", "project": "EXO", "type": "task", "title": "from agent"}}}})
	if err != nil || res.IsError {
		t.Fatalf("items_write: %v %+v", err, res)
	}

	// Checklist and comments are readable in item_get after an agent wrote them.
	if res, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "checklist_write", Arguments: map[string]any{"key": "EXO-2", "add": []any{"step one"}}}); err != nil || res.IsError {
		t.Fatalf("checklist_write: %v %+v", err, res)
	}
	if res, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "item_get", Arguments: map[string]any{"key": "EXO-2"}}); err != nil || res.IsError ||
		!strings.Contains(res.Content[0].(*sdk.TextContent).Text, `"checklist":[{"id":1,"text":"step one","done":false}]`) {
		t.Fatalf("item_get checklist: %v %+v", err, res)
	}

	// Resources: readable inside the token's projects, hidden outside them.
	rr, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "atlas://items/EXO-2"})
	if err != nil || len(rr.Contents) != 1 || !strings.Contains(rr.Contents[0].Text, "from agent") {
		t.Fatalf("item resource: %v %+v", err, rr)
	}
	if _, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "atlas://items/OTH-1"}); err == nil {
		t.Fatal("resource outside the token's projects was readable")
	}
	for _, uri := range []string{"atlas://projects/EXO/board", "atlas://projects/EXO/workflow"} {
		if rr, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri}); err != nil || len(rr.Contents) != 1 {
			t.Fatalf("%s: %v", uri, err)
		}
	}
}

// TestMCPMassArchiveGuardAndStats: archiving is the only way to remove items, so a runaway agent is
// stopped after 10 per hour unless it confirms; every call lands in the aggregate metrics.
func TestMCPMassArchiveGuardAndStats(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	for i := 0; i < 12; i++ {
		e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, asUser)
	}
	_, write, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx := context.Background()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{write}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	archive := func(from, to int, confirm bool) *sdk.CallToolResult {
		var ops []any
		for i := from; i <= to; i++ {
			ops = append(ops, map[string]any{"op": "update", "key": "EXO-" + strconv.Itoa(i), "version": 1, "archived": true})
		}
		args := map[string]any{"operations": ops}
		if confirm {
			args["confirm_mass_archive"] = true
		}
		res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "items_write", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := archive(1, 10, false); res.IsError {
		t.Fatalf("10 archives refused: %+v", res.Content)
	}
	res := archive(11, 11, false)
	if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "confirmation_required") {
		t.Fatalf("11th archive in the hour was not stopped: %+v", res.Content)
	}
	if res := archive(11, 12, true); res.IsError {
		t.Fatalf("confirmed archive refused: %+v", res.Content)
	}

	w := e.do("GET", "/api/v1/mcp/stats", nil, asUser)
	stats := decodeBody[struct {
		Calls     int
		Connected int
		Clients   []struct {
			Actor, Name string
			Connected   bool
		}
		Recent []struct{ Tool, Error string }
		Tools  map[string]struct{ Calls, Errors int }
	}](t, w)
	if stats.Calls != 3 || stats.Tools["items_write"].Calls != 3 || stats.Tools["items_write"].Errors != 1 {
		t.Fatalf("stats: %s", w.Body)
	}
	if stats.Connected != 1 || len(stats.Clients) != 1 || !stats.Clients[0].Connected || stats.Clients[0].Name != "test 1" || !strings.HasPrefix(stats.Clients[0].Actor, "token:pat-") {
		t.Fatalf("clients: %s", w.Body)
	}
	if len(stats.Recent) != 3 || stats.Recent[0].Error != "" || stats.Recent[1].Error != "confirmation_required" {
		t.Fatalf("recent calls (newest first): %s", w.Body)
	}
	if w := e.do("GET", "/api/v1/mcp/stats", nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+write) }); w.Code != http.StatusForbidden {
		t.Fatalf("a token read the connection list: %d", w.Code)
	}
}

func TestHardeningHeadersAndMCPBodyLimit(t *testing.T) {
	e := setup(t)
	h := e.do("GET", "/api/v1/health", nil, nil).Header()
	if h.Get("Permissions-Policy") == "" || h.Get("Cross-Origin-Opener-Policy") != "same-origin" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %v", h)
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS sent although cookies are not secure-only")
	}
	_, tok, _ := e.auth.CreateToken(1, "big", []string{auth.ScopeRead}, nil, nil)
	w := e.do("POST", "/mcp", nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Body = io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 2<<20) + `"}}`))
	})
	if w.Code == http.StatusOK {
		t.Fatalf("a 2 MiB MCP request was accepted: %s", w.Body.String()[:80])
	}
}

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	for _, want := range []string{"projects_list", "workflow_get", "board_get", "items_search", "item_get", "items_write", "comment_add", "changes_since"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}

	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "items_write", Arguments: map[string]any{
		"operations": []any{map[string]any{"op": "create", "project": "EXO", "type": "task", "title": "from agent"}}}})
	if err != nil || res.IsError {
		t.Fatalf("items_write: %v %+v", err, res)
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

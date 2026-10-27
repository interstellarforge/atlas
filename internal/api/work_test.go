package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
)

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body, err)
	}
	return v
}

func TestRESTProjectItemFlowAndVersionConflict(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }

	if w := e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "Exosphere"}, asUser); w.Code != 201 {
		t.Fatalf("create project: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/projects", map[string]any{"key": "exo-x", "name": "bad"}, asUser); w.Code != 400 {
		t.Fatalf("bad key: %d", w.Code)
	}
	w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "story", "title": "Calendar sync"}, asUser)
	if w.Code != 201 {
		t.Fatalf("create item: %d %s", w.Code, w.Body)
	}
	item := decodeBody[map[string]any](t, w)
	if item["key"] != "EXO-1" || item["version"] != 1.0 {
		t.Fatalf("item: %v", item)
	}

	if w := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "status": "Todo"}, asUser); w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	// Stale version: compact 409 with the current version.
	w = e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "title": "stale"}, asUser)
	if w.Code != 409 {
		t.Fatalf("stale patch: %d %s", w.Code, w.Body)
	}
	body := decodeBody[map[string]map[string]any](t, w)
	cur, _ := body["error"]["current"].(map[string]any)
	if body["error"]["code"] != "conflict" || cur["version"] != 2.0 || cur["status"] != "Todo" {
		t.Fatalf("conflict body: %v", body)
	}
	if w := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"title": "no version"}, asUser); w.Code != 400 {
		t.Fatalf("missing version: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 2, "status": "Nope"}, asUser); w.Code != 400 {
		t.Fatalf("unknown status: %d", w.Code)
	}

	// Field selection keeps list responses compact.
	w = e.do("GET", "/api/v1/items?project=EXO&fields=key,status", nil, asUser)
	page := decodeBody[struct{ Items []map[string]any }](t, w)
	if len(page.Items) != 1 || len(page.Items[0]) != 2 || page.Items[0]["status"] != "Todo" {
		t.Fatalf("list: %s", w.Body)
	}
	if w := e.do("GET", "/api/v1/items/EXO-99", nil, asUser); w.Code != 404 {
		t.Fatalf("missing item: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/projects/EXO/workflow", nil, asUser); w.Code != 200 || !strings.Contains(w.Body.String(), `"In Progress"`) {
		t.Fatalf("workflow: %d %s", w.Code, w.Body)
	}
}

func mcpCall(e env, token, tool string, args map[string]any) *httptest.ResponseRecorder {
	return e.do("POST", "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	}, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
	})
}

type rpcResult struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	} `json:"result"`
}

func TestMCPReadToolsAreScopedAndCompact(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, asUser)
		e.do("POST", "/api/v1/items", map[string]any{"project": k, "type": "task", "title": k + " task"}, asUser)
	}

	_, read, err := e.auth.CreateToken(1, "agent", []string{auth.ScopeRead}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, exoOnly, _ := e.auth.CreateToken(1, "exo", []string{auth.ScopeRead}, []string{"EXO"}, nil)

	if w := mcpCall(e, "", "projects_list", nil); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}

	w := mcpCall(e, read, "items_search", map[string]any{"project": "EXO"})
	if w.Code != 200 {
		t.Fatalf("search: %d %s", w.Code, w.Body)
	}
	res := decodeBody[rpcResult](t, w)
	if res.Result.IsError || len(res.Result.Content) != 1 || len(res.Result.StructuredContent) != 0 {
		t.Fatalf("expected one text block and no structured duplicate: %s", w.Body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(res.Result.Content[0].Text), &page)
	if len(page.Items) != 1 || page.Items[0]["key"] != "EXO-1" || page.Items[0]["description"] != nil {
		t.Fatalf("items: %s", res.Result.Content[0].Text)
	}

	// A token limited to EXO cannot see OTH, and projects_list only shows EXO.
	res = decodeBody[rpcResult](t, mcpCall(e, exoOnly, "items_search", map[string]any{}))
	if strings.Contains(res.Result.Content[0].Text, "OTH-1") || !strings.Contains(res.Result.Content[0].Text, "EXO-1") {
		t.Fatalf("project limit not enforced: %s", res.Result.Content[0].Text)
	}
	res = decodeBody[rpcResult](t, mcpCall(e, exoOnly, "item_get", map[string]any{"key": "OTH-1"}))
	if !res.Result.IsError {
		t.Fatalf("item outside token projects was readable: %v", res)
	}
	res = decodeBody[rpcResult](t, mcpCall(e, read, "item_get", map[string]any{"key": "EXO-1", "fields": []string{"key", "title"}}))
	if res.Result.Content[0].Text != `{"key":"EXO-1","title":"EXO task"}` {
		t.Fatalf("field selection: %s", res.Result.Content[0].Text)
	}
}

func TestMCPWritesAreAtomicIdempotentAndVersioned(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	_, read, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, nil, nil)
	_, write, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)

	call := func(token string, args map[string]any) (rpcResult, string) {
		r := decodeBody[rpcResult](t, mcpCall(e, token, "items_write", args))
		text := ""
		if len(r.Result.Content) > 0 {
			text = r.Result.Content[0].Text
		}
		return r, text
	}
	create := func(title string) map[string]any {
		return map[string]any{"op": "create", "project": "EXO", "type": "task", "title": title}
	}
	count := func() int {
		page := decodeBody[struct{ Items []map[string]any }](t, e.do("GET", "/api/v1/items?project=EXO", nil, asUser))
		return len(page.Items)
	}

	if r, _ := call(read, map[string]any{"operations": []any{create("x")}}); !r.Result.IsError || count() != 0 {
		t.Fatalf("read-only token wrote (items=%d)", count())
	}

	// Atomic: the second operation is invalid, so the first must not persist.
	r, text := call(write, map[string]any{"operations": []any{create("a"), map[string]any{"op": "update", "key": "EXO-99", "version": 1, "title": "z"}}})
	if !r.Result.IsError || !strings.Contains(text, `"operation":1`) || count() != 0 {
		t.Fatalf("atomic batch: %s items=%d", text, count())
	}

	// Idempotent: the same key and request creates once; a different request with the key is refused.
	args := map[string]any{"idempotency_key": "k1", "operations": []any{create("a")}}
	if r, text = call(write, args); r.Result.IsError || count() != 1 {
		t.Fatalf("first write: %s", text)
	}
	if r, text = call(write, args); r.Result.IsError || !strings.Contains(text, `"replayed":true`) || count() != 1 {
		t.Fatalf("replay: %s items=%d", text, count())
	}
	if r, _ = call(write, map[string]any{"idempotency_key": "k1", "operations": []any{create("b")}}); !r.Result.IsError || count() != 1 {
		t.Fatal("key reuse with a different request was accepted")
	}

	// Stale version: conflict carries the current state; partial mode reports it per operation.
	upd := func(v int, title string) map[string]any {
		return map[string]any{"op": "update", "key": "EXO-1", "version": v, "title": title}
	}
	if r, text = call(write, map[string]any{"operations": []any{upd(1, "new")}}); r.Result.IsError {
		t.Fatalf("update: %s", text)
	}
	r, text = call(write, map[string]any{"partial": true, "operations": []any{upd(1, "stale"), create("c")}})
	if r.Result.IsError || !strings.Contains(text, `"code":"conflict"`) || !strings.Contains(text, `"version":2`) || count() != 2 {
		t.Fatalf("partial: %s items=%d", text, count())
	}
}

func TestMCPCommentsAndResponseCap(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t", "description": strings.Repeat("x", 20000)}, asUser)
	_, read, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, nil, nil)
	_, write, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)

	args := map[string]any{"key": "EXO-1", "body": "done"}
	if r := decodeBody[rpcResult](t, mcpCall(e, read, "comment_add", args)); !r.Result.IsError {
		t.Fatal("read-only token added a comment")
	}
	r := decodeBody[rpcResult](t, mcpCall(e, write, "comment_add", args))
	if r.Result.IsError || !strings.Contains(r.Result.Content[0].Text, `"actor":"token:pat-`) {
		t.Fatalf("comment: %+v", r)
	}

	// An oversized result is an explicit error, never cut-off JSON.
	r = decodeBody[rpcResult](t, mcpCall(e, write, "item_get", map[string]any{"key": "EXO-1"}))
	var got struct{ Error struct{ Code string } }
	if err := json.Unmarshal([]byte(r.Result.Content[0].Text), &got); err != nil || !r.Result.IsError || got.Error.Code != "too_large" {
		t.Fatalf("cap: %v %s", err, r.Result.Content[0].Text)
	}
}

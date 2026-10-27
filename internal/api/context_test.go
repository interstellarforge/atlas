package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
)

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

type ctxResult struct {
	Entry          map[string]any `json:"entry"`
	ContextVersion int            `json:"context_version"`
	Replayed       bool           `json:"replayed"`
}

func TestRESTContextFlowAndRights(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, kees)
	}
	member, _ := e.auth.CreateUser("member@x.io", "Member", "password-123", false)
	viewer, _ := e.auth.CreateUser("viewer@x.io", "Viewer", "password-123", false)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(member.ID, 10), map[string]any{"role": "member"}, kees)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(viewer.ID, 10), map[string]any{"role": "viewer"}, kees)
	asMember := loginAs(e, t, "member@x.io", "password-123")
	asViewer := loginAs(e, t, "viewer@x.io", "password-123")

	body := map[string]any{"category": "architecture", "title": "Session handling", "summary": "Cookies plus CSRF header.", "pinned": true,
		"sources": []any{map[string]any{"type": "file", "path": "docs/security.md"}}}
	if w := e.do("POST", "/api/v1/projects/EXO/context", body, asViewer); w.Code != 403 {
		t.Fatalf("viewer create: %d", w.Code)
	}
	w := e.do("POST", "/api/v1/projects/EXO/context", body, asMember)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	created := decodeBody[ctxResult](t, w)
	if created.Entry["key"] != "session-handling" || created.Entry["verification_state"] != "unverified" || created.Entry["source_kind"] != "repository" {
		t.Fatalf("created: %s", w.Body)
	}

	// Validation: bad category, path escaping the repository, unknown field.
	for name, b := range map[string]map[string]any{
		"category": {"category": "misc", "title": "t", "summary": "s"},
		"path":     {"category": "pitfall", "title": "t", "summary": "s", "sources": []any{map[string]any{"type": "file", "path": "../../etc/passwd"}}},
		"env":      {"category": "pitfall", "title": "t", "summary": "s", "sources": []any{map[string]any{"type": "file", "path": ".env"}}},
		"unknown":  {"category": "pitfall", "title": "t", "summary": "s", "nope": 1},
		"version":  {"category": "pitfall", "title": "t", "summary": "s", "version": 2},
	} {
		if w := e.do("POST", "/api/v1/projects/EXO/context", b, asMember); w.Code != 400 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}

	// Reading: viewer yes, anything outside the project 404, reserved words are not entries.
	if w := e.do("GET", "/api/v1/projects/EXO/context/session-handling", nil, asViewer); w.Code != 200 || w.Header().Get("ETag") != `"1"` {
		t.Fatalf("viewer read: %d etag %q", w.Code, w.Header().Get("ETag"))
	}
	if w := e.do("GET", "/api/v1/projects/OTH/context/session-handling", nil, asMember); w.Code != 404 {
		t.Fatalf("other project: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/projects/EXO/context?status=nope", nil, asMember); w.Code != 400 {
		t.Fatalf("bad status: %d", w.Code)
	}

	// Optimistic locking: the version comes in the body or in If-Match; a stale one returns the current state.
	patch := map[string]any{"summary": "Cookies, CSRF header and rotation.", "version": 1}
	if w := e.do("PATCH", "/api/v1/projects/EXO/context/session-handling", map[string]any{"summary": "x"}, asMember); w.Code != 400 {
		t.Fatalf("patch without version: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/projects/EXO/context/session-handling", patch, asMember); w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	w = e.do("PATCH", "/api/v1/projects/EXO/context/session-handling", patch, asMember)
	conflict := decodeBody[struct {
		Error struct {
			Code    string
			Current map[string]any
		}
	}](t, w)
	if w.Code != 409 || conflict.Error.Code != "conflict" || conflict.Error.Current["version"] != float64(2) {
		t.Fatalf("stale patch: %d %s", w.Code, w.Body)
	}
	viaHeader := func(r *http.Request) { asMember(r); r.Header.Set("If-Match", `"2"`) }
	if w := e.do("PATCH", "/api/v1/projects/EXO/context/session-handling", map[string]any{"pinned": false}, viaHeader); w.Code != 200 {
		t.Fatalf("If-Match patch: %d %s", w.Code, w.Body)
	}
	if w := e.do("PATCH", "/api/v1/projects/EXO/context/missing", map[string]any{"title": "x", "version": 1}, asMember); w.Code != 404 {
		t.Fatalf("patch of unknown key creates nothing: %d", w.Code)
	}

	// Idempotency-Key: the replay answers 200 with the first result and writes nothing.
	idem := func(r *http.Request) { asMember(r); r.Header.Set("Idempotency-Key", "once-1") }
	first := e.do("POST", "/api/v1/projects/EXO/context", map[string]any{"category": "decision", "title": "Only once", "summary": "s"}, idem)
	replay := e.do("POST", "/api/v1/projects/EXO/context", map[string]any{"category": "decision", "title": "Only once", "summary": "s"}, idem)
	if first.Code != 201 || replay.Code != 200 || !decodeBody[ctxResult](t, replay).Replayed {
		t.Fatalf("idempotency: %d then %d %s", first.Code, replay.Code, replay.Body)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/context", map[string]any{"category": "decision", "title": "Another", "summary": "s"}, idem); w.Code != 400 {
		t.Fatalf("key reused for another request: %d", w.Code)
	}

	// Verify needs a human with context.verify; a member and a token (even of an admin) are refused.
	if w := e.do("POST", "/api/v1/projects/EXO/context/session-handling/verify", nil, asMember); w.Code != 403 {
		t.Fatalf("member verify: %d", w.Code)
	}
	_, adminToken, _ := e.auth.CreateToken(1, "admin-rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	if w := e.do("POST", "/api/v1/projects/EXO/context/session-handling/verify", nil, bearer(adminToken)); w.Code != 403 {
		t.Fatalf("token verify: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/context/session-handling/verify", nil, kees); w.Code != 200 {
		t.Fatalf("admin verify: %d %s", w.Code, w.Body)
	}
	_, readToken, _ := e.auth.CreateToken(member.ID, "ro", []string{auth.ScopeRead}, nil, nil)
	if w := e.do("POST", "/api/v1/projects/EXO/context", body, bearer(readToken)); w.Code != 403 {
		t.Fatalf("read-only token write: %d", w.Code)
	}

	// Bootstrap: compact, within budget, labels what is not verified; search finds it with a snippet.
	if w := e.do("POST", "/api/v1/projects/EXO/context", map[string]any{"category": "pitfall", "title": "Pinned and unverified", "summary": "s", "pinned": true}, asMember); w.Code != 201 {
		t.Fatalf("second entry: %d", w.Code)
	}
	w = e.do("GET", "/api/v1/projects/EXO/context/bootstrap?max_bytes=4096", nil, asViewer)
	if w.Code != 200 || w.Body.Len() > 4096 || strings.Contains(w.Body.String(), `"body"`) || !strings.Contains(w.Body.String(), `"state":"unverified"`) {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/projects/EXO/context/search", nil, asViewer); w.Code != 400 {
		t.Fatalf("search without q: %d", w.Code)
	}
	w = e.do("GET", "/api/v1/projects/EXO/context/search?q=rotation", nil, asViewer)
	if !strings.Contains(w.Body.String(), `"snippet"`) || !strings.Contains(w.Body.String(), "file:docs/security.md") {
		t.Fatalf("search: %s", w.Body)
	}
	if w := e.do("GET", "/api/v1/projects/OTH/context/search?q=rotation", nil, asMember); w.Code != 404 {
		t.Fatalf("search in a project the user is not in: %d", w.Code)
	}

	// Supersede, archive: nothing is deleted, the audit log has actor and operation.
	if w := e.do("POST", "/api/v1/projects/EXO/context/session-handling/supersede", map[string]any{"by": "only-once"}, asMember); w.Code != 200 {
		t.Fatalf("supersede: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", "/api/v1/projects/EXO/context/only-once", nil, asViewer); w.Code != 403 {
		t.Fatalf("viewer archive: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/projects/EXO/context/only-once", nil, asMember); w.Code != 200 {
		t.Fatalf("archive: %d", w.Code)
	}
	audit := e.do("GET", "/api/v1/admin/audit?operation=context.", nil, kees)
	if !strings.Contains(audit.Body.String(), `"context.verify"`) || !strings.Contains(audit.Body.String(), "human:user-") {
		t.Fatalf("audit: %s", audit.Body)
	}
}

func TestMCPContextTools(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, kees)
	}
	e.do("POST", "/api/v1/projects/OTH/context", map[string]any{"key": "secret-plan", "category": "decision", "title": "Other project secret", "summary": "must never leak"}, kees)
	_, rw, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, []string{"EXO"}, nil)
	_, ro, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, []string{"EXO"}, nil)
	_, none, _ := e.auth.CreateToken(1, "none", []string{auth.ScopeWrite}, []string{"EXO"}, nil)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	connect := func(token string) *sdk.ClientSession {
		s, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), &sdk.StreamableClientTransport{
			Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{token}},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	call := func(s *sdk.ClientSession, tool string, args map[string]any) (string, bool) {
		t.Helper()
		res, err := s.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(*sdk.TextContent).Text, res.IsError
	}
	agent, reader := connect(rw), connect(ro)

	tools, _ := agent.ListTools(context.Background(), nil)
	catalog, _ := json.Marshal(tools.Tools)
	ctxBytes := 0
	for _, tl := range tools.Tools {
		if strings.HasPrefix(tl.Name, "context_") {
			b, _ := json.Marshal(tl)
			ctxBytes += len(b)
		}
	}
	t.Logf("tool catalog: %d tools, %d bytes in total, %d bytes for the context tools", len(tools.Tools), len(catalog), ctxBytes)

	// An agent writes: entries start unverified; sources are kept; an update needs the version.
	text, isErr := call(agent, "context_write", map[string]any{"project": "EXO", "category": "convention", "title": "Migration naming", "pinned": true,
		"summary": "Migrations are numbered 0001_name.sql and never edited after release.",
		"sources": []any{map[string]any{"type": "file", "path": "migrations/embed.go", "content_hash": "sha256:aa"}}, "idempotency_key": "mig-1"})
	if isErr || !strings.Contains(text, `"key":"migration-naming"`) || !strings.Contains(text, `"state":"unverified"`) || !strings.Contains(text, `"context_version":1`) {
		t.Fatalf("write: %s", text)
	}
	if again, _ := call(agent, "context_write", map[string]any{"project": "EXO", "category": "convention", "title": "Migration naming", "pinned": true,
		"summary": "Migrations are numbered 0001_name.sql and never edited after release.",
		"sources": []any{map[string]any{"type": "file", "path": "migrations/embed.go", "content_hash": "sha256:aa"}}, "idempotency_key": "mig-1"}); !strings.Contains(again, `"replayed":true`) {
		t.Fatalf("replay: %s", again)
	}
	text, isErr = call(agent, "context_write", map[string]any{"project": "EXO", "key": "migration-naming", "summary": "x"})
	if !isErr || !strings.Contains(text, "expected_version") {
		t.Fatalf("update without version: %s", text)
	}
	text, isErr = call(agent, "context_write", map[string]any{"project": "EXO", "key": "migration-naming", "summary": "x", "expected_version": 5})
	if !isErr || !strings.Contains(text, `"code":"conflict"`) || !strings.Contains(text, `"version":1`) {
		t.Fatalf("conflict shape: %s", text)
	}
	if text, isErr = call(agent, "context_write", map[string]any{"project": "EXO", "op": "verify", "key": "migration-naming"}); !isErr || !strings.Contains(text, "forbidden") {
		t.Fatalf("token verify: %s", text)
	}
	if text, isErr = call(agent, "context_write", map[string]any{"project": "OTH", "category": "pitfall", "title": "x", "summary": "y"}); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("write outside the token's projects: %s", text)
	}
	if text, isErr = call(reader, "context_write", map[string]any{"project": "EXO", "category": "pitfall", "title": "x", "summary": "y"}); !isErr || !strings.Contains(text, "forbidden") {
		t.Fatalf("read-only token write: %s", text)
	}
	if log := e.auditEntries(t, "context.create"); !strings.Contains(log, `"actor":"token:pat-`) || !strings.Contains(log, `"batch_id":"`) {
		t.Fatalf("audit entry of the agent's create lacks actor or batch id: %s", log)
	}

	// Bootstrap is compact and valid JSON; unverified is labelled; references list the sources.
	text, isErr = call(reader, "context_get", map[string]any{"project": "EXO"})
	var boot struct {
		Project        string
		ContextVersion int `json:"context_version"`
		Entries        []map[string]any
		References     []string
	}
	if err := json.Unmarshal([]byte(text), &boot); err != nil || isErr || len(boot.Entries) != 1 || boot.Entries[0]["state"] != "unverified" ||
		boot.Entries[0]["body"] != nil || len(boot.References) != 1 || boot.References[0] != "file:migrations/embed.go" || len(text) > 4096 {
		t.Fatalf("bootstrap: %s", text)
	}
	if text, isErr = call(reader, "context_get", map[string]any{"project": "OTH"}); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("bootstrap of a foreign project: %s", text)
	}
	if text, isErr = call(connect(none), "context_get", map[string]any{"project": "EXO"}); !isErr || !strings.Contains(text, "forbidden") {
		t.Fatalf("token without read: %s", text)
	}

	// Search keeps to the project and returns snippets plus refs, never bodies.
	text, _ = call(reader, "context_get", map[string]any{"project": "EXO", "mode": "search", "query": "numbered"})
	if !strings.Contains(text, `"snippet"`) || !strings.Contains(text, "file:migrations/embed.go") || strings.Contains(text, `"body"`) {
		t.Fatalf("search: %s", text)
	}
	if text, _ = call(reader, "context_get", map[string]any{"project": "EXO", "mode": "search", "query": "leak"}); strings.Contains(text, "secret-plan") {
		t.Fatalf("search leaked another project: %s", text)
	}
	if _, isErr = call(reader, "context_get", map[string]any{"project": "EXO", "mode": "search"}); !isErr {
		t.Fatal("search without a query")
	}

	// Detail: full entry with body and sources; a budget pages instead of cutting the JSON.
	for i := 0; i < 4; i++ {
		call(agent, "context_write", map[string]any{"project": "EXO", "category": "reference", "title": "Doc " + strconv.Itoa(i), "summary": "s", "body": strings.Repeat("d", 700)})
	}
	text, _ = call(reader, "context_get", map[string]any{"project": "EXO", "mode": "detail", "keys": []any{"doc-0", "doc-1", "doc-2", "doc-3"}, "max_bytes": 1500})
	var page struct {
		Items      []map[string]any
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(text), &page); err != nil || len(page.Items) == 0 || len(page.Items) == 4 || page.NextCursor == "" || len(text) > 1500 || page.Items[0]["body"] == nil {
		t.Fatalf("detail page: %s", text)
	}
	if text, isErr = call(reader, "context_get", map[string]any{"project": "EXO", "mode": "detail", "keys": []any{"doc-0"}, "fields": []any{"key", "nope"}}); !isErr {
		t.Fatalf("unknown field accepted: %s", text)
	}

	// The same bootstrap as a resource.
	rr, err := reader.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "atlas://projects/EXO/context"})
	if err != nil || len(rr.Contents) != 1 || !strings.Contains(rr.Contents[0].Text, "migration-naming") {
		t.Fatalf("resource: %v %+v", err, rr)
	}
	if _, err := reader.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "atlas://projects/OTH/context"}); err == nil {
		t.Fatal("resource of another project was readable")
	}
}

// auditEntries returns the raw audit entries of an operation prefix.
func (e env) auditEntries(t *testing.T, operation string) string {
	t.Helper()
	return e.do("GET", "/api/v1/admin/audit?operation="+operation, nil, sessionOf(e, t)).Body.String()
}

func TestSSEContextUpdatedFollowsProjectMembership(t *testing.T) {
	f := realtimeFixture(t)
	frames := stream(t, f.srv.URL, f.aliceToken, "") // alice is a member of EXO only
	next(t, frames, 2*time.Second)                   // hello

	f.adminDo("POST", "/api/v1/projects/OTH/context", map[string]any{"category": "pitfall", "title": "Hidden", "summary": "s"})
	f.adminDo("POST", "/api/v1/projects/EXO/context", map[string]any{"category": "pitfall", "title": "Visible", "summary": "s"})

	got, ok := next(t, frames, 3*time.Second)
	if !ok {
		t.Fatal("no event received")
	}
	var ev struct{ Type, Project, Key string }
	_ = json.Unmarshal([]byte(got.Data), &ev)
	if ev.Type != "context.updated" || ev.Project != "EXO" || ev.Key != "visible" {
		t.Fatalf("event %+v", ev)
	}
	if extra, ok := next(t, frames, 400*time.Millisecond); ok {
		t.Fatalf("leaked event: %+v", extra)
	}
}

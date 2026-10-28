package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestCommentsAndChecklistREST(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, asUser)
	_, read, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, nil, nil)
	asReader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+read) }

	if w := e.do("POST", "/api/v1/items/EXO-1/comments", map[string]any{"body": "hello **world**"}, asUser); w.Code != 201 {
		t.Fatalf("comment: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/items/EXO-1/comments", map[string]any{"body": "x"}, asReader); w.Code != 403 {
		t.Fatalf("read-only token commented: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/items/EXO-1/comments", map[string]any{"body": "  "}, asUser); w.Code != 400 {
		t.Fatalf("empty comment: %d", w.Code)
	}
	list := decodeBody[struct{ Items []map[string]any }](t, e.do("GET", "/api/v1/items/EXO-1/comments", nil, asReader))
	if len(list.Items) != 1 || list.Items[0]["body"] != "hello **world**" {
		t.Fatalf("comments: %v", list.Items)
	}

	for _, text := range []string{"one", "two"} {
		if w := e.do("POST", "/api/v1/items/EXO-1/checklist", map[string]any{"text": text}, asUser); w.Code != 201 {
			t.Fatalf("add: %d %s", w.Code, w.Body)
		}
	}
	if w := e.do("PATCH", "/api/v1/items/EXO-1/checklist/2", map[string]any{"done": true}, asUser); w.Code != 200 {
		t.Fatalf("tick: %d %s", w.Code, w.Body)
	}
	if w := e.do("PATCH", "/api/v1/items/EXO-1/checklist/2", map[string]any{"done": false}, asReader); w.Code != 403 {
		t.Fatalf("read-only token ticked: %d", w.Code)
	}
	// An id of another item must not be reachable through this item's URL.
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t2"}, asUser)
	if w := e.do("PATCH", "/api/v1/items/EXO-2/checklist/2", map[string]any{"done": true}, asUser); w.Code != 404 {
		t.Fatalf("cross-item checklist edit: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/items/EXO-1/checklist/1", nil, asUser); w.Code != 204 {
		t.Fatalf("remove: %d", w.Code)
	}
	cl := decodeBody[struct{ Items []map[string]any }](t, e.do("GET", "/api/v1/items/EXO-1/checklist", nil, asReader))
	if len(cl.Items) != 1 || cl.Items[0]["done"] != true {
		t.Fatalf("checklist: %v", cl.Items)
	}
}

func TestMilestones(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "a"}, asUser)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "b"}, asUser)

	// Only a project admin manages milestones; a member can assign items to them.
	member, err := e.auth.CreateUser("m@example.com", "Mem", "correct-horse", false)
	if err != nil {
		t.Fatal(err)
	}
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(member.ID, 10), map[string]any{"role": "member"}, asUser)
	_, memTok, _ := e.auth.CreateToken(member.ID, "m", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	asMember := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+memTok) }
	if w := e.do("POST", "/api/v1/projects/EXO/milestones", map[string]any{"name": "v1"}, asMember); w.Code != 403 {
		t.Fatalf("member created a milestone: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/milestones", map[string]any{"name": "v1.0"}, asUser); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/projects/EXO/milestones", map[string]any{"name": "V1.0"}, asUser); w.Code != 422 {
		t.Fatalf("duplicate name (case-insensitive): %d", w.Code)
	}

	// Assign, filter, and a milestone in use cannot be deleted.
	if w := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "milestone": "v1.0"}, asMember); w.Code != 200 {
		t.Fatalf("assign: %d %s", w.Code, w.Body)
	}
	if w := e.do("PATCH", "/api/v1/items/EXO-2", map[string]any{"version": 1, "milestone": "nope"}, asMember); w.Code != 400 {
		t.Fatalf("unknown milestone: %d", w.Code)
	}
	page := decodeBody[struct{ Items []map[string]any }](t, e.do("GET", "/api/v1/items?project=EXO&milestone=v1.0&fields=key,milestone", nil, asMember))
	if len(page.Items) != 1 || page.Items[0]["key"] != "EXO-1" || page.Items[0]["milestone"] != "v1.0" {
		t.Fatalf("filter: %v", page.Items)
	}
	list := decodeBody[struct{ Items []map[string]any }](t, e.do("GET", "/api/v1/projects/EXO/milestones", nil, asMember))
	if len(list.Items) != 1 || list.Items[0]["total"] != float64(1) || list.Items[0]["done"] != float64(0) {
		t.Fatalf("counts: %v", list.Items)
	}
	if w := e.do("DELETE", "/api/v1/projects/EXO/milestones/1", nil, asUser); w.Code != 422 {
		t.Fatalf("deleted a used milestone: %d", w.Code)
	}
	e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 2, "milestone": nil}, asMember)
	if w := e.do("PATCH", "/api/v1/projects/EXO/milestones/1", map[string]any{"released": true}, asUser); w.Code != 200 {
		t.Fatalf("release: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", "/api/v1/projects/EXO/milestones/1", nil, asUser); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestNotifications(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t) // admin = the actor
	asAdmin := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asAdmin)
	for _, title := range []string{"blocker", "blocked", "followed"} {
		e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": title}, asAdmin)
	}
	bob, _ := e.auth.CreateUser("bob@example.com", "Bob", "correct-horse", false)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(bob.ID, 10), map[string]any{"role": "member"}, asAdmin)
	_, tok, _ := e.auth.CreateToken(bob.ID, "bob", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	asBob := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	_, readTok, _ := e.auth.CreateToken(bob.ID, "bob-ro", []string{auth.ScopeRead}, nil, nil)
	asBobRO := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+readTok) }

	inbox := func(mut func(*http.Request)) (items []map[string]any, unread float64) {
		r := decodeBody[struct {
			Items  []map[string]any
			Unread float64
		}](t, e.do("GET", "/api/v1/notifications", nil, mut))
		return r.Items, r.Unread
	}
	types := func(items []map[string]any) []string {
		var out []string
		for _, n := range items {
			out = append(out, n["type"].(string)+":"+n["item"].(string))
		}
		return out
	}

	// Assigned: Bob is told; the admin acting on their own changes is not.
	e.do("PATCH", "/api/v1/items/EXO-2", map[string]any{"version": 1, "assignee": bob.ID}, asAdmin)
	// Followed: Bob follows EXO-3 and is told about a status change there.
	if w := e.do("PUT", "/api/v1/items/EXO-3/follow", nil, asBobRO); w.Code != 403 {
		t.Fatalf("read-only token followed: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/items/EXO-3/follow", nil, asBob); w.Code != 204 {
		t.Fatalf("follow: %d", w.Code)
	}
	e.do("PATCH", "/api/v1/items/EXO-3", map[string]any{"version": 1, "status": "Todo"}, asAdmin)
	// Mention in a comment.
	e.do("POST", "/api/v1/items/EXO-3/comments", map[string]any{"body": "ping @Bob please check"}, asAdmin)
	// Blocker done: EXO-1 blocks EXO-2 (assigned to Bob); moving EXO-1 to Done tells Bob.
	e.do("POST", "/api/v1/items/EXO-1/links", map[string]any{"type": "blocks", "target": "EXO-2"}, asAdmin)
	e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "status": "Done"}, asAdmin)

	items, unread := inbox(asBob)
	got := types(items)
	want := map[string]bool{"assigned:EXO-2": true, "followed:EXO-3": true, "mention:EXO-3": true, "blocker_done:EXO-2": true}
	for _, g := range got {
		delete(want, g)
	}
	if len(want) != 0 || unread != float64(len(items)) {
		t.Fatalf("inbox %v (unread %v), missing %v", got, unread, want)
	}
	if adminItems, _ := inbox(asAdmin); len(adminItems) != 0 {
		t.Fatalf("admin notified of own actions: %v", types(adminItems))
	}

	// The notification signal reaches only its recipient.
	feed := decodeBody[struct{ Events []map[string]any }](t, e.do("GET", "/api/v1/changes?since=0", nil, asBob))
	adminFeed := decodeBody[struct{ Events []map[string]any }](t, e.do("GET", "/api/v1/changes?since=0", nil, asAdmin))
	count := func(ev []map[string]any) (n int) {
		for _, x := range ev {
			if x["type"] == "notification.created" {
				n++
			}
		}
		return
	}
	if count(feed.Events) != len(items) || count(adminFeed.Events) != 0 {
		t.Fatalf("notification events: bob %d admin %d", count(feed.Events), count(adminFeed.Events))
	}

	// Read state is the owner's: others cannot touch it; read and delete work for the owner.
	id := strconv.Itoa(int(items[0]["id"].(float64)))
	if w := e.do("POST", "/api/v1/notifications/"+id+"/read", nil, asAdmin); w.Code != 404 {
		t.Fatalf("read someone else's notification: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/notifications/"+id+"/read", nil, asBobRO); w.Code != 403 {
		t.Fatalf("read-only token marked read: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/notifications/"+id+"/read", nil, asBob); w.Code != 204 {
		t.Fatalf("mark read: %d", w.Code)
	}
	if _, unreadNow := inbox(asBob); unreadNow != unread-1 {
		t.Fatalf("unread after one read: %v", unreadNow)
	}
	if w := e.do("DELETE", "/api/v1/notifications/"+id, nil, asBob); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/notifications/read", nil, asBob); w.Code != 204 {
		t.Fatalf("read all: %d", w.Code)
	}
	if _, unreadNow := inbox(asBob); unreadNow != 0 {
		t.Fatalf("unread after read-all: %v", unreadNow)
	}
}

// TestExportImportRoundTrip: an export imports into another project; the dry run writes nothing and reports
// duplicates, errors and the key mapping; a failing row stops the whole import.
func TestExportImportRoundTrip(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	for _, k := range []string{"SRC", "DST"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, asUser)
	}
	e.do("POST", "/api/v1/items", map[string]any{"project": "SRC", "type": "epic", "title": "Platform", "labels": []string{"core"}}, asUser)
	e.do("POST", "/api/v1/items", map[string]any{"project": "SRC", "type": "story", "title": "Login", "parent": "SRC-1"}, asUser)
	e.do("POST", "/api/v1/items/SRC-1/comments", map[string]any{"body": "hello"}, asUser)
	e.do("POST", "/api/v1/projects/SRC/context", map[string]any{"category": "architecture", "title": "Exported note", "summary": "In the export."}, asUser)

	w := e.do("GET", "/api/v1/projects/SRC/export", nil, asUser)
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	exported := w.Body.Bytes()
	if !strings.Contains(string(exported), `"hello"`) || !strings.Contains(string(exported), `"Exported note"`) {
		t.Fatalf("export misses comments or context: %s", exported)
	}
	if w := e.do("GET", "/api/v1/projects/SRC/export?format=csv", nil, asUser); w.Code != 200 || !strings.Contains(w.Body.String(), "SRC-2,story,Login") {
		t.Fatalf("csv export: %d %s", w.Code, w.Body)
	}

	post := func(path string, body []byte) map[string]any {
		r := httptest.NewRequest("POST", path, bytes.NewReader(body))
		asUser(r)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return decodeBody[map[string]any](t, rec)
	}
	rep := post("/api/v1/projects/DST/import?dry_run=1", exported)
	if rep["applied"] != false || rep["created"] != 2.0 {
		t.Fatalf("dry run: %v", rep)
	}
	if w := e.do("GET", "/api/v1/items/DST-1", nil, asUser); w.Code != 404 {
		t.Fatalf("dry run wrote an item: %d", w.Code)
	}
	rep = post("/api/v1/projects/DST/import", exported)
	mapping, _ := rep["mapping"].(map[string]any)
	if rep["applied"] != true || mapping["SRC-2"] != "DST-2" {
		t.Fatalf("import: %v", rep)
	}
	if w := e.do("GET", "/api/v1/items/DST-2", nil, asUser); !strings.Contains(w.Body.String(), `"parent":"DST-1"`) {
		t.Fatalf("parent not remapped: %s", w.Body)
	}
	// Importing again finds both rows as duplicates and creates nothing.
	rep = post("/api/v1/projects/DST/import", exported)
	if dups, _ := rep["duplicates"].([]any); len(dups) != 2 || rep["created"] != 0.0 {
		t.Fatalf("duplicates: %v", rep)
	}
	// One bad row (unknown status) rejects the whole file.
	bad := []byte(`{"items":[{"type":"task","title":"fine"},{"type":"task","title":"broken","status":"Nope"}]}`)
	rep = post("/api/v1/projects/DST/import", bad)
	if errs, _ := rep["errors"].([]any); rep["applied"] != false || len(errs) != 1 {
		t.Fatalf("bad row: %v", rep)
	}
	if w := e.do("GET", "/api/v1/items?project=DST&q=fine", nil, asUser); strings.Contains(w.Body.String(), `"fine"`) {
		t.Fatalf("partial import leaked: %s", w.Body)
	}
}

// TestBlockedCreateStatus: a status with allow_create=false refuses new items over REST and MCP, items can still be
// moved into it, and the last creatable status cannot be blocked.
func TestBlockedCreateStatus(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, asUser)
	wf := decodeBody[struct {
		Statuses []struct {
			ID   int64
			Name string
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/workflow", nil, asUser))
	byName := map[string]int64{}
	for _, s := range wf.Statuses {
		byName[s.Name] = s.ID
	}
	patch := func(name string, allow bool) int {
		return e.do("PATCH", "/api/v1/projects/EXO/statuses/"+strconv.FormatInt(byName[name], 10), map[string]any{"allow_create": allow}, asUser).Code
	}
	if c := patch("Review", false); c != 200 {
		t.Fatalf("block Review: %d", c)
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "x", "status": "Review"}, asUser); w.Code != 422 {
		t.Fatalf("create in blocked status: %d %s", w.Code, w.Body)
	}
	// MCP gives the same refusal.
	_, tok, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	w := mcpCall(e, tok, "items_write", map[string]any{"operations": []any{map[string]any{"op": "create", "project": "EXO", "type": "task", "title": "x", "status": "Review"}}})
	var r rpcResult
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if !r.Result.IsError || !strings.Contains(r.Result.Content[0].Text, "rule_violation") {
		t.Fatalf("mcp create in blocked status: %s", w.Body)
	}
	// Creating elsewhere and moving into the blocked status still works.
	it := decodeBody[map[string]any](t, e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "y"}, asUser))
	if w := e.do("POST", "/api/v1/items/"+it["key"].(string)+"/move", map[string]any{"version": it["version"], "status": "Review"}, asUser); w.Code != 200 {
		t.Fatalf("move into blocked status: %d %s", w.Code, w.Body)
	}
	// The default (first) status blocked: a clear error, no silent fallback.
	if c := patch("Backlog", false); c != 200 {
		t.Fatalf("block Backlog: %d", c)
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "z"}, asUser); w.Code != 422 {
		t.Fatalf("create in blocked default status: %d", w.Code)
	}
	// Block everything else; the last creatable one is protected.
	for _, n := range []string{"Todo", "In Progress"} {
		if c := patch(n, false); c != 200 {
			t.Fatalf("block %s: %d", n, c)
		}
	}
	if c := patch("Done", false); c != 422 {
		t.Fatalf("blocking the last creatable status: %d", c)
	}
}

package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
)

type sseFrame struct {
	Event string
	ID    string
	Data  string
}

// stream opens an SSE connection and delivers parsed frames on a channel.
func stream(t *testing.T, url, token, lastID string) <-chan sseFrame {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("events: %d", resp.StatusCode)
	}
	frames := make(chan sseFrame, 64)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var f sseFrame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if f != (sseFrame{}) {
					frames <- f
				}
				f = sseFrame{}
			case strings.HasPrefix(line, "event: "):
				f.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "id: "):
				f.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				f.Data = strings.TrimPrefix(line, "data: ")
			}
		}
		close(frames)
	}()
	return frames
}

func next(t *testing.T, frames <-chan sseFrame, wait time.Duration) (sseFrame, bool) {
	t.Helper()
	select {
	case f, ok := <-frames:
		return f, ok
	case <-time.After(wait):
		return sseFrame{}, false
	}
}

type fixture struct {
	e                      env
	srv                    *httptest.Server
	adminDo                func(method, path string, body any) *httptest.ResponseRecorder
	aliceToken, adminToken string
}

func realtimeFixture(t *testing.T) fixture {
	t.Helper()
	e := setup(t)
	cookie, csrf := e.login(t)
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		return e.do(method, path, body, func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) })
	}
	for _, k := range []string{"EXO", "OTH"} {
		if w := do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}); w.Code != 201 {
			t.Fatalf("project %s: %d %s", k, w.Code, w.Body)
		}
	}
	alice, err := e.auth.CreateUser("alice@x.io", "Alice", "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if w := do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(alice.ID, 10), map[string]any{"role": "member"}); w.Code != 204 {
		t.Fatalf("member: %d %s", w.Code, w.Body)
	}
	_, at, _ := e.auth.CreateToken(alice.ID, "alice", []string{auth.ScopeRead}, nil, nil)
	_, admin, _ := e.auth.CreateToken(1, "admin", []string{auth.ScopeRead}, nil, nil)
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	return fixture{e: e, srv: srv, adminDo: do, aliceToken: at, adminToken: admin}
}

func (f fixture) createItem(t *testing.T, project, title string) {
	t.Helper()
	w := f.adminDo("POST", "/api/v1/items", map[string]any{"project": project, "type": "task", "title": title})
	if w.Code != 201 {
		t.Fatalf("create item: %d %s", w.Code, w.Body)
	}
}

func TestSSEDeliversCommittedEventsFilteredByPermission(t *testing.T) {
	f := realtimeFixture(t)
	frames := stream(t, f.srv.URL, f.aliceToken, "")
	if hello, ok := next(t, frames, 2*time.Second); !ok || hello.Event != "hello" || !strings.Contains(hello.Data, "generation") {
		t.Fatalf("expected hello, got %+v", hello)
	}

	f.createItem(t, "OTH", "alice must not see this") // OTH-1: no access
	f.createItem(t, "EXO", "alice sees this")         // EXO-1

	got, ok := next(t, frames, 3*time.Second)
	if !ok {
		t.Fatal("no event received")
	}
	var ev struct {
		Seq     int64
		Type    string
		Project string
		Key     string
	}
	_ = json.Unmarshal([]byte(got.Data), &ev)
	if ev.Key != "EXO-1" || ev.Type != "item.created" || got.ID != strconv.FormatInt(ev.Seq, 10) {
		t.Fatalf("event %+v (id %s)", ev, got.ID)
	}
	// Nothing about OTH may follow.
	if extra, ok := next(t, frames, 400*time.Millisecond); ok {
		t.Fatalf("leaked event: %+v", extra)
	}
}

func TestSSEResumesFromLastEventIDAndResyncsWhenCursorIsUnusable(t *testing.T) {
	f := realtimeFixture(t)
	f.createItem(t, "EXO", "one") // seq after project/member events
	f.createItem(t, "EXO", "two")

	// Catch-up: everything after the given id is replayed, in order.
	w := f.e.do("GET", "/api/v1/changes?since=0", nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+f.adminToken) })
	var all struct {
		Events []struct {
			Seq int64
			Key string
		}
		Seq int64
	}
	_ = json.Unmarshal(w.Body.Bytes(), &all)
	if w.Code != 200 || len(all.Events) < 2 {
		t.Fatalf("changes: %d %s", w.Code, w.Body)
	}
	last := all.Events[len(all.Events)-1]
	before := strconv.FormatInt(last.Seq-1, 10)

	frames := stream(t, f.srv.URL, f.adminToken, before)
	if h, _ := next(t, frames, 2*time.Second); h.Event != "hello" {
		t.Fatalf("expected hello, got %+v", h)
	}
	replay, ok := next(t, frames, 2*time.Second)
	if !ok || replay.ID != strconv.FormatInt(last.Seq, 10) {
		t.Fatalf("expected replay of seq %d, got %+v", last.Seq, replay)
	}

	// A cursor beyond the journal (database replaced) needs a full resync.
	frames = stream(t, f.srv.URL, f.adminToken, "99999999")
	next(t, frames, 2*time.Second) // hello
	if r, ok := next(t, frames, 2*time.Second); !ok || r.Event != "resync_required" {
		t.Fatalf("expected resync_required, got %+v", r)
	}
}

func TestSSERequiresAuthentication(t *testing.T) {
	f := realtimeFixture(t)
	resp, err := http.Get(f.srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous stream: %d", resp.StatusCode)
	}
}

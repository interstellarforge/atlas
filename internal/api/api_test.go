package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/kees/atlas/internal/api"
	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/events"
	"github.com/kees/atlas/internal/mcp"
	"github.com/kees/atlas/internal/media"
	"github.com/kees/atlas/migrations"
)

type env struct {
	h    http.Handler
	auth *auth.Service
}

func setup(t *testing.T) env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(d)
	if _, err := a.CreateUser("kees@example.com", "Kees", "correct-horse", true); err != nil {
		t.Fatal(err)
	}
	spa := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	svc := core.New(d, a)
	feed := events.New(d, svc)
	svc.SetNotifier(feed.Hub().Notify)
	const maxUpload = 200 << 10
	store := media.NewStore(t.TempDir(), media.Limits{MaxBytes: maxUpload, MaxPixels: 1_000_000})
	svc.SetMedia(store)
	svc.SetRepoFetcher(fakeRepo{})
	stats := mcp.NewMetrics()
	h := api.New(api.Deps{DB: d, Auth: a, Core: svc, Feed: feed, SPA: spa, MCP: mcp.Handler(svc, feed, mcp.Options{WritesPerHour: 500, Metrics: stats}), MCPStats: stats, Media: store, MaxUpload: maxUpload}).Handler()
	return env{h: h, auth: a}
}

func (e env) do(method, path string, body any, mut func(*http.Request)) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	if mut != nil {
		mut(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e env) login(t *testing.T) (cookie *http.Cookie, csrf string) {
	t.Helper()
	w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "correct-horse"}, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var out struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Result().Cookies()[0], out.CSRF
}

func TestAuthRequiredAndSessionCSRF(t *testing.T) {
	e := setup(t)

	if w := e.do("GET", "/api/v1/tokens", nil, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "wrong"}, nil); w.Code != 401 {
		t.Fatalf("bad password: %d", w.Code)
	}

	cookie, csrf := e.login(t)
	withCookie := func(r *http.Request) { r.AddCookie(cookie) }
	if w := e.do("GET", "/api/v1/tokens", nil, withCookie); w.Code != 200 {
		t.Fatalf("session read: %d", w.Code)
	}
	// Cookie auth without the CSRF header must not be able to mutate.
	body := map[string]any{"name": "ci", "scopes": []string{"read"}}
	if w := e.do("POST", "/api/v1/tokens", body, withCookie); w.Code != 401 {
		t.Fatalf("missing csrf: %d", w.Code)
	}
	withCSRF := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	if w := e.do("POST", "/api/v1/tokens", body, withCSRF); w.Code != 201 {
		t.Fatalf("with csrf: %d", w.Code)
	}
}

func TestTokenScopesRevocationAndNoMinting(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }

	mint := func(scopes ...string) (secret string, id float64) {
		w := e.do("POST", "/api/v1/tokens", map[string]any{"name": "t", "scopes": scopes}, asUser)
		if w.Code != 201 {
			t.Fatalf("mint: %d %s", w.Code, w.Body)
		}
		var out struct {
			Token  map[string]any `json:"token"`
			Secret string         `json:"secret"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.Secret, out.Token["id"].(float64)
	}
	bearer := func(s string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+s) }
	}

	readTok, readID := mint(auth.ScopeRead)
	if w := e.do("GET", "/api/v1/auth/me", nil, bearer(readTok)); w.Code != 200 {
		t.Fatalf("read token on GET: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/logout", nil, bearer(readTok)); w.Code != 403 {
		t.Fatalf("read-only token on write: %d", w.Code)
	}

	writeTok, _ := mint(auth.ScopeRead, auth.ScopeWrite)
	if w := e.do("POST", "/api/v1/tokens", map[string]any{"name": "x", "scopes": []string{"read"}}, bearer(writeTok)); w.Code != 403 {
		t.Fatalf("token minting a token: %d", w.Code)
	}

	// Revocation is immediate, no restart.
	if w := e.do("DELETE", "/api/v1/tokens/"+itoa(int64(readID)), nil, asUser); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, bearer(readTok)); w.Code != 401 {
		t.Fatalf("revoked token: %d", w.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := setup(t)
	bad := map[string]string{"email": "kees@example.com", "password": "nope"}
	for i := 0; i < 5; i++ {
		e.do("POST", "/api/v1/auth/login", bad, nil)
	}
	good := map[string]string{"email": "kees@example.com", "password": "correct-horse"}
	if w := e.do("POST", "/api/v1/auth/login", good, nil); w.Code != 429 {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}

func TestHealthAndSPAFallback(t *testing.T) {
	e := setup(t)
	if w := e.do("GET", "/api/v1/health", nil, nil); w.Code != 200 {
		t.Fatalf("health: %d", w.Code)
	}
	if w := e.do("GET", "/board/EXO", nil, nil); w.Code != 200 || w.Body.String() != "<html></html>" {
		t.Fatalf("spa fallback: %d %q", w.Code, w.Body)
	}
	if w := e.do("GET", "/.well-known/other", nil, nil); w.Code != 404 {
		t.Fatalf("well-known: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/nope", nil, nil); w.Code != 404 {
		t.Fatalf("unknown api: %d", w.Code)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

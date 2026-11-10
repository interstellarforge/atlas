package api_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/db"
)

// ATL-480 / ADR-0030: a pending, rejected or suspended account reaches nothing on any authentication path,
// even with a session or token that was issued while it was active; only login after correct credentials says why.
func TestNonActiveAccountsAreLockedOut(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	other, err := db.Open(e.path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	unknown := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "nobody@example.com", "password": "password-123"}, nil)
	for _, state := range []string{"pending", "rejected", "suspended"} {
		email := state + "@example.com"
		u, err := e.auth.CreateUser(email, state, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		session := loginAs(e, t, email, "password-123")
		_, secret, err := e.auth.CreateToken(u.ID, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
		if w := e.do("GET", "/api/v1/projects", nil, session); w.Code != 200 {
			t.Fatalf("%s: active session before the change: %d", state, w.Code)
		}
		if _, err := other.W.Exec(`UPDATE users SET status = ? WHERE id = ?`, state, u.ID); err != nil {
			t.Fatal(err)
		}

		for name, as := range map[string]func(*http.Request){"session": session, "token": bearer} {
			for _, p := range []string{"/api/v1/projects", "/api/v1/tokens", "/api/v1/events"} {
				if w := e.do("GET", p, nil, as); w.Code != 401 {
					t.Errorf("%s %s: GET %s answers %d, want 401", state, name, p, w.Code)
				}
			}
		}
		if w := e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, bearer); w.Code != 401 {
			t.Errorf("%s: MCP answers %d, want 401", state, w.Code)
		}

		w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "password-123"}, nil)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "account_"+state) {
			t.Errorf("%s: login with correct credentials: %d %s", state, w.Code, w.Body)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: login set a session cookie", state)
		}
		wrong := e.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "wrong-password"}, nil)
		if wrong.Code != 401 || wrong.Body.String() != unknown.Body.String() {
			t.Errorf("%s: wrong password differs from an unknown user: %d %q vs %d %q", state, wrong.Code, wrong.Body, unknown.Code, unknown.Body)
		}

		// Approval grants no access: the account is active again, and still sees nothing of EXO.
		if _, err := other.W.Exec(`UPDATE users SET status = 'active' WHERE id = ?`, u.ID); err != nil {
			t.Fatal(err)
		}
		again := loginAs(e, t, email, "password-123")
		if w := e.do("GET", "/api/v1/projects/EXO", nil, again); w.Code != 404 {
			t.Errorf("%s: an approved account reads EXO: %d", state, w.Code)
		}
	}
}

// ADR-0031: registration creates an ordinary account without any access; a taken email answers like a new one.
func TestFreeAccountRegistration(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	setMode := func(mode, domain string) {
		w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": mode, "allowed_domain": domain, "session_days": 14, "done_visible_days": 14}, kees)
		if w.Code != 200 {
			t.Fatalf("policy %s: %d %s", mode, w.Code, w.Body)
		}
	}
	reg := func(email string) (int, string) {
		w := e.do("POST", "/api/v1/auth/register", map[string]any{"email": email, "name": email, "password": "password-123"}, nil)
		return w.Code, w.Body.String()
	}
	if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": "request", "session_days": 14}, kees); w.Code != 400 {
		t.Fatalf("the retired registration mode request: %d, want 400", w.Code)
	}
	if c, _ := reg("closed@example.com"); c != 403 {
		t.Fatalf("registration while closed: %d", c)
	}
	setMode("open", "example.com")
	if c, _ := reg("a@elsewhere.org"); c != 422 {
		t.Fatalf("wrong domain: %d", c)
	}
	c, body := reg("new@example.com")
	if c != 201 || !strings.Contains(body, `"id":0`) || strings.Contains(body, "status") {
		t.Fatalf("registration: %d %s", c, body)
	}
	// the new account is active at once and reaches nothing of an organization it is not part of
	newbie := loginAs(e, t, "new@example.com", "password-123")
	if w := e.do("GET", "/api/v1/projects/EXO", nil, newbie); w.Code != 404 {
		t.Errorf("a new account reads EXO: %d", w.Code)
	}
	// an account without any organization gets an empty list, not an error
	lonely, _ := e.auth.CreateUser("lonely@example.com", "Lonely", "password-123", false)
	if err := e.svc.RemoveOrgMember(context.Background(), auth.Actor{Kind: auth.KindHuman, UserID: 1}, "MAIN", lonely.ID, false); err != nil {
		t.Fatal(err)
	}
	if w := e.do("GET", "/api/v1/organizations", nil, loginAs(e, t, "lonely@example.com", "password-123")); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("organizations of an account without one: %d %s", w.Code, w.Body)
	}
	// a taken email: the same answer, nothing changed
	c2, b2 := reg("NEW@example.com")
	c3, b3 := reg("kees@example.com")
	for _, got := range []struct {
		c int
		b string
	}{{c2, b2}, {c3, b3}} {
		if got.c != 201 || !strings.Contains(got.b, `"id":0`) {
			t.Errorf("a taken email answers %d %s", got.c, got.b)
		}
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "correct-horse"}, nil); w.Code != 200 {
		t.Fatalf("the existing account was changed by a registration: %d", w.Code)
	}
}

// ADR-0031: only the SUPERADMIN suspends or reactivates (human session); one decision wins a race; suspension cuts
// the session, the token and an open event stream and does not give a revoked token back.
func TestSuspension(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("plain@example.com", "Plain", "password-123", false)
	plain := loginAs(e, t, "plain@example.com", "password-123")
	adm, _ := e.auth.CreateUser("orgadmin@example.com", "Org admin", "password-123", false)
	e.orgAdmin(t, adm.ID)
	orgAdmin := loginAs(e, t, "orgadmin@example.com", "password-123")
	_, kt, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	superToken := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+kt) }
	target, _ := e.auth.CreateUser("target@example.com", "Target", "password-123", false)
	id := strconv.FormatInt(target.ID, 10)
	targetS := loginAs(e, t, "target@example.com", "password-123")
	_, secret, _ := e.auth.CreateToken(target.ID, "rw", []string{auth.ScopeRead}, nil, nil)
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
	patch := func(as func(*http.Request), uid string, active bool) int {
		return e.do("PATCH", "/api/v1/admin/users/"+uid, map[string]any{"active": active}, as).Code
	}

	for name, as := range map[string]func(*http.Request){"organization admin": orgAdmin, "plain user": plain, "superadmin token": superToken} {
		if c := patch(as, id, false); c != 403 {
			t.Errorf("%s suspends: %d, want 403", name, c)
		}
	}
	if w := e.do("GET", "/api/v1/projects", nil, bearer); w.Code != 200 {
		t.Fatalf("token before suspension: %d", w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	targetS(req)
	done := make(chan struct{})
	go func() { e.h.ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	time.Sleep(200 * time.Millisecond)

	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() { codes <- patch(kees, id, false) }()
	}
	got := []int{<-codes, <-codes}
	if !(got[0] == 204 && got[1] == 422) && !(got[0] == 422 && got[1] == 204) {
		t.Errorf("two concurrent suspensions answered %v, want one 204 and one 422", got)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("the event stream of a suspended account stayed open")
	}
	for name, as := range map[string]func(*http.Request){"session": targetS, "token": bearer} {
		if w := e.do("GET", "/api/v1/projects", nil, as); w.Code != 401 {
			t.Errorf("%s after suspension: %d, want 401", name, w.Code)
		}
	}
	if c := patch(kees, id, true); c != 204 {
		t.Fatalf("reactivate: %d", c)
	}
	if w := e.do("GET", "/api/v1/projects", nil, bearer); w.Code != 401 {
		t.Errorf("a revoked token works again after reactivation: %d", w.Code)
	}
	if c := patch(kees, "1", false); c != 422 {
		t.Errorf("suspending yourself: %d, want 422", c)
	}
}

// ATL-376: a sign-up cannot choose its own privileges, and a registration with a taken email changes nothing,
// also with an invite code (ADR-0031).
func TestRegistrationCannotEscalateOrTouchExistingAccounts(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	other, err := db.Open(e.path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	setMode := func(mode string) {
		if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": mode, "session_days": 14, "done_visible_days": 14}, kees); w.Code != 200 {
			t.Fatalf("policy %s: %d", mode, w.Code)
		}
	}
	reg := func(body map[string]any) *httptest.ResponseRecorder {
		return e.do("POST", "/api/v1/auth/register", body, nil)
	}

	// no privilege can be sent along: unknown fields are refused, known ones carry no privilege
	setMode("open")
	for _, extra := range []string{"is_superadmin", "status", "role", "organization"} {
		w := reg(map[string]any{"email": "evil@example.com", "name": "E", "password": "password-123", extra: "admin"})
		if w.Code != 400 {
			t.Errorf("register with %s: %d, want 400", extra, w.Code)
		}
	}
	if w := reg(map[string]any{"email": "evil@example.com", "name": "E", "password": "password-123"}); w.Code != 201 {
		t.Fatalf("register: %d", w.Code)
	}
	var super bool
	var status string
	if err := other.R.QueryRow(`SELECT is_superadmin, status FROM users WHERE email = 'evil@example.com'`).Scan(&super, &status); err != nil || super || status != "active" {
		t.Fatalf("a sign-up came out superadmin=%v status=%s (%v)", super, status, err)
	}

	// a registration invite for a taken email: same answer as for a new one, nothing consumed, nothing changed
	setMode("invite")
	w := e.do("POST", "/api/v1/admin/invites", map[string]any{"days": 3}, kees)
	code := decodeBody[struct{ Code string }](t, w).Code
	taken := reg(map[string]any{"email": "kees@example.com", "name": "X", "password": "attacker-pw-1", "invite": code})
	if taken.Code != 201 || !strings.Contains(taken.Body.String(), `"id":0`) {
		t.Errorf("taken email with a valid invite: %d %s", taken.Code, taken.Body)
	}
	var used sql.NullInt64
	other.R.QueryRow(`SELECT used_at FROM invites WHERE email = ''`).Scan(&used)
	if used.Valid {
		t.Error("an invite was consumed by a registration for a taken email")
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "attacker-pw-1"}, nil); w.Code != 401 {
		t.Errorf("a registration changed the password of an existing account: %d", w.Code)
	}
}

func mustID(t *testing.T, e env, email string) int64 {
	t.Helper()
	other, err := db.Open(e.path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var id int64
	if err := other.R.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

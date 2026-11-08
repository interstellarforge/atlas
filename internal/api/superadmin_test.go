package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/db"
)

// Nobody but an existing SUPERADMIN in a human session can make a SUPERADMIN (ATL-438): not an invite, not
// registration, not an organization admin, not a plain user, not a token, not MCP.
func TestOnlyASuperadminCanMakeASuperadmin(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	mk := func(email string) (func(*http.Request), int64) {
		u, err := e.auth.CreateUser(email, email, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		return loginAs(e, t, email, "password-123"), u.ID
	}
	plain, plainID := mk("plain@example.com")
	orgAdmin, orgAdminID := mk("orgadmin@example.com")
	e.orgAdmin(t, orgAdminID)
	target := strconv.FormatInt(plainID, 10)
	grant := map[string]any{"is_superadmin": true}

	for name, as := range map[string]func(*http.Request){"plain user": plain, "organization admin": orgAdmin} {
		if c := e.do("PATCH", "/api/v1/admin/users/"+target, grant, as).Code; c != 403 {
			t.Errorf("%s granted SUPERADMIN: HTTP %d", name, c)
		}
		if c := e.do("POST", "/api/v1/users", map[string]any{"email": "x@example.com", "name": "x", "password": "password-123", "is_superadmin": true}, as).Code; c != 403 {
			t.Errorf("%s created a SUPERADMIN: HTTP %d", name, c)
		}
	}
	// a token never administers, even the token of a SUPERADMIN
	_, tok, err := e.auth.CreateToken(1, "root", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	if c := e.do("PATCH", "/api/v1/admin/users/"+target, grant, bearer).Code; c != 403 {
		t.Errorf("a SUPERADMIN's token granted SUPERADMIN: HTTP %d", c)
	}
	// registration and invitations carry no such field and create ordinary accounts
	e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": "open", "session_days": 14, "done_visible_days": 14}, kees)
	if c := e.do("POST", "/api/v1/auth/register", map[string]any{"email": "r@example.com", "name": "r", "password": "password-123", "is_superadmin": true}, nil).Code; c != 400 {
		t.Errorf("register with a superadmin field: HTTP %d, want 400", c)
	}
	if c := e.do("POST", "/api/v1/admin/invites", map[string]any{"email": "i@example.com", "is_superadmin": true, "days": 3}, kees).Code; c != 400 {
		t.Errorf("an invite with a superadmin field: HTTP %d, want 400", c)
	}
	if c := e.do("POST", "/api/v1/auth/register", map[string]any{"email": "r@example.com", "name": "r", "password": "password-123"}, nil).Code; c != 201 {
		t.Fatalf("register: HTTP %d", c)
	}
	users := decodeBody[struct {
		Items []struct {
			Email        string
			IsSuperadmin bool `json:"is_superadmin"`
		}
	}](t, e.do("GET", "/api/v1/admin/users", nil, kees))
	supers := 0
	for _, u := range users.Items {
		if u.IsSuperadmin {
			supers++
		}
	}
	if supers != 1 { // only the bootstrap account
		t.Fatalf("%d accounts are SUPERADMIN, want 1", supers)
	}
	// the one path that works: an existing SUPERADMIN in a session, audited
	if c := e.do("PATCH", "/api/v1/admin/users/"+target, grant, kees).Code; c != 204 {
		t.Fatalf("SUPERADMIN grant: HTTP %d", c)
	}
	if body := e.do("GET", "/api/v1/admin/audit?operation=user.superadmin", nil, kees).Body.String(); !strings.Contains(body, `"operation":"user.superadmin"`) {
		t.Errorf("the grant is not in the audit log: %s", body)
	}
}

// A busy database answers 503 with Retry-After and changes nothing (ATL-483).
func TestBusyDatabaseAnswers503(t *testing.T) {
	e := setupWith(t, db.Options{BusyTimeout: 50 * time.Millisecond})
	kees := sessionOf(e, t)
	other, err := db.Open(e.path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.W.Begin() // BEGIN IMMEDIATE: holds the write lock
	if err != nil {
		t.Fatal(err)
	}
	w := e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	tx.Rollback()
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("locked database: HTTP %d %s, Retry-After %q", w.Code, w.Body, w.Header().Get("Retry-After"))
	}
	if c := e.do("GET", "/api/v1/projects/EXO", nil, kees).Code; c != 404 {
		t.Fatalf("the project exists after a refused create: HTTP %d", c)
	}
	if c := e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees).Code; c != 201 {
		t.Fatalf("retry after the lock is gone: HTTP %d", c)
	}
}

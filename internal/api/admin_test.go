package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

// TestAdminOnlyAndDeactivation: the admin API is for global admins in a session; a deactivated user loses
// sessions, tokens and sign-in at once; nobody locks the instance out of its last admin.
func TestAdminOnlyAndDeactivation(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	annS := loginAs(e, t, "ann@example.com", "password-123")
	_, annTok, _ := e.auth.CreateToken(ann.ID, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	annBearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+annTok) }
	_, adminTok, _ := e.auth.CreateToken(1, "admin-token", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	adminBearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+adminTok) }

	for _, p := range []string{"/api/v1/admin/users", "/api/v1/admin/audit", "/api/v1/admin/instance"} {
		if w := e.do("GET", p, nil, annS); w.Code != 403 {
			t.Fatalf("non-admin %s: %d", p, w.Code)
		}
		if w := e.do("GET", p, nil, adminBearer); w.Code != 403 {
			t.Fatalf("admin token %s: %d", p, w.Code)
		}
		if w := e.do("GET", p, nil, kees); w.Code != 200 {
			t.Fatalf("admin %s: %d", p, w.Code)
		}
	}
	id := strconv.FormatInt(ann.ID, 10)
	if w := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"active": false}, annS); w.Code != 403 {
		t.Fatalf("non-admin deactivated a user: %d", w.Code)
	}

	if w := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"active": false}, kees); w.Code != 204 {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, annS); w.Code != 401 {
		t.Fatalf("deactivated user's session works: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/projects", nil, annBearer); w.Code != 401 {
		t.Fatalf("deactivated user's token works: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "ann@example.com", "password": "password-123"}, nil); w.Code != 401 {
		t.Fatalf("deactivated user signed in: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"active": true}, kees); w.Code != 204 {
		t.Fatalf("reactivate: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "ann@example.com", "password": "password-123"}, nil); w.Code != 200 {
		t.Fatalf("reactivated user cannot sign in: %d", w.Code)
	}

	if w := e.do("PATCH", "/api/v1/admin/users/1", map[string]any{"active": false}, kees); w.Code != 422 {
		t.Fatalf("self-deactivation: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/admin/users/1", map[string]any{"is_admin": false}, kees); w.Code != 422 {
		t.Fatalf("demoting the last admin: %d", w.Code)
	}
	// With a second admin the first can step down.
	if w := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"is_admin": true}, kees); w.Code != 204 {
		t.Fatalf("promote: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/admin/users/1", map[string]any{"is_admin": false}, kees); w.Code != 204 {
		t.Fatalf("demote with another admin around: %d", w.Code)
	}
}

// TestAuditLogFiltersAndHidesSecrets: filters work and no password or token secret ever lands in the log.
func TestAuditLogFiltersAndHidesSecrets(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "brand-new-password"}, kees)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	w := e.do("POST", "/api/v1/projects/EXO/tokens", map[string]any{"name": "ci"}, kees)
	secret := decodeBody[struct{ Secret string }](t, w).Secret

	all := e.do("GET", "/api/v1/admin/audit?limit=200", nil, kees)
	if strings.Contains(all.Body.String(), "brand-new-password") || strings.Contains(all.Body.String(), "correct-horse") || strings.Contains(all.Body.String(), secret) {
		t.Fatalf("a secret reached the audit log: %s", all.Body)
	}
	only := decodeBody[struct{ Items []struct{ Operation string } }](t, e.do("GET", "/api/v1/admin/audit?operation=token.", nil, kees))
	if len(only.Items) != 1 || only.Items[0].Operation != "token.create" {
		t.Fatalf("operation filter: %+v", only)
	}
	if n := len(decodeBody[struct{ Items []any }](t, e.do("GET", "/api/v1/admin/audit?actor=human:user-999", nil, kees)).Items); n != 0 {
		t.Fatalf("actor filter returned %d entries", n)
	}
}

// TestRightsMatrixMatchesEnforcement: the matrix shown to admins is what each role can really do.
func TestRightsMatrixMatchesEnforcement(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, kees)
	roles := map[string]func(*http.Request){}
	for _, r := range []string{"viewer", "member", "admin"} {
		u, err := e.auth.CreateUser(r+"@example.com", r, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(u.ID, 10), map[string]any{"role": r}, kees)
		roles[r] = loginAs(e, t, r+"@example.com", "password-123")
	}
	try := map[string]func(role string, as func(*http.Request)) int{
		"view": func(_ string, as func(*http.Request)) int { return e.do("GET", "/api/v1/items/EXO-1", nil, as).Code },
		"export": func(_ string, as func(*http.Request)) int {
			return e.do("GET", "/api/v1/projects/EXO/export", nil, as).Code
		},
		"item.create": func(_ string, as func(*http.Request)) int {
			return e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "n"}, as).Code
		},
		"item.update": func(_ string, as func(*http.Request)) int {
			cur := decodeBody[map[string]any](t, e.do("GET", "/api/v1/items/EXO-1", nil, kees))
			return e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": cur["version"], "priority": "high"}, as).Code
		},
		"comment": func(_ string, as func(*http.Request)) int {
			return e.do("POST", "/api/v1/items/EXO-1/comments", map[string]any{"body": "hi"}, as).Code
		},
		"import": func(_ string, as func(*http.Request)) int {
			return e.do("POST", "/api/v1/projects/EXO/import?dry_run=1", map[string]any{"items": []any{map[string]any{"type": "task", "title": "i"}}}, as).Code
		},
		"workflow": func(role string, as func(*http.Request)) int {
			return e.do("POST", "/api/v1/projects/EXO/statuses", map[string]any{"name": "S-" + role, "category": "todo"}, as).Code
		},
		"project.update": func(_ string, as func(*http.Request)) int {
			return e.do("PATCH", "/api/v1/projects/EXO", map[string]any{"name": "EXO"}, as).Code
		},
		"members": func(_ string, as func(*http.Request)) int {
			return e.do("PUT", "/api/v1/projects/EXO/members/1", map[string]any{"role": "admin"}, as).Code
		},
		"tokens": func(_ string, as func(*http.Request)) int {
			return e.do("GET", "/api/v1/projects/EXO/tokens", nil, as).Code
		},
	}
	for _, row := range core.RightsMatrix() {
		f, ok := try[row.Action]
		if !ok {
			t.Fatalf("matrix row %q has no check", row.Action)
		}
		for role, want := range map[string]bool{"viewer": row.Viewer, "member": row.Member, "admin": row.Admin} {
			code := f(role, roles[role])
			if allowed := code < 300; allowed != want {
				t.Errorf("%s as %s: matrix says %v, got HTTP %d", row.Action, role, want, code)
			}
		}
	}
}

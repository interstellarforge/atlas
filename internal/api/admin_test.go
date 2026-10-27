package api_test

import (
	"net/http"
	"slices"
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
	if c := e.do("PUT", "/api/v1/admin/roles/commenter", map[string]any{"actions": []string{"view", "comment"}}, kees); c.Code != 200 {
		t.Fatalf("create role: %d", c.Code)
	}
	for _, r := range []string{"viewer", "member", "admin", "commenter"} {
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
	matrix := decodeBody[struct {
		Items   []core.Role
		Actions []string
	}](t, e.do("GET", "/api/v1/roles", nil, kees))
	if len(matrix.Items) != 4 {
		t.Fatalf("roles: %+v", matrix.Items)
	}
	for _, act := range matrix.Actions {
		f, ok := try[act]
		if !ok {
			t.Fatalf("action %q has no check", act)
		}
		for _, role := range matrix.Items {
			want := slices.Contains(role.Actions, act)
			code := f(role.Name, roles[role.Name])
			if allowed := code < 300; allowed != want {
				t.Errorf("%s as %s: matrix says %v, got HTTP %d", act, role.Name, want, code)
			}
		}
	}
}

// TestRoleEditing: edits take effect at once, admin is fixed, assigned roles cannot be deleted and a
// custom role with "members" cannot hand out admin.
func TestRoleEditing(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	u, _ := e.auth.CreateUser("lead@example.com", "lead", "password-123", false)
	uid := strconv.FormatInt(u.ID, 10)
	if c := e.do("PUT", "/api/v1/admin/roles/lead", map[string]any{"actions": []string{"view", "members"}}, kees).Code; c != 200 {
		t.Fatalf("create: %d", c)
	}
	e.do("PUT", "/api/v1/projects/EXO/members/"+uid, map[string]any{"role": "lead"}, kees)
	lead := loginAs(e, t, "lead@example.com", "password-123")
	if c := e.do("PUT", "/api/v1/projects/EXO/members/"+uid, map[string]any{"role": "admin"}, lead).Code; c != 403 {
		t.Errorf("self-promotion to admin: HTTP %d, want 403", c)
	}
	for name, body := range map[string]map[string]any{
		"admin":  {"actions": []string{"view"}},
		"noview": {"actions": []string{"comment"}},
		"bad":    {"actions": []string{"view", "nuke"}},
	} {
		if c := e.do("PUT", "/api/v1/admin/roles/"+name, body, kees).Code; c < 400 {
			t.Errorf("PUT role %s: HTTP %d, want an error", name, c)
		}
	}
	if c := e.do("DELETE", "/api/v1/admin/roles/lead", nil, kees).Code; c < 400 {
		t.Errorf("delete assigned role: HTTP %d", c)
	}
	if c := e.do("DELETE", "/api/v1/admin/roles/member", nil, kees).Code; c < 400 {
		t.Errorf("delete built-in role: HTTP %d", c)
	}
	if c := e.do("PUT", "/api/v1/projects/EXO/members/"+uid, map[string]any{"role": "ghost"}, kees).Code; c < 400 {
		t.Errorf("unknown role: HTTP %d", c)
	}
	if c := e.do("PUT", "/api/v1/admin/roles/x", map[string]any{"actions": []string{"view"}}, lead).Code; c != 403 {
		t.Errorf("non-admin role edit: HTTP %d, want 403", c)
	}
	if c := e.do("PUT", "/api/v1/admin/roles/viewer", map[string]any{"actions": []string{"view"}}, kees).Code; c != 200 {
		t.Fatalf("edit viewer: %d", c)
	}
	v, _ := e.auth.CreateUser("v@example.com", "v", "password-123", false)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(v.ID, 10), map[string]any{"role": "viewer"}, kees)
	if c := e.do("GET", "/api/v1/projects/EXO/export", nil, loginAs(e, t, "v@example.com", "password-123")).Code; c != 403 {
		t.Errorf("export after removing it from viewer: HTTP %d, want 403", c)
	}
	e.do("PUT", "/api/v1/projects/EXO/members/"+uid, map[string]any{"role": "viewer"}, kees)
	if c := e.do("DELETE", "/api/v1/admin/roles/lead", nil, kees).Code; c != 204 {
		t.Errorf("delete unused role: HTTP %d", c)
	}
}

// TestForcePasswordReset: the user is signed out, the next session can only change the password, and
// changing it restores full access. Only global admins can force it.
func TestForcePasswordReset(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	u, _ := e.auth.CreateUser("pw@example.com", "pw", "password-123", false)
	id := strconv.FormatInt(u.ID, 10)
	old := loginAs(e, t, "pw@example.com", "password-123")
	if c := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"force_password_reset": true}, old).Code; c != 403 {
		t.Fatalf("non-admin force: HTTP %d, want 403", c)
	}
	if c := e.do("PATCH", "/api/v1/admin/users/"+id, map[string]any{"force_password_reset": true}, kees).Code; c != 204 {
		t.Fatalf("force: HTTP %d", c)
	}
	if c := e.do("GET", "/api/v1/auth/me", nil, old).Code; c != 401 {
		t.Errorf("old session after force: HTTP %d, want 401", c)
	}
	fresh := loginAs(e, t, "pw@example.com", "password-123")
	if c := e.do("GET", "/api/v1/projects", nil, fresh).Code; c != 403 {
		t.Errorf("work before changing the password: HTTP %d, want 403", c)
	}
	me := decodeBody[struct {
		User struct {
			MustChangePassword bool `json:"must_change_password"`
		}
	}](t, e.do("GET", "/api/v1/auth/me", nil, fresh))
	if !me.User.MustChangePassword {
		t.Error("/auth/me does not report must_change_password")
	}
	if c := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "password-123", "new": "another-pass-1"}, fresh).Code; c != 204 {
		t.Fatalf("change password: HTTP %d", c)
	}
	if c := e.do("GET", "/api/v1/projects", nil, fresh).Code; c != 200 {
		t.Errorf("work after changing the password: HTTP %d, want 200", c)
	}
}

// TestDefaultRole: a member added without a role gets the instance default; it must be a real role and
// cannot be deleted while it is the default.
func TestDefaultRole(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("PUT", "/api/v1/admin/roles/reader", map[string]any{"actions": []string{"view"}}, kees)
	pol := decodeBody[map[string]any](t, e.do("GET", "/api/v1/admin/policy", nil, kees))
	if pol["default_role"] != "member" {
		t.Fatalf("default_role = %v, want member", pol["default_role"])
	}
	pol["default_role"] = "ghost"
	if c := e.do("PUT", "/api/v1/admin/policy", pol, kees).Code; c != 400 {
		t.Errorf("unknown default role: HTTP %d, want 400", c)
	}
	pol["default_role"] = "reader"
	if c := e.do("PUT", "/api/v1/admin/policy", pol, kees).Code; c != 200 {
		t.Fatalf("set default role: HTTP %d", c)
	}
	if c := e.do("DELETE", "/api/v1/admin/roles/reader", nil, kees).Code; c < 400 {
		t.Errorf("delete default role: HTTP %d", c)
	}
	u, _ := e.auth.CreateUser("new@example.com", "new", "password-123", false)
	id := strconv.FormatInt(u.ID, 10)
	if c := e.do("PUT", "/api/v1/projects/EXO/members/"+id, map[string]any{}, kees).Code; c != 204 {
		t.Fatalf("add without role: HTTP %d", c)
	}
	members := decodeBody[struct {
		Items []struct {
			UserID int64 `json:"user_id"`
			Role   string
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/members", nil, kees))
	for _, m := range members.Items {
		if m.UserID == u.ID && m.Role != "reader" {
			t.Errorf("new member role = %q, want reader", m.Role)
		}
	}
}

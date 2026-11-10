package api_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// ADR-0031: the Atlas SUPERADMIN has no access to an organization's work without a support grant from its ADMIN.
func TestSupportAccessGrants(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t) // SUPERADMIN, and ADMIN of MAIN
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "secret roadmap"}, kees)

	ops, err := e.auth.CreateUser("ops@example.com", "Ops", "password-123", true) // Atlas admin, not a member of MAIN
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveOrgMember(context.Background(), auth.Actor{Kind: auth.KindHuman, UserID: 1}, "MAIN", ops.ID, false); err != nil {
		t.Fatal(err)
	}
	opsS := loginAs(e, t, "ops@example.com", "password-123")
	_, secret, _ := e.auth.CreateToken(ops.ID, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	opsTok := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
	e.auth.CreateUser("plain@example.com", "Plain", "password-123", false)
	plain := loginAs(e, t, "plain@example.com", "password-123")
	grants := "/api/v1/organizations/MAIN/support-grants"

	// without a grant the SUPERADMIN sees nothing of the organization, and a missing project answers the same
	for _, p := range []string{"/api/v1/projects/EXO", "/api/v1/items/EXO-1", "/api/v1/items?project=EXO", "/api/v1/projects/EXO/export"} {
		hidden, ghost := e.do("GET", p, nil, opsS), e.do("GET", strings.ReplaceAll(p, "EXO", "NOPE"), nil, opsS)
		if hidden.Code != 404 || hidden.Code != ghost.Code || hidden.Body.String() != ghost.Body.String() {
			t.Errorf("GET %s without a grant: %d %q (a missing project: %d %q)", p, hidden.Code, hidden.Body, ghost.Code, ghost.Body)
		}
	}
	if body := e.do("GET", "/api/v1/projects", nil, opsS).Body.String(); strings.Contains(body, "EXO") {
		t.Errorf("the project list shows EXO without a grant: %s", body)
	}
	if body := e.do("GET", "/api/v1/changes?since=0", nil, opsS).Body.String(); strings.Contains(body, "EXO") || strings.Contains(body, "secret roadmap") {
		t.Errorf("the change feed leaks without a grant: %s", body)
	}
	if c := e.do("GET", "/api/v1/admin/users", nil, opsS).Code; c != 200 {
		t.Errorf("the SUPERADMIN lost the account list: %d", c)
	}

	// only an ADMIN of the organization gives access
	if c := e.do("POST", grants, map[string]any{"level": "full", "hours": 1}, opsS).Code; c != 403 {
		t.Errorf("a SUPERADMIN granted themselves access: %d", c)
	}
	if c := e.do("POST", grants, map[string]any{"level": "full", "hours": 1}, plain).Code; c != 403 {
		t.Errorf("a member granted access: %d", c)
	}
	if c := e.do("POST", grants, map[string]any{"level": "full"}, kees).Code; c != 400 {
		t.Errorf("a grant without an end: %d, want 400", c)
	}
	w := e.do("POST", grants, map[string]any{"level": "read_only", "hours": 1}, kees)
	if w.Code != 201 {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	id := strconv.Itoa(decodeBody[struct{ ID int }](t, w).ID)

	// read-only: view and export, nothing else; a token never uses a grant
	if c := e.do("GET", "/api/v1/items/EXO-1", nil, opsS).Code; c != 200 {
		t.Errorf("read with a read-only grant: %d", c)
	}
	if body := e.do("GET", "/api/v1/projects", nil, opsS).Body.String(); !strings.Contains(body, "EXO") {
		t.Errorf("the project list misses EXO under a grant: %s", body)
	}
	if c := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "title": "x"}, opsS).Code; c != 403 {
		t.Errorf("write with a read-only grant: %d, want 403", c)
	}
	if c := e.do("POST", "/api/v1/items/EXO-1/comments", map[string]any{"body": "hi"}, opsS).Code; c != 403 {
		t.Errorf("comment with a read-only grant: %d, want 403", c)
	}
	if c := e.do("GET", "/api/v1/items/EXO-1", nil, opsTok).Code; c != 404 {
		t.Errorf("a token used a support grant: %d, want 404", c)
	}
	if c := e.do("GET", grants, nil, opsS).Code; c != 403 {
		t.Errorf("a SUPERADMIN listed the grants: %d", c)
	}
	if body := e.do("GET", grants, nil, kees).Body.String(); !strings.Contains(body, `"valid":true`) {
		t.Errorf("grant list: %s", body)
	}

	// the use is audited
	var audit string
	for i := 0; i < 40 && !strings.Contains(audit, "support.access"); i++ {
		time.Sleep(50 * time.Millisecond)
		audit = e.do("GET", "/api/v1/admin/audit?operation=support.access", nil, kees).Body.String()
	}
	if !strings.Contains(audit, "support.access") || !strings.Contains(audit, "user-"+strconv.FormatInt(ops.ID, 10)) {
		t.Errorf("support use is not audited: %s", audit)
	}

	// revoking works at once; a full grant allows changes
	if c := e.do("DELETE", grants+"/"+id, nil, kees).Code; c != 204 {
		t.Fatalf("revoke: %d", c)
	}
	if c := e.do("GET", "/api/v1/items/EXO-1", nil, opsS).Code; c != 404 {
		t.Errorf("access after revocation: %d, want 404", c)
	}
	if c := e.do("DELETE", grants+"/"+id, nil, kees).Code; c != 404 {
		t.Errorf("revoking twice: %d, want 404", c)
	}
	if c := e.do("POST", grants, map[string]any{"level": "full", "until_revoked": true}, kees).Code; c != 201 {
		t.Fatalf("full grant: %d", c)
	}
	if c := e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "title": "fixed by support"}, opsS).Code; c != 200 {
		t.Errorf("change with a full grant: %d", c)
	}
	if c := e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(ops.ID, 10), map[string]any{"role": "member"}, opsS).Code; c == 204 {
		t.Errorf("a SUPERADMIN under a grant made themselves a project member")
	}
}

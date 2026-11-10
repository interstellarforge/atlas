package api_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

// ADR-0031: organization applications over REST, the Atlas admin's organization list, and who may call what.
func TestOrganizationApplicationsAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	ann := loginAs(e, t, "ann@example.com", "password-123")
	_, secret, _ := e.auth.CreateToken(2, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	annTok := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
	apply := func(as func(*http.Request), key string) int {
		return e.do("POST", "/api/v1/organizations/applications", map[string]any{"key": key, "name": key + " Inc", "note": "hi"}, as).Code
	}
	if c := apply(ann, "ANN"); c != 403 {
		t.Fatalf("applying while the setting is admin_only: %d", c)
	}
	if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": "closed", "organization_creation": "bogus", "session_days": 14}, kees); w.Code != 400 {
		t.Fatalf("an unknown organization_creation: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": "closed", "organization_creation": "apply", "session_days": 14, "done_visible_days": 14}, kees); w.Code != 200 {
		t.Fatalf("policy: %d %s", w.Code, w.Body)
	}
	if body := e.do("GET", "/api/v1/auth/policy", nil, nil).Body.String(); !strings.Contains(body, `"organization_creation":"apply"`) {
		t.Errorf("public policy: %s", body)
	}
	if c := apply(annTok, "ANN"); c != 403 {
		t.Errorf("a token applied: %d", c)
	}
	if c := apply(ann, "ANN"); c != 201 {
		t.Fatalf("apply: %d", c)
	}
	if c := apply(ann, "ANN2"); c != 422 {
		t.Errorf("a second waiting application: %d, want 422", c)
	}
	for _, p := range []string{"/api/v1/admin/organization-applications", "/api/v1/admin/organizations"} {
		if c := e.do("GET", p, nil, ann).Code; c != 403 {
			t.Errorf("a plain account reads %s: %d, want 403", p, c)
		}
	}
	queue := e.do("GET", "/api/v1/admin/organization-applications", nil, kees).Body.String()
	if !strings.Contains(queue, "ann@example.com") {
		t.Fatalf("queue: %s", queue)
	}
	id := strconv.Itoa(decodeBody[struct{ Items []struct{ ID int } }](t, e.do("GET", "/api/v1/admin/organization-applications", nil, kees)).Items[0].ID)
	if c := e.do("POST", "/api/v1/admin/organization-applications/"+id+"/approve", map[string]any{}, ann).Code; c != 403 {
		t.Errorf("the applicant approved their own application: %d", c)
	}
	if c := e.do("POST", "/api/v1/admin/organization-applications/"+id+"/approve", map[string]any{}, kees).Code; c != 204 {
		t.Fatalf("approve: %d", c)
	}
	if body := e.do("GET", "/api/v1/organizations", nil, ann).Body.String(); !strings.Contains(body, `"key":"ANN"`) || !strings.Contains(body, `"role":"admin"`) {
		t.Errorf("the applicant does not administer the new organization: %s", body)
	}
	if body := e.do("GET", "/api/v1/organizations/applications", nil, ann).Body.String(); !strings.Contains(body, `"status":"approved"`) {
		t.Errorf("own applications: %s", body)
	}
	if body := e.do("GET", "/api/v1/admin/organizations", nil, kees).Body.String(); !strings.Contains(body, "ann@example.com") {
		t.Errorf("admin organization list: %s", body)
	}
	// the Atlas admin creates one directly for an existing account and does not join it
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "DIRECT", "name": "Direct", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
}

// ADR-0031: organization invitations over REST: ADMIN-only creation, the open accept path and its non-disclosure.
func TestOrganizationInvitesAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	ann := loginAs(e, t, "ann@example.com", "password-123")
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	if c := e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "x@example.com"}, kees).Code; c != 403 {
		t.Errorf("the Atlas admin invited into a company: %d, want 403", c)
	}
	w := e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "new@example.com"}, ann)
	if w.Code != 201 {
		t.Fatalf("invite: %d %s", w.Code, w.Body)
	}
	token := decodeBody[struct{ Token string }](t, w).Token
	if c := e.do("GET", "/api/v1/auth/invitations/wrong", nil, nil).Code; c != 422 {
		t.Errorf("preview with a wrong token: %d, want 422", c)
	}
	if body := e.do("GET", "/api/v1/auth/invitations/"+token, nil, nil).Body.String(); !strings.Contains(body, `"organization":"Ann Inc"`) {
		t.Errorf("preview: %s", body)
	}
	if c := e.do("POST", "/api/v1/auth/invitations/accept", map[string]any{"token": token, "name": "New", "password": "password-123"}, nil).Code; c != 200 {
		t.Fatalf("accept: %d", c)
	}
	if c := e.do("POST", "/api/v1/auth/invitations/accept", map[string]any{"token": token, "name": "New", "password": "password-123"}, nil).Code; c != 422 {
		t.Errorf("reusing the token: %d, want 422", c)
	}
	newer := loginAs(e, t, "new@example.com", "password-123")
	if body := e.do("GET", "/api/v1/organizations", nil, newer).Body.String(); !strings.Contains(body, `"key":"ANN"`) || !strings.Contains(body, `"role":"user"`) {
		t.Errorf("the invited account is not a USER of the organization: %s", body)
	}
	if c := e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "y@example.com"}, newer).Code; c != 403 {
		t.Errorf("a USER invited: %d, want 403", c)
	}
}

// ADR-0031: join link over REST: the same 202 for every request, ADMIN-only queue and decisions.
func TestJoinRequestsAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	e.auth.CreateUser("bo@example.com", "Bo", "password-123", false)
	ann := loginAs(e, t, "ann@example.com", "password-123")
	bo := loginAs(e, t, "bo@example.com", "password-123")
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	req := func(key string) int { return e.do("POST", "/api/v1/organizations/"+key+"/join-requests", nil, bo).Code }
	if a, b := req("ANN"), req("NOPE"); a != 202 || b != 202 {
		t.Fatalf("link off or unknown: %d and %d, want 202 for both", a, b)
	}
	if c := e.do("PUT", "/api/v1/organizations/ANN/join-link", map[string]any{"enabled": true}, bo).Code; c != 404 {
		t.Errorf("a stranger switched the link on: %d, want 404", c)
	}
	if c := e.do("PUT", "/api/v1/organizations/ANN/join-link", map[string]any{"enabled": true}, ann).Code; c != 204 {
		t.Fatalf("enable: %d", c)
	}
	if c := req("ANN"); c != 202 {
		t.Fatalf("request: %d", c)
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/join-requests", nil, bo).Code; c != 404 {
		t.Errorf("the requester read the queue: %d, want 404", c)
	}
	queue := decodeBody[struct{ Items []struct{ ID int } }](t, e.do("GET", "/api/v1/organizations/ANN/join-requests?status=pending", nil, ann))
	if len(queue.Items) != 1 {
		t.Fatalf("queue: %+v", queue)
	}
	id := strconv.Itoa(queue.Items[0].ID)
	if c := e.do("POST", "/api/v1/organizations/ANN/join-requests/"+id+"/approve", map[string]any{}, bo).Code; c != 404 {
		t.Errorf("the requester approved themselves: %d, want 404", c)
	}
	if c := e.do("POST", "/api/v1/organizations/ANN/join-requests/"+id+"/approve", map[string]any{}, ann).Code; c != 204 {
		t.Fatalf("approve: %d", c)
	}
	if body := e.do("GET", "/api/v1/organizations", nil, bo).Body.String(); !strings.Contains(body, `"role":"user"`) {
		t.Errorf("the requester is not a USER after approval: %s", body)
	}
}

// ADR-0031: email domains over REST: ADMIN claims, only the SUPERADMIN confirms, free mail is refused.
func TestEmailDomainsAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	ann := loginAs(e, t, "ann@example.com", "password-123")
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	if c := e.do("POST", "/api/v1/organizations/ANN/domains", map[string]any{"domain": "gmail.com"}, ann).Code; c != 422 {
		t.Errorf("a free-mail domain: %d, want 422", c)
	}
	w := e.do("POST", "/api/v1/organizations/ANN/domains", map[string]any{"domain": "example.com", "mode": "auto"}, ann)
	if w.Code != 201 {
		t.Fatalf("claim: %d %s", w.Code, w.Body)
	}
	id := strconv.Itoa(decodeBody[struct{ ID int }](t, w).ID)
	if c := e.do("POST", "/api/v1/admin/organization-domains/"+id+"/approve", map[string]any{}, ann).Code; c != 403 {
		t.Errorf("the claimant confirmed their own domain: %d, want 403", c)
	}
	if c := e.do("GET", "/api/v1/admin/organization-domains", nil, ann).Code; c != 403 {
		t.Errorf("the claimant read the queue: %d, want 403", c)
	}
	if c := e.do("POST", "/api/v1/admin/organization-domains/"+id+"/approve", map[string]any{}, kees).Code; c != 204 {
		t.Fatalf("confirm: %d", c)
	}
	if body := e.do("GET", "/api/v1/organizations/ANN/domains", nil, ann).Body.String(); !strings.Contains(body, `"status":"confirmed"`) {
		t.Errorf("domains: %s", body)
	}
	if c := e.do("POST", "/api/v1/auth/email-verification", map[string]any{"token": "wrong"}, nil).Code; c != 422 {
		t.Errorf("a wrong verification link: %d, want 422", c)
	}
	if c := e.do("POST", "/api/v1/me/email/verification", nil, ann).Code; c != 422 {
		t.Errorf("a verification mail without outgoing mail: %d, want 422", c)
	}
	if body := e.do("GET", "/api/v1/me/join-options", nil, ann).Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Errorf("join options of an unverified address: %s", body)
	}
}

// ADR-0031 point 9: organization rules on sessions and tokens: session length, required 2FA (limited session),
// token access and lifetime at creation and at use.
func TestOrganizationPolicyOnSessionsAndTokens(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	bo, _ := e.auth.CreateUser("bo@example.com", "Bo", "password-123", false)
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	annActor := auth.Actor{Kind: auth.KindHuman, UserID: ann.ID}
	if err := e.svc.SetOrgMember(context.Background(), annActor, "ANN", bo.ID, "user", false); err != nil {
		t.Fatal(err)
	}
	annS := loginAs(e, t, "ann@example.com", "password-123")
	put := func(body map[string]any) int {
		return e.do("PUT", "/api/v1/organizations/ANN/settings", body, annS).Code
	}
	if c := put(map[string]any{"session_days": 2}); c != 200 {
		t.Fatalf("settings: %d", c)
	}

	// session length: the shorter organization value
	w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "bo@example.com", "password": "password-123"}, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d", w.Code)
	}
	if left := time.Until(w.Result().Cookies()[0].Expires); left > 49*time.Hour || left < 47*time.Hour {
		t.Errorf("session cookie lasts %v, want about 2 days", left)
	}
	boS := loginAs(e, t, "bo@example.com", "password-123")

	// token policy at creation and at use
	if c := put(map[string]any{"tokens_allowed": false}); c != 200 {
		t.Fatal(c)
	}
	mk := func(body map[string]any) *httptest.ResponseRecorder { return e.do("POST", "/api/v1/tokens", body, boS) }
	if w := mk(map[string]any{"name": "t", "scopes": []string{"read"}}); w.Code != 403 || !strings.Contains(w.Body.String(), "token_policy") {
		t.Errorf("a token while access is off: %d %s", w.Code, w.Body)
	}
	if c := put(map[string]any{"tokens_allowed": true, "token_max_days": 7}); c != 200 {
		t.Fatal(c)
	}
	day := int64(86_400_000)
	if c := mk(map[string]any{"name": "t", "scopes": []string{"read"}}).Code; c != 403 {
		t.Errorf("a token without expiry under a lifetime limit: %d", c)
	}
	if c := mk(map[string]any{"name": "t", "scopes": []string{"read"}, "expires_at": time.Now().UnixMilli() + 30*day}).Code; c != 403 {
		t.Errorf("a token living 30 days under a 7-day limit: %d", c)
	}
	w = mk(map[string]any{"name": "t", "scopes": []string{"read"}, "expires_at": time.Now().UnixMilli() + 3*day})
	if w.Code != 201 {
		t.Fatalf("a 3-day token: %d %s", w.Code, w.Body)
	}
	secret := decodeBody[struct{ Secret string }](t, w).Secret
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
	if c := e.do("GET", "/api/v1/organizations", nil, bearer).Code; c != 200 {
		t.Fatalf("the token does not work: %d", c)
	}
	if c := put(map[string]any{"tokens_allowed": false}); c != 200 {
		t.Fatal(c)
	}
	if c := e.do("GET", "/api/v1/organizations", nil, bearer).Code; c != 401 {
		t.Errorf("a token kept working after the organization switched access off: %d, want 401", c)
	}

	// required 2FA: the session may only set it up (an ADMIN who requires it for everyone is bound by it too)
	if c := put(map[string]any{"require_2fa": true}); c != 200 {
		t.Fatal(c)
	}
	if w := e.do("GET", "/api/v1/organizations", nil, boS); w.Code != 403 || !strings.Contains(w.Body.String(), "totp_required") {
		t.Errorf("a session without 2FA reached the app: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, boS); w.Code != 200 || !strings.Contains(w.Body.String(), `"must_enable_totp":true`) {
		t.Errorf("me: %d %s", w.Code, w.Body)
	}
	if c := e.do("GET", "/api/v1/me/security", nil, boS).Code; c != 200 {
		t.Errorf("security page while 2FA is required: %d", c)
	}

}

// ADR-0031: organization members over REST: ADMIN-only, the last ADMIN is protected.
func TestOrganizationMembersAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	bo, _ := e.auth.CreateUser("bo@example.com", "Bo", "password-123", false)
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	annS := loginAs(e, t, "ann@example.com", "password-123")
	boS := loginAs(e, t, "bo@example.com", "password-123")
	if c := e.do("GET", "/api/v1/organizations/ANN/members", nil, kees).Code; c != 403 {
		t.Errorf("the Atlas admin listed the members of a company: %d, want 403", c)
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/members", nil, boS).Code; c != 404 {
		t.Errorf("a stranger listed the members: %d, want 404", c)
	}
	if c := e.do("PUT", "/api/v1/organizations/ANN/members/"+strconv.FormatInt(bo.ID, 10), map[string]any{"role": "user"}, annS).Code; c != 204 {
		t.Fatalf("add: %d", c)
	}
	if body := e.do("GET", "/api/v1/organizations/ANN/members", nil, annS).Body.String(); !strings.Contains(body, "bo@example.com") {
		t.Errorf("members: %s", body)
	}
	if c := e.do("PUT", "/api/v1/organizations/ANN/members/"+strconv.FormatInt(ann.ID, 10), map[string]any{"role": "user"}, annS).Code; c != 422 {
		t.Errorf("demoting the last ADMIN: %d, want 422", c)
	}
	if c := e.do("DELETE", "/api/v1/organizations/ANN/members/"+strconv.FormatInt(ann.ID, 10), nil, annS).Code; c != 422 {
		t.Errorf("removing the last ADMIN: %d, want 422", c)
	}
	if c := e.do("DELETE", "/api/v1/organizations/ANN/members/"+strconv.FormatInt(bo.ID, 10), nil, boS).Code; c != 403 {
		t.Errorf("a USER removed a member: %d, want 403", c)
	}
	if c := e.do("DELETE", "/api/v1/organizations/ANN/members/"+strconv.FormatInt(bo.ID, 10), nil, annS).Code; c != 204 {
		t.Errorf("remove: %d", c)
	}
}

// ADR-0031 point 9: the organization logo: only the ADMIN changes it, members read it, outsiders get the same
// answer as for a missing logo.
func TestOrganizationLogoAPI(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	bo, _ := e.auth.CreateUser("bo@example.com", "Bo", "password-123", false)
	e.auth.CreateUser("cy@example.com", "Cy", "password-123", false)
	if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": "ANN", "name": "Ann Inc", "admin_email": "ann@example.com"}, kees).Code; c != 201 {
		t.Fatalf("create: %d", c)
	}
	if err := e.svc.SetOrgMember(context.Background(), auth.Actor{Kind: auth.KindHuman, UserID: ann.ID}, "ANN", bo.ID, "user", false); err != nil {
		t.Fatal(err)
	}
	annS := loginAs(e, t, "ann@example.com", "password-123")
	boS := loginAs(e, t, "bo@example.com", "password-123")
	cyS := loginAs(e, t, "cy@example.com", "password-123")
	put := func(as func(*http.Request), data []byte) int {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("file", "logo.png")
		_, _ = part.Write(data)
		_ = mw.Close()
		r := httptest.NewRequest("PUT", "/api/v1/organizations/ANN/logo", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		as(r)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		return w.Code
	}
	if c := put(boS, pngBytes(t, 10, 10)); c != 403 {
		t.Errorf("a member set the logo: %d, want 403", c)
	}
	if c := put(cyS, pngBytes(t, 10, 10)); c != 404 {
		t.Errorf("an outsider set the logo: %d, want 404", c)
	}
	if c := put(annS, []byte("not an image")); c != 400 {
		t.Errorf("a non-image logo: %d, want 400", c)
	}
	if c := put(annS, pngBytes(t, 10, 10)); c != 204 {
		t.Fatalf("set the logo: %d", c)
	}
	if w := e.do("GET", "/api/v1/organizations/ANN/logo", nil, boS); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("a member reads the logo: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/logo", nil, cyS).Code; c != 404 {
		t.Errorf("an outsider reads the logo: %d, want 404", c)
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/logo", nil, nil).Code; c != 401 {
		t.Errorf("anonymous reads the logo: %d, want 401", c)
	}
	if body := e.do("GET", "/api/v1/organizations", nil, boS).Body.String(); !strings.Contains(body, `"logo":true`) {
		t.Errorf("the organization list does not show the logo: %s", body)
	}
	if c := e.do("DELETE", "/api/v1/organizations/ANN/logo", nil, boS).Code; c != 403 {
		t.Errorf("a member removed the logo: %d, want 403", c)
	}
	if c := e.do("DELETE", "/api/v1/organizations/ANN/logo", nil, annS).Code; c != 204 {
		t.Errorf("remove: %d", c)
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/logo", nil, boS).Code; c != 404 {
		t.Errorf("the removed logo: %d, want 404", c)
	}
}

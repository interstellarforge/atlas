package api_test

import (
	"strings"
	"testing"

	"github.com/kees/atlas/internal/api"
)

func TestRequestLogHidesInvitationToken(t *testing.T) {
	if got := api.LogPath("/api/v1/auth/invitations/SECRET"); strings.Contains(got, "SECRET") {
		t.Fatalf("token in log path: %s", got)
	}
	if got := api.LogPath("/api/v1/auth/invitations/accept"); got != "/api/v1/auth/invitations/accept" {
		t.Fatalf("accept path changed: %s", got)
	}
}

// ATL-456: token guessing is limited, every unusable invitation looks the same, and the token never reaches the
// audit log. ATL-481: a user in two organizations acts only where they hold rights.
func TestInvitationHardeningAndMultiOrganization(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	e.auth.CreateUser("bea@example.com", "Bea", "password-123", false)
	ann := loginAs(e, t, "ann@example.com", "password-123")
	bea := loginAs(e, t, "bea@example.com", "password-123")
	for _, o := range []string{"ANN", "BEA"} {
		admin := map[string]string{"ANN": "ann@example.com", "BEA": "bea@example.com"}[o]
		if c := e.do("POST", "/api/v1/admin/organizations", map[string]any{"key": o, "name": o + " Inc", "admin_email": admin}, kees).Code; c != 201 {
			t.Fatalf("create %s: %d", o, c)
		}
	}
	if c := e.do("POST", "/api/v1/projects", map[string]any{"key": "ANNP", "name": "ann project", "organization": "ANN"}, ann).Code; c != 201 {
		t.Fatalf("project: %d", c)
	}
	// Ann is also a plain member of BEA: she holds no rights there
	if c := e.do("POST", "/api/v1/organizations/BEA/invites", map[string]any{"email": "ann@example.com"}, bea).Code; c != 201 {
		t.Fatalf("invite ann to BEA: %d", c)
	}
	// bea cannot see ann's project or members; ann's admin rights stop at her organization
	if c := e.do("GET", "/api/v1/projects/ANNP", nil, bea).Code; c != 404 {
		t.Errorf("another organization's ADMIN reads a project: %d, want 404", c)
	}
	if c := e.do("GET", "/api/v1/organizations/ANN/members", nil, bea).Code; c != 404 {
		t.Errorf("another organization's members: %d, want 404", c)
	}
	if c := e.do("POST", "/api/v1/organizations/BEA/invites", map[string]any{"email": "z@example.com", "projects": []string{"ANNP"}}, bea).Code; c != 400 {
		t.Errorf("inviting onto another organization's project: %d, want 400", c)
	}

	// the same answer for an address with an account and one without
	known := e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "bea@example.com"}, ann)
	fresh := e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "nobody@example.com"}, ann)
	if known.Code != 201 || fresh.Code != 201 {
		t.Fatalf("invite answers differ: %d vs %d", known.Code, fresh.Code)
	}
	token := decodeBody[struct{ Token string }](t, fresh).Token
	revoked := decodeBody[struct{ Token string }](t, known).Token

	// the token is never written to the audit log
	if body := e.do("GET", "/api/v1/organizations/ANN/audit", nil, ann).Body.String(); strings.Contains(body, token) || strings.Contains(body, revoked) {
		t.Errorf("an invitation token is in the audit log: %s", body)
	}
	if body := e.do("GET", "/api/v1/admin/audit", nil, kees).Body.String(); strings.Contains(body, token) {
		t.Errorf("an invitation token is in the instance audit log")
	}

	// revoked, used and wrong tokens all answer alike
	list := decodeBody[struct {
		Items []struct {
			ID    int64
			Email string
		}
	}](t, e.do("GET", "/api/v1/organizations/ANN/invites", nil, ann))
	for _, i := range list.Items {
		if i.Email == "bea@example.com" {
			if c := e.do("DELETE", "/api/v1/organizations/ANN/invites/"+itoa(i.ID), nil, ann).Code; c != 204 {
				t.Fatalf("revoke: %d", c)
			}
		}
	}
	if c := e.do("POST", "/api/v1/auth/invitations/accept", map[string]any{"token": token, "name": "N", "password": "password-123"}, nil).Code; c != 200 {
		t.Fatalf("accept: %d", c)
	}
	var want string
	for _, tk := range []string{"wrong-token", revoked, token} {
		w := e.do("GET", "/api/v1/auth/invitations/"+tk, nil, nil)
		if w.Code != 422 {
			t.Errorf("unusable token %q: %d, want 422", tk, w.Code)
		}
		if want == "" {
			want = w.Body.String()
		} else if w.Body.String() != want {
			t.Errorf("unusable invitations answer differently: %q vs %q", w.Body.String(), want)
		}
	}

	// guessing is limited: after enough failures even a valid token is refused with 429
	valid := decodeBody[struct{ Token string }](t, e.do("POST", "/api/v1/organizations/ANN/invites", map[string]any{"email": "late@example.com"}, ann)).Token
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		limited = e.do("GET", "/api/v1/auth/invitations/guess-"+itoa(int64(i)), nil, nil).Code == 429
	}
	if !limited {
		t.Fatal("40 wrong invitation tokens were never throttled")
	}
	if c := e.do("GET", "/api/v1/auth/invitations/"+valid, nil, nil).Code; c != 429 {
		t.Errorf("a throttled client got %d for a valid token, want 429", c)
	}
}

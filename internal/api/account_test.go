package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
)

func sessionOf(e env, t *testing.T) func(*http.Request) {
	cookie, csrf := e.login(t)
	return func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
}

// TestPasswordChangeRevokesOtherSessions: the current password is required, other sessions and the old
// password stop working, the session that changed it stays, and a token cannot do it at all.
func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	e := setup(t)
	a, b := sessionOf(e, t), sessionOf(e, t)

	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "nope", "new": "brand-new-password"}, a); w.Code != 422 {
		t.Fatalf("wrong current password: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "short"}, a); w.Code != 400 {
		t.Fatalf("weak password: %d", w.Code)
	}
	_, token, _ := e.auth.CreateToken(1, "rw", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
	viaToken := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "brand-new-password"}, viaToken); w.Code != 403 {
		t.Fatalf("token changed a password: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "brand-new-password"}, a); w.Code != 204 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, a); w.Code != 200 {
		t.Fatalf("own session after change: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, b); w.Code != 401 {
		t.Fatalf("other session survived: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "correct-horse"}, nil); w.Code != 401 {
		t.Fatalf("old password still works: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "brand-new-password"}, nil); w.Code != 200 {
		t.Fatalf("new password: %d", w.Code)
	}
}

func TestSessionsAreListedAndRevocable(t *testing.T) {
	e := setup(t)
	a, b := sessionOf(e, t), sessionOf(e, t)
	list := decodeBody[struct {
		Items []struct {
			ID      int64
			Current bool
		}
	}](t, e.do("GET", "/api/v1/me/sessions", nil, a))
	if len(list.Items) != 2 {
		t.Fatalf("sessions: %+v", list)
	}
	var other int64
	for _, s := range list.Items {
		if !s.Current {
			other = s.ID
		}
	}
	if w := e.do("DELETE", "/api/v1/me/sessions/999", nil, a); w.Code != 404 {
		t.Fatalf("revoke unknown session: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/me/sessions/"+strconv.FormatInt(other, 10), nil, a); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, b); w.Code != 401 {
		t.Fatalf("revoked session still works: %d", w.Code)
	}
}

// TestPreferencesAndMutedNotifications: preferences are validated, and a muted type creates no notification.
func TestPreferencesAndMutedNotifications(t *testing.T) {
	e := setup(t)
	a := sessionOf(e, t)
	if w := e.do("PUT", "/api/v1/me/preferences", map[string]any{"theme": "neon", "default_view": "board"}, a); w.Code != 400 {
		t.Fatalf("invalid theme: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/me/preferences", map[string]any{"theme": "dark", "default_view": "list", "muted_notifications": []string{"followed"}}, a); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	got := decodeBody[map[string]any](t, e.do("GET", "/api/v1/me/preferences", nil, a))
	if got["theme"] != "dark" || got["default_view"] != "list" {
		t.Fatalf("preferences: %v", got)
	}
	// Bob follows an item and Kees changes it. With "followed" muted Bob gets nothing; unmuted he gets one.
	if _, err := e.auth.CreateUser("bob@example.com", "Bob", "password-123", true); err != nil {
		t.Fatal(err)
	}
	bobLogin := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "bob@example.com", "password": "password-123"}, nil)
	bobCSRF := decodeBody[struct {
		CSRF string `json:"csrf_token"`
	}](t, bobLogin).CSRF
	bobCookie := bobLogin.Result().Cookies()[0]
	bob := func(r *http.Request) { r.AddCookie(bobCookie); r.Header.Set(auth.CSRFHeader, bobCSRF) }

	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, a)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, a)
	e.do("PUT", "/api/v1/items/EXO-1/follow", nil, bob)
	unread := func() int {
		return int(decodeBody[struct{ Unread float64 }](t, e.do("GET", "/api/v1/notifications", nil, bob)).Unread)
	}
	e.do("PUT", "/api/v1/me/preferences", map[string]any{"theme": "system", "default_view": "board", "muted_notifications": []string{"followed"}}, bob)
	e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 1, "title": "changed"}, a)
	if n := unread(); n != 0 {
		t.Fatalf("muted type still notified: %d", n)
	}
	e.do("PUT", "/api/v1/me/preferences", map[string]any{"theme": "system", "default_view": "board", "muted_notifications": []string{}}, bob)
	e.do("PATCH", "/api/v1/items/EXO-1", map[string]any{"version": 2, "title": "changed again"}, a)
	if n := unread(); n != 1 {
		t.Fatalf("unmuted type not notified: %d", n)
	}
}

func loginAs(e env, t *testing.T, email, password string) func(*http.Request) {
	w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, nil)
	if w.Code != 200 {
		t.Fatalf("login %s: %d", email, w.Code)
	}
	csrf := decodeBody[struct {
		CSRF string `json:"csrf_token"`
	}](t, w).CSRF
	cookie := w.Result().Cookies()[0]
	return func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
}

// TestProjectTokens: only a project admin manages them; a token reads and writes only its project and
// only within its scope; revoking works at once; and a token dies with its owner's membership.
func TestProjectTokens(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	ann, err := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := e.auth.CreateUser("bob@example.com", "Bob", "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, kees)
		e.do("POST", "/api/v1/items", map[string]any{"project": k, "type": "task", "title": k + " item"}, kees)
	}
	member := func(key string, uid int64, role string) {
		if w := e.do("PUT", "/api/v1/projects/"+key+"/members/"+strconv.FormatInt(uid, 10), map[string]any{"role": role}, kees); w.Code != 204 && w.Code != 200 {
			t.Fatalf("member: %d %s", w.Code, w.Body)
		}
	}
	member("EXO", ann.ID, "admin")
	member("EXO", bob.ID, "member")
	member("OTH", ann.ID, "member")
	annS, bobS := loginAs(e, t, "ann@example.com", "password-123"), loginAs(e, t, "bob@example.com", "password-123")

	if w := e.do("POST", "/api/v1/projects/EXO/tokens", map[string]any{"name": "ci"}, bobS); w.Code != 403 {
		t.Fatalf("member created a project token: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/projects/OTH/tokens", map[string]any{"name": "ci"}, annS); w.Code != 403 {
		t.Fatalf("non-admin of OTH created a token: %d", w.Code)
	}
	w := e.do("POST", "/api/v1/projects/EXO/tokens", map[string]any{"name": "ci", "write": false}, annS)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	made := decodeBody[struct {
		Token  struct{ ID int64 }
		Secret string
	}](t, w)
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+made.Secret) }

	if w := e.do("GET", "/api/v1/items?project=EXO", nil, bearer); w.Code != 200 {
		t.Fatalf("token reads its project: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/items?project=OTH", nil, bearer); w.Code == 200 && strings.Contains(w.Body.String(), "OTH-1") {
		t.Fatalf("token saw another project: %s", w.Body)
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "w"}, bearer); w.Code != 403 {
		t.Fatalf("read-only token wrote: %d", w.Code)
	}
	list := decodeBody[struct {
		Items []struct {
			Owner      string
			LastUsedAt *int64 `json:"last_used_at"`
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/tokens", nil, annS))
	if len(list.Items) != 1 || list.Items[0].Owner != "Ann" || list.Items[0].LastUsedAt == nil {
		t.Fatalf("list: %+v", list)
	}

	// The owner leaves the project: the token is revoked and stops working.
	if w := e.do("DELETE", "/api/v1/projects/EXO/members/"+strconv.FormatInt(ann.ID, 10), nil, kees); w.Code != 204 {
		t.Fatalf("remove member: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/items?project=EXO", nil, bearer); w.Code != 401 {
		t.Fatalf("token of a removed owner still works: %d", w.Code)
	}
	revoked := decodeBody[struct {
		Items []struct {
			RevokedAt *int64 `json:"revoked_at"`
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/tokens", nil, kees))
	if len(revoked.Items) != 1 || revoked.Items[0].RevokedAt == nil {
		t.Fatalf("token not marked revoked: %+v", revoked)
	}
	// Revoking by hand.
	w = e.do("POST", "/api/v1/projects/EXO/tokens", map[string]any{"name": "second", "write": true}, kees)
	second := decodeBody[struct {
		Token  struct{ ID int64 }
		Secret string
	}](t, w)
	viaSecond := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+second.Secret) }
	if w := e.do("GET", "/api/v1/items?project=EXO", nil, viaSecond); w.Code != 200 {
		t.Fatalf("second token: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/projects/EXO/tokens/"+strconv.FormatInt(second.Token.ID, 10), nil, kees); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/items?project=EXO", nil, viaSecond); w.Code != 401 {
		t.Fatalf("revoked token works: %d", w.Code)
	}
}

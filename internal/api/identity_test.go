package api_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// TestConcurrentPasswordChanges: many sessions race to change the same password; the current-password check
// and the update share one transaction, so exactly one wins and only its password works afterwards.
func TestConcurrentPasswordChanges(t *testing.T) {
	e := setup(t)
	const n = 8
	sessions := make([]func(*http.Request), n)
	for i := range sessions {
		sessions[i] = sessionOf(e, t)
	}
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "password-number-" + strconv.Itoa(i)}, sessions[i]).Code
		}()
	}
	wg.Wait()
	winner, ok := -1, 0
	for i, c := range codes {
		switch c {
		case 204:
			winner, ok = i, ok+1
		case 422:
		default:
			t.Fatalf("attempt %d: unexpected %d", i, c)
		}
	}
	if ok != 1 {
		t.Fatalf("%d password changes succeeded: %v", ok, codes)
	}
	// Check a few losers (the login limiter allows 5 failures) and the winner.
	checked := 0
	for i := 0; i < n && checked < 4; i++ {
		want := 401
		if i == winner {
			want = 200
		} else {
			checked++
		}
		if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "password-number-" + strconv.Itoa(i)}, nil); w.Code != want {
			t.Fatalf("login with password %d: %d, want %d", i, w.Code, want)
		}
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "password-number-" + strconv.Itoa(winner)}, nil); w.Code != 200 {
		t.Fatalf("the winner's password does not work: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, sessions[(winner+1)%n]); w.Code != 401 {
		t.Fatalf("a session of the losers survived: %d", w.Code)
	}
}

func TestConcurrentSessionRevoke(t *testing.T) {
	e := setup(t)
	a := sessionOf(e, t)
	_ = sessionOf(e, t)
	list := decodeBody[struct {
		Items []struct {
			ID      int64
			Current bool
		}
	}](t, e.do("GET", "/api/v1/me/sessions", nil, a))
	var other string
	for _, s := range list.Items {
		if !s.Current {
			other = strconv.FormatInt(s.ID, 10)
		}
	}
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = e.do("DELETE", "/api/v1/me/sessions/"+other, nil, a).Code
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 204 {
			ok++
		} else if c != 404 {
			t.Fatalf("unexpected %d", c)
		}
	}
	if ok != 1 {
		t.Fatalf("%d revokes succeeded: %v", ok, codes)
	}
}

func TestChangeEmail(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	if _, err := e.auth.CreateUser("other@example.com", "Other", "password-123", false); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]int{
		`wrong`: 422, `bad`: 400, `taken`: 422, `ok`: 200,
	} {
		in := map[string]any{"password": "correct-horse", "email": "new@example.com"}
		switch body {
		case "wrong":
			in["password"] = "nope"
		case "bad":
			in["email"] = "not an address"
		case "taken":
			in["email"] = "other@example.com"
		}
		if w := e.do("PUT", "/api/v1/me/email", in, kees); w.Code != want {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "new@example.com", "password": "correct-horse"}, nil); w.Code != 200 {
		t.Fatalf("sign in with the new address: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "kees@example.com", "password": "correct-horse"}, nil); w.Code != 401 {
		t.Fatalf("old address still works: %d", w.Code)
	}
}

func TestDeleteAccount(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	annID := strconv.FormatInt(ann.ID, 10)
	e.do("PUT", "/api/v1/projects/EXO/members/"+annID, map[string]any{"role": "admin"}, kees)
	annS := loginAs(e, t, "ann@example.com", "password-123")
	_, annTok, _ := e.auth.CreateToken(ann.ID, "t", []string{auth.ScopeRead}, nil, nil)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "by ann"}, annS)

	if w := e.do("DELETE", "/api/v1/me", map[string]any{"password": "nope"}, annS); w.Code != 422 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	// Kees is the only global admin; Ann is not the only project admin, so she may leave; Kees may not.
	if w := e.do("DELETE", "/api/v1/me", map[string]any{"password": "correct-horse"}, kees); w.Code != 422 {
		t.Fatalf("the only admin deleted themselves: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/me", map[string]any{"password": "password-123"}, annS); w.Code != 204 {
		t.Fatalf("delete account: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, annS); w.Code != 401 {
		t.Fatalf("session survived: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/projects", nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+annTok) }); w.Code != 401 {
		t.Fatalf("token survived: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "ann@example.com", "password": "password-123"}, nil); w.Code != 401 {
		t.Fatalf("deleted user signed in: %d", w.Code)
	}
	if body := e.do("GET", "/api/v1/users", nil, kees).Body.String(); !strings.Contains(body, "Deleted user") || strings.Contains(body, "ann@example.com") {
		t.Fatalf("user not anonymised: %s", body)
	}
	if body := e.do("GET", "/api/v1/items/EXO-1", nil, kees).Body.String(); !strings.Contains(body, "by ann") {
		t.Fatalf("her work disappeared: %s", body)
	}
	// A sole project admin cannot leave a project orphaned.
	bob, _ := e.auth.CreateUser("bob@example.com", "Bob", "password-123", false)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "BOB", "name": "BOB"}, loginAs(e, t, "bob@example.com", "password-123"))
	_ = bob
	if w := e.do("DELETE", "/api/v1/me", map[string]any{"password": "password-123"}, loginAs(e, t, "bob@example.com", "password-123")); w.Code != 422 {
		t.Fatalf("sole project admin deleted themselves: %d", w.Code)
	}
}

func TestTwoFactor(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	login := func(code string) *httptest.ResponseRecorder {
		in := map[string]string{"email": "kees@example.com", "password": "correct-horse"}
		if code != "" {
			in["code"] = code
		}
		return e.do("POST", "/api/v1/auth/login", in, nil)
	}
	setup := decodeBody[struct{ Secret, URI string }](t, e.do("POST", "/api/v1/me/totp/setup", nil, kees))
	if setup.Secret == "" || !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatalf("setup: %+v", setup)
	}
	if w := e.do("POST", "/api/v1/me/totp/enable", map[string]any{"code": "000000"}, kees); w.Code != 422 {
		t.Fatalf("enabled with a wrong code: %d", w.Code)
	}
	w := e.do("POST", "/api/v1/me/totp/enable", map[string]any{"code": auth.TOTPCode(setup.Secret, time.Now())}, kees)
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	recovery := decodeBody[struct {
		Codes []string `json:"recovery_codes"`
	}](t, w).Codes
	if len(recovery) != 10 {
		t.Fatalf("recovery codes: %v", recovery)
	}

	if w := login(""); w.Code != 401 || !strings.Contains(w.Body.String(), "totp_required") {
		t.Fatalf("sign in without a code: %d %s", w.Code, w.Body)
	}
	if w := login("123456"); w.Code != 401 || strings.Contains(w.Body.String(), "totp_required") {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body)
	}
	// The code used to enable counts as used: a code can be used only once.
	next := auth.TOTPCode(setup.Secret, time.Now().Add(30*time.Second))
	if w := login(next); w.Code != 200 {
		t.Fatalf("sign in with the next code: %d %s", w.Code, w.Body)
	}
	if w := login(next); w.Code != 401 {
		t.Fatalf("code replayed: %d", w.Code)
	}
	// A recovery code works once.
	if w := login(recovery[0]); w.Code != 200 {
		t.Fatalf("recovery code: %d %s", w.Code, w.Body)
	}
	if w := login(recovery[0]); w.Code != 401 {
		t.Fatalf("recovery code reused: %d", w.Code)
	}
	if sec := decodeBody[struct {
		TOTP      bool `json:"totp"`
		Remaining int  `json:"recovery_remaining"`
	}](t, e.do("GET", "/api/v1/me/security", nil, kees)); !sec.TOTP || sec.Remaining != 9 {
		t.Fatalf("security: %+v", sec)
	}

	// Switching it off needs the password and a code.
	if w := e.do("POST", "/api/v1/me/totp/disable", map[string]any{"password": "nope", "code": recovery[1]}, kees); w.Code != 422 {
		t.Fatalf("disable with a wrong password: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/me/totp/disable", map[string]any{"password": "correct-horse", "code": recovery[1]}, kees); w.Code != 204 {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if w := login(""); w.Code != 200 {
		t.Fatalf("sign in after disabling: %d", w.Code)
	}

	// An admin can reset it for someone who lost their device.
	e.do("POST", "/api/v1/me/totp/setup", nil, kees)
	s2 := decodeBody[struct{ Secret string }](t, e.do("POST", "/api/v1/me/totp/setup", nil, kees))
	e.do("POST", "/api/v1/me/totp/enable", map[string]any{"code": auth.TOTPCode(s2.Secret, time.Now())}, kees)
	if w := login(""); w.Code != 401 {
		t.Fatalf("2FA not on again: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/v1/admin/users/1", map[string]any{"reset_totp": true}, kees); w.Code != 204 {
		t.Fatalf("admin reset: %d", w.Code)
	}
	if w := login(""); w.Code != 200 {
		t.Fatalf("sign in after reset: %d", w.Code)
	}
}

func TestRegistrationPolicyAndInvites(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	reg := func(email, pw, invite string) int {
		return e.do("POST", "/api/v1/auth/register", map[string]any{"email": email, "name": "New", "password": pw, "invite": invite}, nil).Code
	}
	if c := reg("a@corp.example", "password-123", ""); c != 403 {
		t.Fatalf("registration while closed: %d", c)
	}
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	_ = ann
	if w := e.do("GET", "/api/v1/admin/policy", nil, loginAs(e, t, "ann@example.com", "password-123")); w.Code != 403 {
		t.Fatalf("non-admin read the policy: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 4, "registration": "open", "session_days": 14}, kees); w.Code != 400 {
		t.Fatalf("a policy below the floor: %d", w.Code)
	}
	if w := e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 12, "registration": "open", "allowed_domain": "@corp.example", "session_days": 7}, kees); w.Code != 200 {
		t.Fatalf("policy: %d %s", w.Code, w.Body)
	}
	if c := reg("a@corp.example", "short-pw", ""); c != 400 {
		t.Fatalf("password below the policy: %d", c)
	}
	if c := reg("a@elsewhere.example", "long-enough-pw!", ""); c != 422 {
		t.Fatalf("wrong domain: %d", c)
	}
	if c := reg("a@corp.example", "long-enough-pw!", ""); c != 201 {
		t.Fatalf("open registration: %d", c)
	}
	if w := e.do("POST", "/api/v1/auth/login", map[string]string{"email": "a@corp.example", "password": "long-enough-pw!"}, nil); w.Code != 200 {
		t.Fatalf("registered user cannot sign in: %d", w.Code)
	}
	// The policy also governs password changes made by existing users.
	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "correct-horse", "new": "twelve-chars"}, sessionOf(e, t)); w.Code != 204 {
		t.Fatalf("a password that meets the policy: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", "/api/v1/me/password", map[string]any{"current": "twelve-chars", "new": "eleven-char"}, loginAsPW(e, t, "twelve-chars")); w.Code != 400 {
		t.Fatalf("a password below the policy: %d", w.Code)
	}

	// Invites: single use, bound to an email when given, expiring, never stored in the clear or logged.
	kees = loginAsPW(e, t, "twelve-chars")
	e.do("PUT", "/api/v1/admin/policy", map[string]any{"min_password_length": 8, "registration": "invite", "session_days": 14}, kees)
	if c := reg("b@elsewhere.example", "password-123", ""); c != 403 {
		t.Fatalf("invite-only without an invite: %d", c)
	}
	w := e.do("POST", "/api/v1/admin/invites", map[string]any{"email": "b@elsewhere.example", "is_admin": true, "days": 3}, kees)
	if w.Code != 201 {
		t.Fatalf("create invite: %d %s", w.Code, w.Body)
	}
	code := decodeBody[struct{ Code string }](t, w).Code
	if c := reg("other@elsewhere.example", "password-123", code); c != 422 {
		t.Fatalf("invite for another address: %d", c)
	}
	if c := reg("b@elsewhere.example", "password-123", "bogus"); c != 422 {
		t.Fatalf("bogus invite: %d", c)
	}
	if c := reg("b@elsewhere.example", "password-123", code); c != 201 {
		t.Fatalf("redeem invite: %d", c)
	}
	if c := reg("b2@elsewhere.example", "password-123", code); c != 422 {
		t.Fatalf("invite reused: %d", c)
	}
	users := decodeBody[struct {
		Items []struct {
			Email   string
			IsAdmin bool `json:"is_admin"`
		}
	}](t, e.do("GET", "/api/v1/admin/users", nil, kees))
	admin := false
	for _, u := range users.Items {
		if u.Email == "b@elsewhere.example" {
			admin = u.IsAdmin
		}
	}
	if !admin {
		t.Fatal("the invite's admin flag was not applied")
	}
	if strings.Contains(e.do("GET", "/api/v1/admin/audit?limit=200", nil, kees).Body.String(), code) {
		t.Fatal("an invite code reached the audit log")
	}
	list := e.do("GET", "/api/v1/admin/invites", nil, kees).Body.String()
	if strings.Contains(list, code) || !strings.Contains(list, "used_at") {
		t.Fatalf("invite list: %s", list)
	}
}

func loginAsPW(e env, t *testing.T, password string) func(*http.Request) {
	return loginAs(e, t, "kees@example.com", password)
}

func TestAuditExport(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	e.do("POST", "/api/v1/projects", map[string]any{"key": "EXO", "name": "EXO"}, kees)
	e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "type": "task", "title": "t"}, kees)
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	_ = ann
	if w := e.do("GET", "/api/v1/admin/audit/export", nil, loginAs(e, t, "ann@example.com", "password-123")); w.Code != 403 {
		t.Fatalf("non-admin exported the audit log: %d", w.Code)
	}
	w := e.do("GET", "/api/v1/admin/audit/export", nil, kees)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.HasPrefix(w.Body.String(), "id,time,actor,operation,target,batch_id,changes") ||
		!strings.Contains(w.Body.String(), "item.create") || !strings.Contains(w.Body.String(), "project.create") {
		t.Fatalf("csv export: %d %s", w.Code, w.Body)
	}
	only := e.do("GET", "/api/v1/admin/audit/export?operation=item.", nil, kees).Body.String()
	if !strings.Contains(only, "item.create") || strings.Contains(only, "project.create") {
		t.Fatalf("filtered export: %s", only)
	}
	if j := e.do("GET", "/api/v1/admin/audit/export?format=json", nil, kees); !strings.Contains(j.Body.String(), `"operation":"item.create"`) {
		t.Fatalf("json export: %s", j.Body)
	}
}

func TestAvatar(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	put := func(name string, data []byte, as func(*http.Request)) int {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		part, _ := mw.CreateFormFile("file", name)
		part.Write(data)
		mw.Close()
		r := httptest.NewRequest("PUT", "/api/v1/me/avatar", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		as(r)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, r)
		return rec.Code
	}
	if w := e.do("GET", "/api/v1/users/1/avatar", nil, kees); w.Code != 404 {
		t.Fatalf("avatar before upload: %d", w.Code)
	}
	if c := put("me.txt", []byte("not an image"), kees); c != 400 {
		t.Fatalf("non-image: %d", c)
	}
	if c := put("me.png", pngBytes(t, 400, 300), kees); c != 204 {
		t.Fatalf("upload: %d", c)
	}
	// Anyone signed in sees it, as a small re-encoded JPEG, never the original.
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	_ = ann
	w := e.do("GET", "/api/v1/users/1/avatar", nil, loginAs(e, t, "ann@example.com", "password-123"))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serve: %d %v", w.Code, w.Header())
	}
	if w := e.do("GET", "/api/v1/users/1/avatar", nil, nil); w.Code != 401 {
		t.Fatalf("anonymous avatar: %d", w.Code)
	}
	if !strings.Contains(e.do("GET", "/api/v1/auth/me", nil, kees).Body.String(), `"avatar":true`) {
		t.Fatal("me does not report the avatar")
	}
	if w := e.do("DELETE", "/api/v1/me/avatar", nil, kees); w.Code != 204 {
		t.Fatalf("remove: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/users/1/avatar", nil, kees); w.Code != 404 {
		t.Fatalf("avatar after removal: %d", w.Code)
	}
}

func TestSubitemsAndInstanceTemplates(t *testing.T) {
	e := setup(t)
	kees := sessionOf(e, t)
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, kees)
	}
	ann, _ := e.auth.CreateUser("ann@example.com", "Ann", "password-123", false)
	e.do("PUT", "/api/v1/projects/EXO/members/"+strconv.FormatInt(ann.ID, 10), map[string]any{"role": "member"}, kees)
	annS := loginAs(e, t, "ann@example.com", "password-123")

	tpl := map[string]any{"name": "Release", "type": "story", "title_prefix": "Release: ", "subitems": []any{
		map[string]any{"type": "task", "title": "Write notes {date}"},
		map[string]any{"type": "bug", "title": "Smoke test", "children": []any{}},
	}}
	if w := e.do("POST", "/api/v1/item-templates", tpl, annS); w.Code != 403 {
		t.Fatalf("non-admin saved an instance template: %d", w.Code)
	}
	bad := map[string]any{"name": "Bad", "type": "task", "subitems": []any{map[string]any{"type": "story", "title": "x"}}}
	if w := e.do("POST", "/api/v1/item-templates", bad, kees); w.Code != 422 {
		t.Fatalf("a story under a task was accepted: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/item-templates", tpl, kees); w.Code != 200 {
		t.Fatalf("save instance template: %d %s", w.Code, w.Body)
	}

	// Usable in every project, by members; the whole tree appears together.
	w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "1.0", "template": "Release"}, annS)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	root := decodeBody[map[string]any](t, w)
	if root["title"] != "Release: 1.0" || len(root["subitems"].([]any)) != 2 {
		t.Fatalf("root: %v", root)
	}
	kids := e.do("GET", "/api/v1/items?project=EXO&parent="+root["key"].(string)+"&fields=key,title,type", nil, kees).Body.String()
	if !strings.Contains(kids, "Write notes 20") || !strings.Contains(kids, "Smoke test") {
		t.Fatalf("subitems: %s", kids)
	}
	if w := e.do("GET", "/api/v1/projects/OTH/workflow", nil, kees); !strings.Contains(w.Body.String(), `"Release"`) {
		t.Fatalf("instance template missing in another project: %s", w.Body)
	}

	// A project template of the same name wins in its project.
	e.do("POST", "/api/v1/projects/OTH/item-templates", map[string]any{"name": "Release", "type": "task", "title_prefix": "Local: "}, kees)
	loc := decodeBody[map[string]any](t, e.do("POST", "/api/v1/items", map[string]any{"project": "OTH", "title": "x", "template": "Release"}, kees))
	if loc["title"] != "Local: x" || loc["type"] != "task" {
		t.Fatalf("project template did not win: %v", loc)
	}

	// All or nothing: the subitems use the default status; when that blocks creating, nothing is created.
	before := e.do("GET", "/api/v1/items?project=EXO&fields=key", nil, kees).Body.String()
	wf := decodeBody[struct {
		Statuses []struct {
			ID   int64
			Name string
		}
	}](t, e.do("GET", "/api/v1/projects/EXO/workflow", nil, kees))
	for _, s := range wf.Statuses {
		if s.Name == "Backlog" {
			e.do("PATCH", "/api/v1/projects/EXO/statuses/"+strconv.FormatInt(s.ID, 10), map[string]any{"allow_create": false}, kees)
		}
	}
	if w := e.do("POST", "/api/v1/items", map[string]any{"project": "EXO", "title": "2.0", "template": "Release", "status": "Todo"}, annS); w.Code != 422 {
		t.Fatalf("subitem in a blocked default status: %d %s", w.Code, w.Body)
	}
	if after := e.do("GET", "/api/v1/items?project=EXO&fields=key", nil, kees).Body.String(); after != before {
		t.Fatalf("a failed template create left items behind:\n%s\n%s", before, after)
	}
}

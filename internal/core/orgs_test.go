package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

func (e *env) org(key string, admin int64) {
	e.t.Helper()
	if _, err := e.svc.CreateOrganization(context.Background(), e.admin, key, key+" org", admin); err != nil {
		e.t.Fatal(err)
	}
}

func TestOrganizationPermissions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganization(ctx, e.alice, "ACME", "Acme", 0); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-admin created an organization: %v", err)
	}
	e.org("ACME", e.aliceID)
	e.org("OTHER", e.bobID)
	if err := e.svc.SetOrgMember(ctx, e.alice, "ACME", e.carolID, "user", false); err != nil {
		t.Fatal(err)
	}
	// visibility first: another organization's ADMIN gets 404, a plain member 403
	if _, err := e.svc.GetOrganization(ctx, e.bob, "ACME"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("foreign admin: %v", err)
	}
	if err := e.svc.SetOrgMember(ctx, e.bob, "ACME", e.bobID, "admin", false); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("foreign admin could manage members: %v", err)
	}
	if err := e.svc.SetOrgMember(ctx, e.carol, "ACME", e.carolID, "admin", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a plain member promoted themselves: %v", err)
	}
	if _, err := e.svc.UpdateOrganization(ctx, e.carol, "ACME", "Mine"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a plain member renamed the organization: %v", err)
	}
	if err := e.svc.SetOrganizationArchived(ctx, e.alice, "ACME", true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("an organization admin archived it: %v", err)
	}
	if o, err := e.svc.UpdateOrganization(ctx, e.alice, "ACME", "Acme BV"); err != nil || o.Name != "Acme BV" {
		t.Fatalf("admin rename: %v %+v", err, o)
	}
	if list, _ := e.svc.ListOrganizations(ctx, e.carol); len(list) != 1 || list[0].Key != "ACME" {
		t.Fatalf("a member must only list their own organizations: %+v", list)
	}
	if list, _ := e.svc.ListOrganizations(ctx, e.admin); len(list) != 3 { // MAIN from setup, ACME, OTHER
		t.Fatalf("a SUPERADMIN lists all: %+v", list)
	}
}

func TestLastOrganizationAdminGuard(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.org("ACME", e.aliceID)
	for name, err := range map[string]error{
		"demote":  e.svc.SetOrgMember(ctx, e.alice, "ACME", e.aliceID, "user", false),
		"remove":  e.svc.RemoveOrgMember(ctx, e.alice, "ACME", e.aliceID, false),
		"suspend": e.svc.SetUserActive(ctx, e.admin, e.aliceID, false),
	} {
		var re *core.RuleError
		if !errors.As(err, &re) {
			t.Errorf("%s: the last admin was lost: %v", name, err)
		}
	}
	// allow_orphan is for a SUPERADMIN only
	if err := e.svc.SetOrgMember(ctx, e.alice, "ACME", e.aliceID, "user", true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("allow_orphan by an organization admin: %v", err)
	}
	if err := e.svc.SetOrgMember(ctx, e.admin, "ACME", e.aliceID, "user", true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a SUPERADMIN without a support grant changed an organization's admins: %v", err)
	}
	if _, err := e.svc.GrantSupport(ctx, e.alice, "ACME", "full", 1, false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetOrgMember(ctx, e.admin, "ACME", e.aliceID, "user", true); err != nil {
		t.Fatal(err)
	}
	o, _ := e.svc.GetOrganization(ctx, e.admin, "ACME")
	if !o.NoActiveAdmin {
		t.Fatal("an organization without active admin must be flagged")
	}
	// a suspended admin does not count: with bob suspended, carol is the last active admin
	e.org("TWO", e.bobID)
	if err := e.svc.SetOrgMember(ctx, e.bob, "TWO", e.carolID, "admin", false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetUserActive(ctx, e.admin, e.bobID, false); err != nil {
		t.Fatalf("suspending one of two admins: %v", err)
	}
	if err := e.svc.SetUserActive(ctx, e.admin, e.carolID, false); err == nil {
		t.Fatal("suspending the last active admin was allowed")
	}
}

func TestOrganizationMembershipAndArchive(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.org("ACME", e.aliceID)
	e.project("EXO")
	if _, err := e.db.W.Exec(`UPDATE projects SET organization_id = (SELECT id FROM organizations WHERE key = 'ACME') WHERE key = 'EXO'`); err != nil {
		t.Fatal(err)
	}
	// a project member must belong to the project's organization
	if err := e.svc.SetMember(ctx, e.alice, "EXO", e.bobID, "member"); err == nil {
		t.Fatal("a user outside the organization became a project member")
	}
	if err := e.svc.SetOrgMember(ctx, e.alice, "ACME", e.bobID, "user", false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetMember(ctx, e.alice, "EXO", e.bobID, "member"); err != nil {
		t.Fatal(err)
	}
	// archive hides the project from everyone, the SUPERADMIN included; the organization itself stays listed
	if err := e.svc.SetOrganizationArchived(ctx, e.admin, "ACME", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetProject(ctx, e.bob, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("archived organization still shows its project: %v", err)
	}
	if list, _ := e.svc.ListProjects(ctx, e.bob); len(list) != 0 {
		t.Fatalf("archived organization still lists its project: %+v", list)
	}
	if _, err := e.svc.GetProject(ctx, e.admin, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("an archived organization's project is visible to the SUPERADMIN: %v", err)
	}
	if o, err := e.svc.GetOrganization(ctx, e.admin, "ACME"); err != nil || o.Status != "archived" {
		t.Fatalf("the SUPERADMIN must still see the archived organization: %v %+v", err, o)
	}
	if err := e.svc.SetOrganizationArchived(ctx, e.admin, "ACME", false); err != nil {
		t.Fatal(err)
	}
	// removing the organization membership removes the project rows too
	if err := e.svc.RemoveOrgMember(ctx, e.alice, "ACME", e.bobID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetProject(ctx, e.bob, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a removed member kept project access: %v", err)
	}
}

// A failing audit write must roll the membership change back (ATL-427).
func TestOrganizationMembershipRollsBackWithAudit(t *testing.T) {
	e := setup(t)
	e.org("ACME", e.aliceID)
	if _, err := e.db.W.Exec(`ALTER TABLE audit_entries RENAME TO audit_entries_gone`); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetOrgMember(context.Background(), e.alice, "ACME", e.bobID, "user", false); err == nil {
		t.Fatal("the change succeeded without an audit entry")
	}
	var n int
	e.db.R.QueryRow(`SELECT COUNT(*) FROM organization_memberships WHERE user_id = ?`, e.bobID).Scan(&n)
	if n != 0 {
		t.Fatal("the membership survived a failed audit write")
	}
}

// Two handles on one database file (as two processes would): concurrent demotions and removals of the two admins
// must leave exactly one (ADR-0029: the guard does not rely on a single connection).
func TestLastOrganizationAdminConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atlas.db")
	open := func() (*core.Service, *db.DB) {
		d, err := db.Open(path, db.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		return core.New(d, auth.NewService(d)), d
	}
	one, d1 := open()
	two, _ := open()
	au := auth.NewService(d1)
	mk := func(email string, admin bool) auth.Actor {
		u, err := au.CreateUser(email, email, "password-123", admin)
		if err != nil {
			t.Fatal(err)
		}
		return human(u.ID, admin)
	}
	super, b, c := mk("s@x.io", true), mk("b@x.io", false), mk("c@x.io", false)
	ctx := context.Background()
	for round := 0; round < 10; round++ {
		key := fmt.Sprintf("ORG%d", round)
		if _, err := one.CreateOrganization(ctx, super, key, key, b.UserID); err != nil {
			t.Fatal(err)
		}
		if err := one.SetOrgMember(ctx, b, key, c.UserID, "admin", false); err != nil {
			t.Fatal(err)
		}
		ops := []func() error{
			func() error { return one.SetOrgMember(ctx, b, key, c.UserID, "user", false) },
			func() error { return two.SetOrgMember(ctx, c, key, b.UserID, "user", false) },
		}
		if round%2 == 1 {
			ops = []func() error{
				func() error { return one.RemoveOrgMember(ctx, b, key, c.UserID, false) },
				func() error { return two.RemoveOrgMember(ctx, c, key, b.UserID, false) },
			}
		}
		var wg sync.WaitGroup
		errs := make([]error, len(ops))
		for i, op := range ops {
			wg.Add(1)
			go func() { defer wg.Done(); errs[i] = op() }()
		}
		wg.Wait()
		if errs[0] == nil && errs[1] == nil {
			t.Fatalf("round %d: both operations succeeded", round)
		}
		o, err := one.GetOrganization(ctx, super, key)
		if err != nil || o.NoActiveAdmin {
			t.Fatalf("round %d: organization lost its last admin: %v %+v", round, err, o)
		}
	}
}

// Table of actor x action on one project (ATL-479): SUPERADMIN without membership, organization ADMIN (inherited),
// project member, organization member without project, ADMIN of another organization, nobody, suspended.
// Hidden is 404, lacking a right is 403.
func TestProjectAccessMatrix(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO") // in MAIN
	e.org("OTHER", e.carolID)
	if err := e.member("EXO", e.bobID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := e.member("", e.aliceID, "admin"); err != nil { // alice administers MAIN
		t.Fatal(err)
	}
	e.orgUser(mkUser(t, e, "dan@x.io")) // dan: organization member without a project row
	super2 := human(mkUserWith(t, e, "super2@x.io", true), true)
	dan := human(mkUser(t, e, "dan2@x.io"), false) // a second one, to keep dan's membership untouched
	e.orgUser(dan.UserID)
	nobody := human(mkUser(t, e, "nobody@x.io"), false)
	gone := human(mkUser(t, e, "gone@x.io"), false)
	if err := e.member("EXO", gone.UserID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetUserActive(ctx, e.admin, gone.UserID, false); err != nil {
		t.Fatal(err)
	}
	actions := map[string]func(a auth.Actor) error{
		"view": func(a auth.Actor) error { _, err := e.svc.GetProject(ctx, a, "EXO"); return err },
		"item.create": func(a auth.Actor) error {
			_, err := e.svc.CreateItem(ctx, a, core.CreateItem{Project: "EXO", Type: "task", Title: "t"})
			return err
		},
		"project.update": func(a auth.Actor) error {
			_, err := e.svc.UpdateProject(ctx, a, "EXO", map[string]any{"name": "EXO"})
			return err
		},
		"members": func(a auth.Actor) error { return e.svc.SetMember(ctx, a, "EXO", e.bobID, "member") },
	}
	all := func(err error) map[string]error {
		return map[string]error{"view": err, "item.create": err, "project.update": err, "members": err}
	}
	rows := []struct {
		who  string
		a    auth.Actor
		want map[string]error // nil = allowed
	}{
		{"SUPERADMIN without membership or grant", super2, all(core.ErrNotFound)},
		{"organization ADMIN (inherited)", e.alice, all(nil)},
		{"project member", e.bob, map[string]error{"view": nil, "item.create": nil, "project.update": core.ErrForbidden, "members": core.ErrForbidden}},
		{"organization member without project", dan, all(core.ErrNotFound)},
		{"ADMIN of another organization", e.carol, all(core.ErrNotFound)},
		{"user without organization", nobody, all(core.ErrNotFound)},
		{"suspended member", gone, all(core.ErrForbidden)},
	}
	check := func(who string, a auth.Actor, want map[string]error) {
		t.Helper()
		for act, f := range actions {
			got := f(a)
			if (want[act] == nil) != (got == nil) || (got != nil && !errors.Is(got, want[act])) {
				t.Errorf("%s / %s: got %v, want %v", who, act, got, want[act])
			}
		}
	}
	for _, r := range rows {
		check(r.who, r.a, r.want)
	}

	// Support access (ADR-0031): only the ADMIN of the organization gives it, only a SUPERADMIN session uses it.
	if _, err := e.svc.GrantSupport(ctx, super2, "MAIN", "full", 1, false); !errors.Is(err, core.ErrForbidden) && !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a SUPERADMIN granted themselves support access: %v", err)
	}
	if _, err := e.svc.GrantSupport(ctx, e.bob, "MAIN", "full", 1, false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a member granted support access: %v", err)
	}
	ro, err := e.svc.GrantSupport(ctx, e.alice, "MAIN", "read_only", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	check("SUPERADMIN with a read-only grant", super2, map[string]error{"view": nil, "item.create": core.ErrForbidden, "project.update": core.ErrForbidden, "members": core.ErrForbidden})
	token := auth.Actor{Kind: auth.KindToken, UserID: super2.UserID, TokenID: 1, Scopes: []string{auth.ScopeRead, auth.ScopeWrite}}
	check("token of a SUPERADMIN with a grant", token, all(core.ErrNotFound))
	if err := e.svc.RevokeSupport(ctx, e.alice, "MAIN", ro.ID); err != nil {
		t.Fatal(err)
	}
	check("SUPERADMIN after the grant was revoked", super2, all(core.ErrNotFound))
	if _, err := e.svc.GrantSupport(ctx, e.alice, "MAIN", "full", 1, false); err != nil {
		t.Fatal(err)
	}
	check("SUPERADMIN with a full grant", super2, all(nil))
	if err := e.svc.SetOrgMember(ctx, super2, "MAIN", super2.UserID, "admin", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a SUPERADMIN under a grant made their own access permanent: %v", err)
	}
	if _, err := e.svc.GrantSupport(ctx, super2, "MAIN", "full", 720, false); err == nil {
		t.Fatal("a SUPERADMIN under a grant created another grant")
	}
	// expiry: the same grant stops working when its time has passed
	later := time.Now().Add(2 * time.Hour)
	e.svc.SetClock(func() time.Time { return later })
	check("SUPERADMIN after the grant expired", super2, all(core.ErrNotFound))
}

func mkUser(t *testing.T, e *env, email string) int64 {
	t.Helper()
	u, err := auth.NewService(e.db).CreateUser(email, email, "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func mkUserWith(t *testing.T, e *env, email string, superadmin bool) int64 {
	t.Helper()
	u, err := auth.NewService(e.db).CreateUser(email, email, "password-123", superadmin)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// ATL-493: people are visible only through a shared organization or project (ADR-0027).
func TestUserListingIsScopedToOrganizations(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")                                           // MAIN: admin = the SUPERADMIN
	if err := e.member("EXO", e.bobID, "member"); err != nil { // bob works on EXO
		t.Fatal(err)
	}
	e.orgUser(e.carolID)                                    // carol: MAIN member, no shared project with bob
	e.org("ACME", e.aliceID)                                // alice administers ACME, a different organization
	stranger := human(mkUser(t, e, "stranger@x.io"), false) // belongs to nothing
	sees := func(a auth.Actor) map[string]bool {
		list, err := e.svc.ListUsers(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, u := range list {
			m[u.Email] = true
			if u.IsSuperadmin && a.UserID != e.admin.UserID {
				t.Errorf("%d sees who is SUPERADMIN", a.UserID)
			}
		}
		return m
	}
	for name, c := range map[string]struct {
		who  auth.Actor
		want []string
		not  []string
	}{
		"member sees self and the organization's admin":     {e.bob, []string{"bob@x.io", "admin@x.io"}, []string{"carol@x.io", "alice@x.io", "stranger@x.io"}},
		"organization member without a shared project":      {e.carol, []string{"carol@x.io", "admin@x.io"}, []string{"bob@x.io", "alice@x.io"}},
		"admin of another organization":                     {e.alice, []string{"alice@x.io"}, []string{"bob@x.io", "carol@x.io", "admin@x.io"}},
		"nobody":                                            {stranger, []string{"stranger@x.io"}, []string{"bob@x.io", "alice@x.io", "admin@x.io"}},
		"SUPERADMIN (people outside a shared organization)": {e.admin, []string{"admin@x.io"}, []string{"stranger@x.io", "alice@x.io"}},
	} {
		got := sees(c.who)
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%s: %s is missing", name, w)
			}
		}
		for _, n := range c.not {
			if got[n] {
				t.Errorf("%s: %s must not be visible", name, n)
			}
		}
	}
	// the organization's ADMIN sees every member of it
	e.orgUser(e.bobID)
	if err := e.member("", e.carolID, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := sees(e.carol); !got["bob@x.io"] || got["alice@x.io"] {
		t.Errorf("an organization admin sees its members only: %v", got)
	}
}

// ADR-0031: the Atlas admin appoints the first ADMIN of an organization that has none, and only then.
func TestSuperadminAppointsFirstAdminOnly(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.org("EMPTY", e.aliceID)
	e.org("BUSY", e.carolID)
	if err := e.svc.SetOrgMember(ctx, e.admin, "BUSY", e.bobID, "admin", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("the SUPERADMIN added an ADMIN to an organization that has one: %v", err)
	}
	if _, err := e.db.W.Exec(`UPDATE organization_memberships SET role = 'user' WHERE user_id = ? AND organization_id = (SELECT id FROM organizations WHERE key = 'EMPTY')`, e.aliceID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetOrgMember(ctx, e.admin, "EMPTY", e.admin.UserID, "admin", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("the SUPERADMIN appointed themselves: %v", err)
	}
	if err := e.svc.SetOrgMember(ctx, e.admin, "EMPTY", e.bobID, "admin", false); err != nil {
		t.Fatalf("appointing the first admin: %v", err)
	}
	if err := e.svc.SetOrgMember(ctx, e.admin, "EMPTY", e.carolID, "admin", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a second appointment after the organization has an ADMIN again: %v", err)
	}
}

// ADR-0031: the Atlas admin creates an organization for a named first ADMIN and does not become a member.
func TestSuperadminCreatesOrganizationWithoutJoining(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganization(ctx, e.admin, "NEWO", "New", 0); err == nil {
		t.Fatal("an organization was created without a first admin")
	}
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "NEWO", "New", "nobody@x.io"); err == nil {
		t.Fatal("an unknown email became the first admin")
	}
	if _, err := e.svc.CreateOrganizationFor(ctx, e.alice, "NEWO", "New", "bob@x.io"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN created an organization: %v", err)
	}
	o, err := e.svc.CreateOrganizationFor(ctx, e.admin, "NEWO", "New", "bob@x.io")
	if err != nil {
		t.Fatal(err)
	}
	if o.Role != "" {
		t.Fatalf("the creating SUPERADMIN became a member (%q)", o.Role)
	}
	list, err := e.svc.ListAdminOrganizations(ctx, e.admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Key == "NEWO" && (len(x.Admins) != 1 || x.Admins[0] != "bob@x.io" || x.Members != 1) {
			t.Fatalf("admin view of the organization: %+v", x)
		}
	}
	if _, err := e.svc.ListAdminOrganizations(ctx, e.alice); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN listed all organizations with their admins: %v", err)
	}
}

// ADR-0031: organization applications: setting, limits, SUPERADMIN-only decisions, one winner, key collision.
func TestOrganizationApplications(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	setCreation := func(mode string) {
		if _, err := e.svc.SetPolicy(ctx, e.admin, core.Policy{MinPasswordLength: 8, Registration: "closed", OrganizationCreation: mode, SessionDays: 14, DoneVisibleDays: 14}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.ApplyForOrganization(ctx, e.bob, "BOBCO", "Bob Co", ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("applying while only the Atlas admin creates organizations: %v", err)
	}
	setCreation("apply")
	token := auth.Actor{Kind: auth.KindToken, UserID: e.bobID, TokenID: 1, Scopes: []string{auth.ScopeRead, auth.ScopeWrite}}
	if _, err := e.svc.ApplyForOrganization(ctx, token, "BOBCO", "Bob Co", ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a token applied: %v", err)
	}
	if _, err := e.svc.ApplyForOrganization(ctx, e.bob, "1", "Bob Co", ""); err == nil {
		t.Fatal("an invalid key was accepted")
	}
	// the key MAIN exists: the applicant is not told
	first, err := e.svc.ApplyForOrganization(ctx, e.bob, "MAIN", "Bob Co", "please")
	if err != nil {
		t.Fatalf("applying with a key that exists: %v", err)
	}
	if _, err := e.svc.ApplyForOrganization(ctx, e.bob, "BOBCO", "Bob Co", ""); err == nil {
		t.Fatal("a second waiting application was accepted")
	}
	if _, err := e.svc.ListApplications(ctx, e.alice, ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN read the queue: %v", err)
	}
	if err := e.svc.DecideApplication(ctx, e.alice, first.ID, "approve", "", ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN decided: %v", err)
	}
	queue, err := e.svc.ListApplications(ctx, e.admin, "")
	if err != nil || len(queue) != 1 || queue[0].ApplicantEmail != "bob@x.io" {
		t.Fatalf("queue: %v %+v", err, queue)
	}
	// the collision shows at decision time and leaves the application waiting; another key works
	var re *core.RuleError
	if err := e.svc.DecideApplication(ctx, e.admin, first.ID, "approve", "", ""); !errors.As(err, &re) {
		t.Fatalf("approving onto an existing key: %v", err)
	}
	if err := e.svc.DecideApplication(ctx, e.admin, first.ID, "approve", "", "BOBCO"); err != nil {
		t.Fatal(err)
	}
	o, err := e.svc.GetOrganization(ctx, e.bob, "BOBCO")
	if err != nil || o.Role != "admin" {
		t.Fatalf("the applicant must be the first ADMIN: %v %+v", err, o)
	}
	if mine, _ := e.svc.GetOrganization(ctx, e.admin, "BOBCO"); mine.Role != "" {
		t.Fatalf("the SUPERADMIN joined the new organization: %q", mine.Role)
	}
	if err := e.svc.DecideApplication(ctx, e.admin, first.ID, "reject", "", ""); !errors.As(err, &re) {
		t.Fatalf("deciding twice: %v", err)
	}

	// concurrent decisions: exactly one wins
	second, err := e.svc.ApplyForOrganization(ctx, e.carol, "CAROL", "Carol Co", "")
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for _, d := range []string{"approve", "reject"} {
		go func() { errs <- e.svc.DecideApplication(ctx, e.admin, second.ID, d, "", "") }()
	}
	if a, b := <-errs, <-errs; (a == nil) == (b == nil) {
		t.Fatalf("two concurrent decisions: %v and %v, want exactly one success", a, b)
	}

	// rejection: wait 24 h, then at most 3 applications per 30 days
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now })
	third, err := e.svc.ApplyForOrganization(ctx, e.alice, "ALICE1", "Alice 1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DecideApplication(ctx, e.admin, third.ID, "reject", "no", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ApplyForOrganization(ctx, e.alice, "ALICE2", "Alice 2", ""); !errors.As(err, &re) {
		t.Fatalf("re-applying within 24 h: %v", err)
	}
	for i, key := range []string{"ALICE2", "ALICE3"} {
		now = now.Add(25 * time.Hour)
		ap, err := e.svc.ApplyForOrganization(ctx, e.alice, key, key, "")
		if err != nil {
			t.Fatalf("re-apply %d: %v", i, err)
		}
		if err := e.svc.DecideApplication(ctx, e.admin, ap.ID, "reject", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(25 * time.Hour)
	if _, err := e.svc.ApplyForOrganization(ctx, e.alice, "ALICE4", "Alice 4", ""); !errors.As(err, &re) {
		t.Fatalf("a 4th application inside 30 days: %v", err)
	}
}

package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

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
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.bobID, "member"); err == nil {
		t.Fatal("a user outside the organization became a project member")
	}
	if err := e.svc.SetOrgMember(ctx, e.alice, "ACME", e.bobID, "user", false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.bobID, "member"); err != nil {
		t.Fatal(err)
	}
	// archive hides the project from members, not from the SUPERADMIN
	if err := e.svc.SetOrganizationArchived(ctx, e.admin, "ACME", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetProject(ctx, e.bob, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("archived organization still shows its project: %v", err)
	}
	if list, _ := e.svc.ListProjects(ctx, e.bob); len(list) != 0 {
		t.Fatalf("archived organization still lists its project: %+v", list)
	}
	if _, err := e.svc.GetProject(ctx, e.admin, "EXO"); err != nil {
		t.Fatal(err)
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
		{"SUPERADMIN without membership", super2, all(nil)},
		{"organization ADMIN (inherited)", e.alice, all(nil)},
		{"project member", e.bob, map[string]error{"view": nil, "item.create": nil, "project.update": core.ErrForbidden, "members": core.ErrForbidden}},
		{"organization member without project", dan, all(core.ErrNotFound)},
		{"ADMIN of another organization", e.carol, all(core.ErrNotFound)},
		{"user without organization", nobody, all(core.ErrNotFound)},
		{"suspended member", gone, all(core.ErrForbidden)},
	}
	for _, r := range rows {
		for act, f := range actions {
			got := f(r.a)
			if (r.want[act] == nil) != (got == nil) || (got != nil && !errors.Is(got, r.want[act])) {
				t.Errorf("%s / %s: got %v, want %v", r.who, act, got, r.want[act])
			}
		}
	}
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

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

// ADR-0031: invitations by the organization ADMIN: who may invite, single use, expiry, no role change, no
// disclosure, other-organization inviter, projects, concurrent accept.
func TestOrganizationInvites(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("PRJ")
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	// only an ADMIN of that organization invites; a plain member or an ADMIN of another one is refused
	e.orgUser(e.bobID)
	if _, _, err := e.svc.CreateOrgInvite(ctx, e.bob, "MAIN", "new@x.io", nil, 7); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER invited: %v", err)
	}
	if _, _, err := e.svc.CreateOrgInvite(ctx, e.carol, "MAIN", "new@x.io", nil, 7); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization invited (want not found): %v", err)
	}
	if _, _, err := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "new@x.io", []string{"NOPE"}, 7); !isInvalid(err) {
		t.Fatalf("an unknown project was accepted: %v", err)
	}

	// a new address: preview, then account creation verifies the email and joins as USER with the project
	inv, token, err := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "New@X.io", []string{"prj"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := e.svc.PreviewOrgInvite(token); err != nil || p.AccountExists || p.Organization != "Main" {
		t.Fatalf("preview: %v %+v", err, p)
	}
	if _, err := e.svc.AcceptOrgInviteNew(ctx, token, "New", "short"); err == nil {
		t.Fatal("a weak password was accepted")
	}
	if _, err := e.svc.AcceptOrgInviteNew(ctx, "wrong", "New", "password-123"); !isRule(err) {
		t.Fatalf("a wrong token: %v", err)
	}
	if org, err := e.svc.AcceptOrgInviteNew(ctx, token, "New", "password-123"); err != nil || org != "MAIN" {
		t.Fatalf("accept: %v %q", err, org)
	}
	if n := e.count(`SELECT COUNT(*) FROM users u JOIN organization_memberships m ON m.user_id = u.id AND m.role = 'user'
		JOIN project_members p ON p.user_id = u.id WHERE u.email = 'new@x.io' AND u.email_verified_at IS NOT NULL`); n != 1 {
		t.Fatalf("the new account is not a verified USER with the project: %d", n)
	}
	if _, err := e.svc.AcceptOrgInviteNew(ctx, token, "New", "password-123"); !isRule(err) {
		t.Fatalf("reusing a token: %v", err)
	}
	if list, _ := e.svc.ListOrgInvites(ctx, e.admin, "MAIN"); len(list) != 1 || list[0].State != "accepted" || list[0].ID != inv.ID {
		t.Fatalf("list: %+v", list)
	}

	// an existing account must sign in as the invited address; a verified email is recorded, an ADMIN is never demoted
	_, tAlice, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "alice@x.io", nil, 7)
	if _, err := e.svc.AcceptOrgInviteNew(ctx, tAlice, "Alice", "password-123"); !isRule(err) {
		t.Fatalf("taking over an existing account by registering: %v", err)
	}
	if _, err := e.svc.AcceptOrgInvite(ctx, e.bob, tAlice); !isRule(err) {
		t.Fatalf("accepting as another account: %v", err)
	}
	if _, err := e.svc.AcceptOrgInvite(ctx, e.alice, tAlice); err != nil {
		t.Fatal(err)
	}
	_, tAdmin, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "admin@x.io", nil, 7)
	if _, err := e.svc.AcceptOrgInvite(ctx, e.admin, tAdmin); err != nil {
		t.Fatal(err)
	}
	if o, _ := e.svc.GetOrganization(ctx, e.admin, "MAIN"); o.Role != "admin" {
		t.Fatalf("an invitation changed the ADMIN's role to %q", o.Role)
	}

	// revoke and expiry both read as "not valid"; a new invitation replaces the waiting one
	_, tRev, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "gone@x.io", nil, 7)
	waiting, _ := e.svc.ListOrgInvites(ctx, e.admin, "MAIN")
	if err := e.svc.RevokeOrgInvite(ctx, e.bob, "MAIN", waiting[0].ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER revoked: %v", err)
	}
	if err := e.svc.RevokeOrgInvite(ctx, e.admin, "MAIN", waiting[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PreviewOrgInvite(tRev); !isRule(err) {
		t.Fatalf("a revoked invitation: %v", err)
	}
	_, tOld, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "twice@x.io", nil, 7)
	_, tNew, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "twice@x.io", nil, 1)
	if _, err := e.svc.PreviewOrgInvite(tOld); !isRule(err) {
		t.Fatalf("the replaced invitation still works: %v", err)
	}
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now.Add(25 * time.Hour) })
	if _, err := e.svc.PreviewOrgInvite(tNew); !isRule(err) {
		t.Fatalf("an expired invitation: %v", err)
	}
	e.svc.SetClock(func() time.Time { return now })

	// concurrent accepts: exactly one wins
	_, tRace, _ := e.svc.CreateOrgInvite(ctx, e.admin, "MAIN", "race@x.io", nil, 7)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.AcceptOrgInviteNew(ctx, tRace, "Race", "password-123")
			errs <- err
		}()
	}
	wg.Wait()
	if a, b := <-errs, <-errs; (a == nil) == (b == nil) {
		t.Fatalf("two concurrent accepts: %v and %v, want exactly one success", a, b)
	}
}

package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func TestAdminUserListFiltersAndShowsMemberships(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	if err := e.member("EXO", e.bobID, "member"); err != nil {
		t.Fatal(err)
	}
	all, total, err := e.svc.ListAdminUsers(ctx, e.admin, core.AdminUserFilter{})
	if err != nil || total != len(all) || total < 4 {
		t.Fatalf("list: %v total %d len %d", err, total, len(all))
	}
	var bob *core.AdminUser
	for i := range all {
		if all[i].ID == e.bobID {
			bob = &all[i]
		}
	}
	if bob == nil || len(bob.Organizations) != 1 || bob.Organizations[0].Key != "MAIN" || bob.Organizations[0].Role != "user" {
		t.Fatalf("memberships missing: %+v", bob)
	}
	page, total2, _ := e.svc.ListAdminUsers(ctx, e.admin, core.AdminUserFilter{Limit: 2, Offset: 1})
	if len(page) != 2 || total2 != total {
		t.Fatalf("pagination: %d items, total %d (want 2 of %d)", len(page), total2, total)
	}
	if got, n, _ := e.svc.ListAdminUsers(ctx, e.admin, core.AdminUserFilter{Q: "nobody-matches"}); len(got) != 0 || n != 0 {
		t.Fatalf("query filter: %+v", got)
	}
	if got, _, _ := e.svc.ListAdminUsers(ctx, e.admin, core.AdminUserFilter{Status: "pending"}); len(got) != 0 {
		t.Fatalf("status filter: %+v", got)
	}
	if _, _, err := e.svc.ListAdminUsers(ctx, e.bob, core.AdminUserFilter{}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a plain user listed accounts: %v", err)
	}
}

func TestSearchOrgMembersIsScoped(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	if err := e.member("EXO", e.carolID, "admin"); err != nil {
		t.Fatal(err)
	}
	e.orgUser(e.bobID)
	e.org("OTH", e.aliceID) // alice is no member of MAIN
	if _, err := e.svc.SearchOrgMembers(ctx, e.carol, "MAIN", "b"); err == nil {
		t.Fatal("a one character search was accepted")
	}
	all, err := e.svc.ListOrgMembers(ctx, e.carol, "MAIN")
	if err != nil || len(all) == 0 {
		t.Fatalf("members: %v", err)
	}
	got, err := e.svc.SearchOrgMembers(ctx, e.carol, "MAIN", all[0].Email[:3])
	if err != nil || len(got) == 0 {
		t.Fatalf("search: %v %+v", err, got)
	}
	for _, m := range got {
		if m.UserID == e.aliceID {
			t.Fatalf("a user of another organization was found: %+v", m)
		}
	}
	if _, err := e.svc.SearchOrgMembers(ctx, e.bob, "MAIN", "ab"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a plain member searched: %v", err)
	}
	if _, err := e.svc.SearchOrgMembers(ctx, e.alice, "MAIN", "ab"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another organization's ADMIN searched: %v", err)
	}
}

func TestSetMemberProjectsIsAtomicAndScoped(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	e.project("PUL")
	if err := e.member("EXO", e.carolID, "admin"); err != nil {
		t.Fatal(err)
	}
	e.orgUser(e.bobID)
	e.org("OTH", e.aliceID)
	if _, err := e.svc.CreateProjectIn(ctx, e.alice, "OTH", "FOR", "foreign"); err != nil {
		t.Fatal(err)
	}
	members := func() int {
		return e.count(`SELECT COUNT(*) FROM project_members WHERE user_id = ?`, e.bobID)
	}
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.bobID, []string{"EXO", "PUL"}); err != nil || members() != 2 {
		t.Fatalf("assign: %v, %d memberships", err, members())
	}
	// one foreign project rejects the whole request: nothing changes
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.bobID, []string{"EXO", "FOR"}); err == nil || members() != 2 {
		t.Fatalf("foreign project: %v, %d memberships (want 2, unchanged)", err, members())
	}
	// follows of a dropped project go with it
	it := e.item(e.carol, core.CreateItem{Project: "PUL", Type: "task", Title: "x"})
	if err := e.svc.Follow(ctx, e.bob, it.Key); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.bobID, []string{"EXO"}); err != nil || members() != 1 {
		t.Fatalf("shrink: %v, %d memberships", err, members())
	}
	if n := e.count(`SELECT COUNT(*) FROM item_follows WHERE user_id = ?`, e.bobID); n != 0 {
		t.Fatalf("%d follows survived", n)
	}
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.bobID, nil); err != nil || members() != 0 {
		t.Fatalf("clear: %v", err)
	}
	if err := e.svc.SetMemberProjects(ctx, e.bob, "MAIN", e.bobID, []string{"EXO"}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER assigned projects: %v", err)
	}
	if err := e.svc.SetMemberProjects(ctx, e.alice, "MAIN", e.bobID, nil); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another organization's ADMIN: %v", err)
	}
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.carolID, []string{"EXO"}); err == nil {
		t.Fatal("an ADMIN got an explicit project set")
	}
	if err := e.svc.SetMemberProjects(ctx, e.carol, "MAIN", e.aliceID, nil); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a person outside the organization: %v", err)
	}
}

func TestInviteCreationIsCapped(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	if err := e.member("EXO", e.carolID, "admin"); err != nil {
		t.Fatal(err)
	}
	var last error
	for i := 0; i < 51; i++ {
		_, _, last = e.svc.CreateOrgInvite(ctx, e.carol, "MAIN", fmt.Sprintf("p%d@example.com", i), nil, 7)
		if last != nil && i < 50 {
			t.Fatalf("invite %d: %v", i, last)
		}
	}
	if last == nil {
		t.Fatal("the 51st invitation within an hour was accepted")
	}
}

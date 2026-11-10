package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

// ADR-0031: join link and join requests: only that organization's ADMIN decides, no access before approval,
// disabled link answers like a missing one, repeat requests, cooldown and limit, one winner.
func TestJoinRequests(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("PRJ")
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	waiting := func() int {
		return e.count(`SELECT COUNT(*) FROM organization_join_requests WHERE user_id = ? AND status = 'pending'`, e.aliceID)
	}

	// a disabled or unknown link answers nil and files nothing
	for _, key := range []string{"MAIN", "NOPE"} {
		if err := e.svc.RequestJoin(ctx, e.alice, key); err != nil || waiting() != 0 {
			t.Fatalf("request to %s with the link off: %v, %d waiting", key, err, waiting())
		}
	}
	if err := e.svc.SetJoinLink(ctx, e.bob, "MAIN", true); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a stranger switched the link on: %v", err)
	}
	if err := e.svc.SetJoinLink(ctx, e.carol, "MAIN", true); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization switched the link on: %v", err)
	}
	if err := e.svc.SetJoinLink(ctx, e.admin, "MAIN", true); err != nil {
		t.Fatal(err)
	}

	// a request files once; repeats answer the same and add nothing; no access before approval
	for range 3 {
		if err := e.svc.RequestJoin(ctx, e.alice, "main"); err != nil {
			t.Fatal(err)
		}
	}
	if waiting() != 1 {
		t.Fatalf("%d waiting requests, want 1", waiting())
	}
	if orgs, _ := e.svc.ListOrganizations(ctx, e.alice); len(orgs) != 0 {
		t.Fatalf("access before approval: %+v", orgs)
	}
	list, err := e.svc.ListJoinRequests(ctx, e.admin, "MAIN", "pending")
	if err != nil || len(list) != 1 || list[0].UserEmail != "alice@x.io" {
		t.Fatalf("queue: %v %+v", err, list)
	}
	id := list[0].ID
	if _, err := e.svc.ListJoinRequests(ctx, e.carol, "MAIN", ""); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another organization's ADMIN read the queue: %v", err)
	}
	e.orgUser(e.bobID)
	if err := e.svc.DecideJoinRequest(ctx, e.bob, "MAIN", id, "approve", ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER decided: %v", err)
	}
	if err := e.svc.DecideJoinRequest(ctx, e.carol, "MAIN", id, "approve", ""); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another organization's ADMIN decided: %v", err)
	}
	if err := e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", id, "maybe", ""); !isInvalid(err) {
		t.Fatalf("an unknown decision: %v", err)
	}

	// approval: USER membership, no project access; deciding twice is a rule error
	if err := e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", id, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if o, err := e.svc.GetOrganization(ctx, e.alice, "MAIN"); err != nil || o.Role != "user" {
		t.Fatalf("after approval: %v %+v", err, o)
	}
	if n := e.count(`SELECT COUNT(*) FROM project_members WHERE user_id = ?`, e.aliceID); n != 0 {
		t.Fatalf("approval gave project access: %d", n)
	}
	if err := e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", id, "reject", ""); !isRule(err) {
		t.Fatalf("deciding twice: %v", err)
	}
	if err := e.svc.RequestJoin(ctx, e.alice, "MAIN"); err != nil || e.count(`SELECT COUNT(*) FROM organization_join_requests WHERE user_id = ?`, e.aliceID) != 1 {
		t.Fatalf("a member filed another request: %v", err)
	}

	// rejection: wait 24 h, then at most 3 requests per 30 days; the answer never changes
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now })
	if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		return e.count(`SELECT COUNT(*) FROM organization_join_requests WHERE user_id = ?`, e.carolID)
	}
	for i := range 3 {
		if i > 0 {
			now = now.Add(25 * time.Hour)
		}
		if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil {
			t.Fatal(err)
		}
		pending, _ := e.svc.ListJoinRequests(ctx, e.admin, "MAIN", "pending")
		if len(pending) != 1 {
			t.Fatalf("round %d: %d waiting, want 1", i, len(pending))
		}
		if err := e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", pending[0].ID, "reject", "no"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil || count() != i+1 {
			t.Fatalf("round %d: request inside the cooldown: %v, %d filed", i, err, count())
		}
	}
	now = now.Add(25 * time.Hour)
	if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil || count() != 3 {
		t.Fatalf("a fourth request within 30 days: %v, %d filed", err, count())
	}

	// a switched-off link answers like a missing one
	e.svc.SetClock(func() time.Time { return now.Add(40 * 24 * time.Hour) })
	if err := e.svc.SetJoinLink(ctx, e.admin, "MAIN", false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil || count() != 3 {
		t.Fatalf("request with the link off: %v, %d filed", err, count())
	}
}

func TestJoinRequestDecisionsAreCompareAndSet(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.svc.SetJoinLink(ctx, e.admin, "MAIN", true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestJoin(ctx, e.alice, "MAIN"); err != nil {
		t.Fatal(err)
	}
	list, _ := e.svc.ListJoinRequests(ctx, e.admin, "MAIN", "pending")
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, d := range []string{"approve", "reject"} {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", list[0].ID, d, "") }()
	}
	wg.Wait()
	if a, b := <-errs, <-errs; (a == nil) == (b == nil) {
		t.Fatalf("two concurrent decisions: %v and %v, want exactly one success", a, b)
	}
}

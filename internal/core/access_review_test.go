package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kees/atlas/internal/core"
)

// The effective access and project access views agree with what authz allows (ATL-462, ATL-486).
func TestAccessViews(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	if err := e.member("EXO", e.carolID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := e.member("EXO", e.bobID, "member"); err != nil {
		t.Fatal(err)
	}
	e.org("OTH", e.aliceID)

	own, err := e.svc.EffectiveAccess(ctx, e.bob, "", 0)
	if err != nil || len(own) != 1 || own[0].Key != "MAIN" || len(own[0].Projects) != 1 || own[0].Projects[0].Source != core.SourceExplicit {
		t.Fatalf("member's own access: %v %+v", err, own)
	}
	adm, err := e.svc.EffectiveAccess(ctx, e.carol, "", 0)
	if err != nil || len(adm) != 1 || len(adm[0].Projects) != 1 || adm[0].Projects[0].Source != core.SourceInheritedAdmin {
		t.Fatalf("admin's own access: %v %+v", err, adm)
	}
	if got, err := e.svc.EffectiveAccess(ctx, e.carol, "MAIN", e.bobID); err != nil || len(got) != 1 || got[0].Projects[0].Key != "EXO" {
		t.Fatalf("admin view of a member: %v %+v", err, got)
	}
	if _, err := e.svc.EffectiveAccess(ctx, e.bob, "MAIN", e.carolID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a plain member read another's access: %v", err)
	}
	if _, err := e.svc.EffectiveAccess(ctx, e.alice, "MAIN", e.bobID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization: %v, want not found", err)
	}
	if _, err := e.svc.EffectiveAccess(ctx, e.carol, "MAIN", e.aliceID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a non-member of the organization: %v, want not found", err)
	}

	list, err := e.svc.ProjectAccess(ctx, e.carol, "EXO")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[int64]string{}
	for _, x := range list {
		sources[x.UserID] = x.Source
	}
	if sources[e.bobID] != core.SourceExplicit || sources[e.carolID] != core.SourceInheritedAdmin {
		t.Fatalf("project access: %+v", list)
	}
	if _, err := e.svc.ProjectAccess(ctx, e.bob, "EXO"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a member read the access review: %v", err)
	}
	if _, err := e.svc.ProjectAccess(ctx, e.alice, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another organization read the access review: %v", err)
	}
}

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kees/atlas/internal/core"
)

// TestOtherOrganizationSeesNothing probes a project of organization MAIN with the key of one of its items as the
// ADMIN of another organization: every read answers not found, the same as for a key that does not exist (ATL-463, ATL-464).
func TestOtherOrganizationSeesNothing(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.project("EXO")
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "secret planning"})
	e.org("OTH", e.bobID)
	if _, err := e.svc.CreateProjectIn(ctx, e.bob, "OTH", "OTP", "other"); err != nil {
		t.Fatal(err)
	}
	mine := e.item(e.bob, core.CreateItem{Project: "OTP", Type: "task", Title: "mine"})

	if ps, _ := e.svc.ListProjects(ctx, e.bob); len(ps) != 1 || ps[0].Key != "OTP" {
		t.Fatalf("project list leaks: %+v", ps)
	}
	if page, err := e.svc.ListItems(ctx, e.bob, core.ItemFilter{Q: "secret"}); err != nil || len(page.Items) != 0 {
		t.Fatalf("search leaks: %v %+v", err, page.Items)
	}
	probes := map[string]func(project, item string) error{
		"project": func(p, _ string) error { _, err := e.svc.GetProject(ctx, e.bob, p); return err },
		"item":    func(_, i string) error { _, err := e.svc.GetItem(ctx, e.bob, i); return err },
		"list": func(p, _ string) error {
			_, err := e.svc.ListItems(ctx, e.bob, core.ItemFilter{Project: p})
			return err
		},
		"board":    func(p, _ string) error { _, err := e.svc.Board(ctx, e.bob, p, core.BoardFilter{}); return err },
		"progress": func(p, _ string) error { _, err := e.svc.ProjectProgress(ctx, e.bob, p); return err },
		"export":   func(p, _ string) error { _, err := e.svc.ExportProject(ctx, e.bob, p); return err },
		"members":  func(p, _ string) error { _, err := e.svc.ListMembers(ctx, e.bob, p); return err },
		"comments": func(_, i string) error { _, err := e.svc.ListComments(ctx, e.bob, i); return err },
		"link":     func(_, i string) error { return e.svc.AddLink(ctx, e.bob, mine.Key, i, "relates") },
		"org":      func(p, _ string) error { _, err := e.svc.GetOrganization(ctx, e.bob, "MAIN"); return err },
	}
	for name, probe := range probes {
		real, ghost := probe("EXO", it.Key), probe("NOPE", "NOPE-1")
		if !errors.Is(real, core.ErrNotFound) {
			t.Errorf("%s of another organization: %v, want not found", name, real)
		}
		if name != "org" && (real == nil || ghost == nil || real.Error() != ghost.Error()) {
			t.Errorf("%s: hidden answers %v, missing answers %v", name, real, ghost)
		}
	}
}

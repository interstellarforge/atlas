package core_test

import (
	"context"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func TestSprintAssignFilterAndClose(t *testing.T) {
	e := setup(t)
	e.project("SP")
	ctx := context.Background()

	if _, err := e.svc.CreateSprint(ctx, e.bob, "SP", "S1", 1000, 2000); err == nil {
		t.Fatal("non-member created a sprint")
	}
	if _, err := e.svc.CreateSprint(ctx, e.admin, "SP", "S1", 2000, 1000); err == nil {
		t.Fatal("sprint ending before it starts accepted")
	}
	sp, err := e.svc.CreateSprint(ctx, e.admin, "SP", "S1", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	in, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "SP", Type: "task", Title: "in sprint", Sprint: "S1"})
	if err != nil || in.Sprint == nil || *in.Sprint != "S1" {
		t.Fatalf("create in sprint: %v %+v", err, in.Sprint)
	}
	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "SP", Type: "task", Title: "backlog"}); err != nil {
		t.Fatal(err)
	}
	keys := func(sprint string) int {
		page, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Project: "SP", Sprint: sprint})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	if keys("S1") != 1 || keys("none") != 1 {
		t.Fatalf("filters: S1=%d none=%d, want 1 and 1", keys("S1"), keys("none"))
	}
	if err := e.svc.DeleteSprint(ctx, e.admin, "SP", sp.ID); err == nil {
		t.Fatal("deleted a sprint that still has items")
	}
	if _, err := e.svc.UpdateSprint(ctx, e.admin, "SP", sp.ID, map[string]any{"closed": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "SP", Type: "task", Title: "late", Sprint: "S1"}); err == nil {
		t.Fatal("closed sprint took a new item")
	}
	if _, err := e.svc.UpdateItem(ctx, e.admin, in.Key, in.Version, map[string]any{"sprint": nil}); err != nil {
		t.Fatalf("remove from sprint: %v", err)
	}
	if keys("none") != 2 {
		t.Fatalf("backlog has %d items, want 2", keys("none"))
	}
}

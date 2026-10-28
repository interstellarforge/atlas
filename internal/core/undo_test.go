package core_test

import (
	"context"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func TestUndoBatchRestoresAndKeepsLaterEdits(t *testing.T) {
	e := setup(t)
	e.project("UN")
	ctx := context.Background()
	base, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "UN", Type: "task", Title: "old title", Priority: "low"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.svc.Batch(ctx, e.admin, []core.BatchOp{
		{Op: "update", Key: base.Key, Version: base.Version, Patch: map[string]any{"title": "new title", "priority": "high"}},
		{Op: "create", Create: core.CreateItem{Project: "UN", Type: "task", Title: "agent made"}},
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	created := out.Results[1].Item["key"].(string)

	// A person edits the title after the batch; the priority is still the batch's value.
	cur, _ := e.svc.GetItem(ctx, e.admin, base.Key)
	if _, err := e.svc.UpdateItem(ctx, e.admin, base.Key, cur.Version, map[string]any{"title": "human title"}); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.UndoBatch(ctx, e.bob, out.BatchID, false); err == nil {
		t.Fatal("non-member undid a batch")
	}
	res, err := e.svc.UndoBatch(ctx, e.admin, out.BatchID, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.GetItem(ctx, e.admin, base.Key)
	if got.Title != "human title" || got.Priority != "low" {
		t.Fatalf("title %q priority %q, want the later edit kept and priority restored", got.Title, got.Priority)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Item != base.Key || res.Conflicts[0].Fields[0] != "title" {
		t.Fatalf("conflicts %+v", res.Conflicts)
	}
	if c, _ := e.svc.GetItem(ctx, e.admin, created); c.ArchivedAt == nil {
		t.Fatal("created item was not archived")
	}
	if _, err := e.svc.UndoBatch(ctx, e.admin, out.BatchID, true); err == nil {
		t.Fatal("batch undone twice")
	}
}

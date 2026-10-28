package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func statusID(t *testing.T, e *env, project, name string) int64 {
	t.Helper()
	w, err := e.svc.GetWorkflow(ctx, e.admin, project)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range w.Statuses {
		if st.Name == name {
			return st.ID
		}
	}
	t.Fatalf("no status %q", name)
	return 0
}

func require(t *testing.T, e *env, project, status string, requires ...string) {
	t.Helper()
	if _, err := e.svc.UpdateStatus(ctx, e.admin, project, statusID(t, e, project, status), map[string]any{"requires": requires}); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredSteps(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	require(t, e, "EXO", "Done", "Review")
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "t", Status: "Todo"})

	// Skipping Review is refused by update and by move, and the message names the missing step.
	_, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "Done"})
	if !isRule(err) || !strings.Contains(err.Error(), "Review") {
		t.Fatalf("update skipping Review: %v", err)
	}
	if _, err := e.svc.MoveItem(ctx, e.admin, it.Key, it.Version, core.MoveSpec{Status: "Done"}); !isRule(err) {
		t.Fatalf("move skipping Review: %v", err)
	}
	// Creating straight into Done skips Review as well.
	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "x", Status: "Done"}); !isRule(err) {
		t.Fatalf("create in Done: %v", err)
	}

	// Passing through Review unlocks Done; going back and forth afterwards stays possible.
	it, err = e.svc.MoveItem(ctx, e.admin, it.Key, it.Version, core.MoveSpec{Status: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if it, err = e.svc.MoveItem(ctx, e.admin, it.Key, it.Version, core.MoveSpec{Status: "Done"}); err != nil {
		t.Fatalf("Review then Done: %v", err)
	}
	if it, err = e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "Todo"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "Done"}); err != nil {
		t.Fatalf("back to Todo and Done again: %v", err)
	}

	// History is keyed by status id: renaming Review keeps it.
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Review"), map[string]any{"name": "QA"}); err != nil {
		t.Fatal(err)
	}
	other := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "o", Status: "Todo"})
	other, _ = e.svc.MoveItem(ctx, e.admin, other.Key, other.Version, core.MoveSpec{Status: "QA"})
	if _, err := e.svc.MoveItem(ctx, e.admin, other.Key, other.Version, core.MoveSpec{Status: "Done"}); err != nil {
		t.Fatalf("after rename: %v", err)
	}

	// An item that is in the required status right now may enter (Review -> Done needs no earlier visit).
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Done"), map[string]any{"requires": []string{"Backlog"}}); err != nil {
		t.Fatal(err)
	}
	b := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "b", Status: "Backlog"})
	if _, err := e.svc.MoveItem(ctx, e.admin, b.Key, b.Version, core.MoveSpec{Status: "Done"}); err != nil {
		t.Fatalf("moving out of the required status: %v", err)
	}

	// Rules: a status cannot require itself, unknown names are invalid, and a transition rule still applies.
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Done"), map[string]any{"requires": []string{"Done"}}); !isInvalid(err) {
		t.Fatalf("self requirement: %v", err)
	}
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Done"), map[string]any{"requires": []string{"Nope"}}); !isInvalid(err) {
		t.Fatalf("unknown requirement: %v", err)
	}
	require(t, e, "EXO", "Done", "QA")
	if err := e.svc.SetTransitions(ctx, e.admin, "EXO", []core.Transition{{From: "Backlog", To: "Todo"}}); err != nil {
		t.Fatal(err)
	}
	c := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "c", Status: "Backlog"})
	if _, err := e.svc.MoveItem(ctx, e.admin, c.Key, c.Version, core.MoveSpec{Status: "Todo"}); err != nil {
		t.Fatalf("allowed transition without requirement: %v", err)
	}
}

func TestRequiredStepsImportAndPermissions(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.aliceID, "member"); err != nil {
		t.Fatal(err)
	}
	require(t, e, "EXO", "Done", "Review")

	rep, err := e.svc.ImportItems(ctx, e.admin, "EXO", []core.ImportRow{{Type: "task", Title: "imported", Status: "Done"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Applied || len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0].Message, "Review") {
		t.Fatalf("import into a status with a missing step: %+v", rep)
	}

	// Only the workflow action may change the rule; members cannot.
	_, err = e.svc.UpdateStatus(ctx, e.alice, "EXO", statusID(t, e, "EXO", "Done"), map[string]any{"requires": []string{}})
	if !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("member changing requirements: %v", err)
	}
	// ... and a failed rule leaves no trace: the item list is unchanged.
	if n := e.count(`SELECT COUNT(*) FROM work_items`); n != 0 {
		t.Fatalf("%d items after rejected import", n)
	}
}

func TestMergedColumns(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	if _, err := e.svc.AddStatus(ctx, e.admin, "EXO", "Blocked", "active", nil); err != nil {
		t.Fatal(err)
	}
	// The merge test wants Review to share the active column; by default it has its own category.
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Review"), map[string]any{"category": "active"}); err != nil {
		t.Fatal(err)
	}
	// Default: one column per status.
	b, err := e.svc.Board(ctx, e.admin, "EXO", core.BoardFilter{})
	if err != nil || len(b.Columns) != 6 {
		t.Fatalf("unmerged: %d columns, %v", len(b.Columns), err)
	}
	if err := e.svc.SetMergeColumns(ctx, e.alice, "EXO", true); !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("non-member merging: %v", err)
	}
	if err := e.svc.SetMergeColumns(ctx, e.admin, "EXO", true); err != nil {
		t.Fatal(err)
	}

	mk := func(title, status string) core.Item {
		return e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: title, Status: status})
	}
	a, r, bl := mk("a", "In Progress"), mk("r", "Review"), mk("bl", "Blocked")
	todo := mk("t", "Todo")

	b, _ = e.svc.Board(ctx, e.admin, "EXO", core.BoardFilter{})
	if len(b.Columns) != 3 { // todo (Backlog, Todo), active (3), completed
		t.Fatalf("merged: %d columns", len(b.Columns))
	}
	active := column(t, e, "EXO", "In Progress", core.BoardFilter{})
	if len(active.Statuses) != 3 || active.Count != 3 || !slicesEqual(keys(active), []string{a.Key, r.Key, bl.Key}) {
		t.Fatalf("active column: %d statuses, count %d, %v", len(active.Statuses), active.Count, keys(active))
	}
	if active.Items[1]["status"] != "Review" {
		t.Fatalf("cards carry their status: %v", active.Items[1])
	}

	// Move within the merged column between neighbours of other statuses; the status stays.
	moved, err := e.svc.MoveItem(ctx, e.admin, bl.Key, bl.Version, core.MoveSpec{After: a.Key, Before: r.Key})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Status != "Blocked" {
		t.Fatalf("status changed to %s", moved.Status)
	}
	if got := keys(column(t, e, "EXO", "In Progress", core.BoardFilter{})); !slicesEqual(got, []string{a.Key, bl.Key, r.Key}) {
		t.Fatalf("order after move %v", got)
	}

	// Moving in from another column picks the status the client sends and appends at the end.
	if _, err := e.svc.MoveItem(ctx, e.admin, todo.Key, todo.Version, core.MoveSpec{Status: "Review"}); err != nil {
		t.Fatal(err)
	}
	if got := keys(column(t, e, "EXO", "In Progress", core.BoardFilter{})); !slicesEqual(got, []string{a.Key, bl.Key, r.Key, todo.Key}) {
		t.Fatalf("order after cross move %v", got)
	}

	// WIP stays per status: a limit on Review is exceeded by two items there and flags the whole column.
	wip := 1
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", statusID(t, e, "EXO", "Review"), map[string]any{"wip_limit": wip}); err != nil {
		t.Fatal(err)
	}
	if !column(t, e, "EXO", "In Progress", core.BoardFilter{}).OverWIP {
		t.Fatal("two items in Review with limit 1 should flag the merged column")
	}

	// Required steps still apply between statuses of one column.
	require(t, e, "EXO", "Review", "In Progress")
	x := mk("x", "Blocked")
	if _, err := e.svc.MoveItem(ctx, e.admin, x.Key, x.Version, core.MoveSpec{Status: "Review"}); !isRule(err) {
		t.Fatalf("required step inside a merged column: %v", err)
	}

	// Off again: columns are per status and the order within each is unchanged.
	if err := e.svc.SetMergeColumns(ctx, e.admin, "EXO", false); err != nil {
		t.Fatal(err)
	}
	if b, _ = e.svc.Board(ctx, e.admin, "EXO", core.BoardFilter{}); len(b.Columns) != 6 {
		t.Fatalf("unmerged again: %d columns", len(b.Columns))
	}
}

func TestMergedColumnsInTemplates(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	require(t, e, "EXO", "Done", "Review")
	if err := e.svc.SetMergeColumns(ctx, e.admin, "EXO", true); err != nil {
		t.Fatal(err)
	}
	tpl, err := e.svc.SaveProjectTemplateFromProject(ctx, e.admin, "Merged", "", "EXO")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateProjectFromTemplate(ctx, e.admin, "NEW", "New", tpl.ID); err != nil {
		t.Fatal(err)
	}
	w, err := e.svc.GetWorkflow(ctx, e.admin, "NEW")
	if err != nil || !w.MergeColumns {
		t.Fatalf("merge_columns not applied: %v %v", w.MergeColumns, err)
	}
	for _, st := range w.Statuses {
		if st.Name == "Done" && !slicesEqual(st.Requires, []string{"Review"}) {
			t.Fatalf("requirement not applied: %v", st.Requires)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReviewIsItsOwnMergedColumn(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	if err := e.svc.SetMergeColumns(ctx, e.admin, "EXO", true); err != nil {
		t.Fatal(err)
	}
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "r", Status: "Review"})
	b, err := e.svc.Board(ctx, e.admin, "EXO", core.BoardFilter{})
	if err != nil || len(b.Columns) != 4 { // todo, active, review, completed
		t.Fatalf("merged board: %d columns, %v", len(b.Columns), err)
	}
	p, err := e.svc.ProjectProgress(ctx, e.admin, "EXO")
	if err != nil || p.Categories["review"] != 1 {
		t.Fatalf("progress by category: %v %v", p.Categories, err)
	}
}

func TestMergeOnlyChosenCategories(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	w, err := e.svc.GetWorkflow(ctx, e.admin, "EXO")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetMergeCategories(ctx, e.admin, "EXO", []string{"todo"}); err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Board(ctx, e.admin, "EXO", core.BoardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	todos := 0
	for _, st := range w.Statuses {
		if st.Category == "todo" {
			todos++
		}
	}
	if todos < 2 || len(b.Columns) != len(w.Statuses)-todos+1 {
		t.Fatalf("only todo should merge: %d columns for %d statuses (%d todo)", len(b.Columns), len(w.Statuses), todos)
	}
	if err := e.svc.SetMergeCategories(ctx, e.admin, "EXO", []string{"nope"}); err == nil {
		t.Fatal("unknown category accepted")
	}
}

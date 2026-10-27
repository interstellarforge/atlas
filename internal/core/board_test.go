package core_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func column(t *testing.T, e *env, project, status string, f core.BoardFilter) core.Column {
	t.Helper()
	b, err := e.svc.Board(ctx, e.admin, project, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.Columns {
		if c.Status.Name == status {
			return c
		}
	}
	t.Fatalf("no column %q", status)
	return core.Column{}
}

func keys(c core.Column) []string {
	out := make([]string, 0, len(c.Items))
	for _, it := range c.Items {
		out = append(out, it["key"].(string))
	}
	return out
}

func todo(e *env, project, title string) core.Item {
	return e.item(e.admin, core.CreateItem{Project: project, Type: "task", Title: title, Status: "Todo"})
}

func TestMoveWritesOneRankAndReorders(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	a, b, c := todo(e, "EXO", "A"), todo(e, "EXO", "B"), todo(e, "EXO", "C")
	if got := keys(column(t, e, "EXO", "Todo", core.BoardFilter{})); !slices.Equal(got, []string{a.Key, b.Key, c.Key}) {
		t.Fatalf("initial order %v", got)
	}

	moved, err := e.svc.MoveItem(ctx, e.admin, c.Key, c.Version, core.MoveSpec{After: a.Key, Before: b.Key})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(column(t, e, "EXO", "Todo", core.BoardFilter{})); !slices.Equal(got, []string{a.Key, c.Key, b.Key}) {
		t.Fatalf("after move %v", got)
	}
	if moved.Version != c.Version+1 {
		t.Fatalf("version %d", moved.Version)
	}
	// The neighbours were not touched: a move is one write.
	for _, k := range []string{a.Key, b.Key} {
		if it, _ := e.svc.GetItem(ctx, e.admin, k); it.Version != 1 {
			t.Fatalf("%s was rewritten (version %d)", k, it.Version)
		}
	}
	// Moving to the top and to another column.
	if _, err := e.svc.MoveItem(ctx, e.admin, b.Key, b.Version, core.MoveSpec{Before: a.Key}); err != nil {
		t.Fatal(err)
	}
	if got := keys(column(t, e, "EXO", "Todo", core.BoardFilter{})); !slices.Equal(got, []string{b.Key, a.Key, c.Key}) {
		t.Fatalf("after move to top %v", got)
	}
	if _, err := e.svc.MoveItem(ctx, e.admin, a.Key, a.Version, core.MoveSpec{Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if got := keys(column(t, e, "EXO", "Done", core.BoardFilter{})); !slices.Equal(got, []string{a.Key}) {
		t.Fatalf("done column %v", got)
	}
}

func TestConcurrentMoves(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	anchor := todo(e, "EXO", "anchor")
	items := make([]core.Item, 10)
	for i := range items {
		items[i] = e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "x", Status: "Backlog"})
	}

	// Different items dropped at the same spot all succeed and keep distinct, ordered ranks.
	var wg sync.WaitGroup
	errs := make([]error, len(items))
	for i := range items {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.MoveItem(ctx, e.admin, items[i].Key, items[i].Version, core.MoveSpec{Status: "Todo", After: anchor.Key})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent move failed: %v", err)
		}
	}
	got := keys(column(t, e, "EXO", "Todo", core.BoardFilter{}))
	if len(got) != 11 || got[0] != anchor.Key {
		t.Fatalf("column after concurrent moves: %v", got)
	}
	if n := e.count(`SELECT COUNT(DISTINCT rank) FROM work_items WHERE status_id = (SELECT id FROM project_statuses WHERE name = 'Todo')`); n != 11 {
		t.Fatalf("%d distinct ranks for 11 items", n)
	}

	// The same item moved twice from the same version: one wins.
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "contested", Status: "Backlog"})
	results := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = e.svc.MoveItem(ctx, e.admin, it.Key, it.Version, core.MoveSpec{Status: "Review"})
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !isConflict(err):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d moves won, want 1", wins)
	}
}

func TestMoveWithEqualRanksRebalances(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	a, b, c := todo(e, "EXO", "A"), todo(e, "EXO", "B"), todo(e, "EXO", "C")
	// Simulate a corrupted or legacy column where everything shares one rank.
	if _, err := e.db.W.Exec(`UPDATE work_items SET rank = 'M'`); err != nil {
		t.Fatal(err)
	}
	moved, err := e.svc.MoveItem(ctx, e.admin, c.Key, c.Version, core.MoveSpec{After: a.Key})
	if err != nil {
		t.Fatalf("move with tied ranks: %v", err)
	}
	if got := keys(column(t, e, "EXO", "Todo", core.BoardFilter{})); !slices.Equal(got, []string{a.Key, moved.Key, b.Key}) {
		t.Fatalf("order after rebalance %v", got)
	}
	if n := e.count(`SELECT COUNT(DISTINCT rank) FROM work_items`); n != 3 {
		t.Fatalf("ranks still tied: %d distinct", n)
	}
	if e.count(`SELECT COUNT(*) FROM change_events WHERE type = 'board.rebalanced'`) != 1 {
		t.Fatal("rebalance was not announced")
	}
}

func TestMoveRejectsForeignNeighboursAndBadTransitions(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.project("OTH")
	mine := todo(e, "EXO", "mine")
	other := todo(e, "OTH", "other project")
	review := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "elsewhere", Status: "Review"})

	if _, err := e.svc.MoveItem(ctx, e.admin, mine.Key, mine.Version, core.MoveSpec{After: other.Key}); !isRule(err) {
		t.Fatalf("neighbour from another project: %v", err)
	}
	if _, err := e.svc.MoveItem(ctx, e.admin, mine.Key, mine.Version, core.MoveSpec{After: review.Key}); !isRule(err) {
		t.Fatalf("neighbour from another column: %v", err)
	}
	if err := e.svc.SetTransitions(ctx, e.admin, "EXO", []core.Transition{{From: "Todo", To: "Review"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.MoveItem(ctx, e.admin, mine.Key, mine.Version, core.MoveSpec{Status: "Done"}); !isRule(err) {
		t.Fatalf("forbidden transition: %v", err)
	}
	if _, err := e.svc.MoveItem(ctx, e.bob, mine.Key, mine.Version, core.MoveSpec{Status: "Review"}); err == nil {
		t.Fatal("a user without access moved an item")
	}
}

func TestBoardCountsWIPPagingAndArchive(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	all := []string{}
	for i := 0; i < 5; i++ {
		all = append(all, todo(e, "EXO", "t").Key)
	}
	wf, _ := e.svc.GetWorkflow(ctx, e.admin, "EXO")
	var todoID int64
	for _, st := range wf.Statuses {
		if st.Name == "Todo" {
			todoID = st.ID
		}
	}
	if _, err := e.svc.UpdateStatus(ctx, e.admin, "EXO", todoID, map[string]any{"wip_limit": 2.0}); err != nil {
		t.Fatal(err)
	}

	first := column(t, e, "EXO", "Todo", core.BoardFilter{Limit: 2})
	if first.Count != 5 || !first.OverWIP || len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page: count=%d over=%v items=%d cursor=%v", first.Count, first.OverWIP, len(first.Items), first.NextCursor)
	}
	got := keys(first)
	cursor := *first.NextCursor
	for cursor != "" {
		next, err := e.svc.BoardColumn(ctx, e.admin, "EXO", todoID, core.BoardFilter{Limit: 2}, cursor)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, keys(next)...)
		cursor = ""
		if next.NextCursor != nil {
			cursor = *next.NextCursor
		}
	}
	if !slices.Equal(got, all) {
		t.Fatalf("paged cards %v, want %v", got, all)
	}

	it, _ := e.svc.GetItem(ctx, e.admin, all[0])
	if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"archived": true}); err != nil {
		t.Fatal(err)
	}
	if c := column(t, e, "EXO", "Todo", core.BoardFilter{}); c.Count != 4 || slices.Contains(keys(c), all[0]) {
		t.Fatalf("archived card still on the board: %v", keys(c))
	}
	if c := column(t, e, "EXO", "Todo", core.BoardFilter{IncludeArchived: true}); c.Count != 5 {
		t.Fatalf("include archived: %d", c.Count)
	}
}

func TestCrossBoardGroupsByCategoryAndRespectsAccess(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.project("OTH")
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "a", Status: "Todo"})
	e.item(e.admin, core.CreateItem{Project: "OTH", Type: "task", Title: "b", Status: "Backlog"})
	e.item(e.admin, core.CreateItem{Project: "OTH", Type: "task", Title: "c", Status: "Done"})

	b, err := e.svc.CrossBoard(ctx, e.admin, nil, core.BoardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, c := range b.Columns {
		counts[c.Status.Category] = c.Count
		if c.Status.WIPLimit != nil {
			t.Fatal("WIP limits of different projects must not be merged")
		}
	}
	if counts["todo"] != 2 || counts["active"] != 0 || counts["completed"] != 1 {
		t.Fatalf("counts %v", counts)
	}
	if p, _ := b.Columns[0].Items[0]["project"].(string); p == "" {
		t.Fatalf("cards need their project: %v", b.Columns[0].Items[0])
	}
	// Carol is a member of nothing: she sees nothing.
	cb, err := e.svc.CrossBoard(ctx, e.carol, nil, core.BoardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cb.Columns {
		if c.Count != 0 || len(c.Items) != 0 {
			t.Fatalf("carol sees %d items", c.Count)
		}
	}
}

func TestListSortAndOffsetPaging(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	prios := []string{"low", "urgent", "normal", "high"}
	for _, p := range prios {
		e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: p, Priority: p})
	}
	var order []string
	cursor := ""
	for {
		page, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Project: "EXO", Sort: "priority", Limit: 3, Cursor: cursor, Fields: []string{"title"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			order = append(order, it["title"].(string))
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !slices.Equal(order, []string{"urgent", "high", "normal", "low"}) {
		t.Fatalf("priority order %v", order)
	}
	if _, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Project: "EXO", Sort: "nonsense"}); !isInvalid(err) {
		t.Fatalf("bad sort accepted: %v", err)
	}
}

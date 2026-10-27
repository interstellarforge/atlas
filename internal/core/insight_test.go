package core_test

import (
	"errors"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func TestProgressCountsActiveItemsOnly(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "a"})
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "b", Priority: "high", Status: "Done"})
	gone := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "bug", Title: "c", Status: "Done"})
	if _, err := e.svc.UpdateItem(ctx, e.admin, gone.Key, gone.Version, map[string]any{"archived": true}); err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.ProjectProgress(ctx, e.admin, "EXO")
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 2 || p.Categories["completed"] != 1 || p.Categories["todo"] != 1 || p.Types["bug"] != 0 || p.Priorities["high"] != 1 {
		t.Fatalf("unexpected progress %+v", p)
	}
	if len(p.Statuses) != 5 {
		t.Fatalf("want every status listed, got %d", len(p.Statuses))
	}
	if _, err := e.svc.ProjectProgress(ctx, e.carol, "EXO"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
}

func TestParseDurationRounds(t *testing.T) {
	for in, want := range map[string]int{"90": 90, "45m": 45, "1h": 60, "1.5h": 90, "1h30m": 90, "0,5h": 30, "1h 15": 75, "0.017h": 1} {
		got, err := core.ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "abc", "25h", "-5"} {
		if _, err := core.ParseDuration(in); !isInvalid(err) {
			t.Errorf("ParseDuration(%q): want invalid, got %v", in, err)
		}
	}
}

func TestTimeTrackingRightsAndSummary(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	for id, role := range map[int64]string{e.aliceID: "member", e.bobID: "viewer", e.carolID: "member"} {
		if err := e.svc.SetMember(ctx, e.admin, "EXO", id, role); err != nil {
			t.Fatal(err)
		}
	}
	it := e.item(e.alice, core.CreateItem{Project: "EXO", Type: "task", Title: "work"})

	if _, err := e.svc.LogTime(ctx, e.alice, it.Key, "1h", "", ""); !isRule(err) {
		t.Fatalf("tracking off: want rule error, got %v", err)
	}
	if err := e.svc.SetTimeTracking(ctx, e.alice, "EXO", true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("member toggling the setting: %v", err)
	}
	if err := e.svc.SetTimeTracking(ctx, e.admin, "EXO", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LogTime(ctx, e.bob, it.Key, "1h", "", ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("viewer booking: %v", err)
	}
	mine, err := e.svc.LogTime(ctx, e.alice, it.Key, "1h30m", "2026-10-26", "design")
	if err != nil || mine.Minutes != 90 {
		t.Fatalf("log: %+v %v", mine, err)
	}
	if _, err := e.svc.LogTime(ctx, e.carol, it.Key, "45", "2026-10-27", ""); err != nil {
		t.Fatal(err)
	}

	// Own bookings only, except for a project admin.
	if _, err := e.svc.UpdateTime(ctx, e.carol, "EXO", mine.ID, map[string]any{"duration": "2h"}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("editing someone else's booking: %v", err)
	}
	if err := e.svc.DeleteTime(ctx, e.carol, "EXO", mine.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("deleting someone else's booking: %v", err)
	}
	if u, err := e.svc.UpdateTime(ctx, e.alice, "EXO", mine.ID, map[string]any{"duration": "2h"}); err != nil || u.Minutes != 120 {
		t.Fatalf("own edit: %+v %v", u, err)
	}

	sum, err := e.svc.TimeSummary(ctx, e.bob, "EXO", core.TimeFilter{Period: "month"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.TotalMinutes != 165 || sum.Entries != 2 || len(sum.People) != 2 || sum.Items[0].Key != it.Key || sum.Periods[0].Key != "2026-10" {
		t.Fatalf("summary %+v", sum)
	}
	day := e.aliceID
	sum, err = e.svc.TimeSummary(ctx, e.bob, "EXO", core.TimeFilter{UserID: &day, From: "2026-10-27"})
	if err != nil || sum.TotalMinutes != 0 {
		t.Fatalf("filtered summary %+v %v", sum, err)
	}

	if err := e.svc.DeleteTime(ctx, e.admin, "EXO", mine.ID); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_entries WHERE operation LIKE 'time.%'`); n != 4 {
		t.Fatalf("%d audit entries, want 4 (2 log, update, delete)", n)
	}
}

func TestTimelineRangeAndActivity(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	due := int64(1_800_000_000_000)
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "dated", DueAt: &due})
	e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "undated"})

	tl, err := e.svc.ProjectTimeline(ctx, e.admin, "EXO", due-1000, due+1000, 0)
	if err != nil || len(tl.Items) != 1 || tl.Items[0].Title != "dated" {
		t.Fatalf("timeline %+v %v", tl, err)
	}
	if tl, _ = e.svc.ProjectTimeline(ctx, e.admin, "EXO", due+1000, due+2000, 0); len(tl.Items) != 0 {
		t.Fatalf("item outside range returned: %+v", tl)
	}
	if _, err := e.svc.ProjectTimeline(ctx, e.admin, "EXO", due, due-1, 0); !isInvalid(err) {
		t.Fatalf("inverted range: %v", err)
	}
	if _, err := e.svc.ProjectTimeline(ctx, e.carol, "EXO", due-1000, due+1000, 0); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}

	e.project("EXOB") // a key sharing a prefix must not leak into EXO's journal
	e.item(e.admin, core.CreateItem{Project: "EXOB", Type: "task", Title: "other"})
	log, err := e.svc.ProjectActivity(ctx, e.admin, "EXO", 0, 0)
	if err != nil || len(log) != 2 {
		t.Fatalf("activity %+v %v", log, err)
	}
	older, err := e.svc.ProjectActivity(ctx, e.admin, "EXO", log[0].ID, 0)
	if err != nil || len(older) != 1 {
		t.Fatalf("activity page 2 %+v %v", older, err)
	}
}

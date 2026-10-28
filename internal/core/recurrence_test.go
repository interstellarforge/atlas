package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

func TestRecurrenceCreatesOneItemPerOccurrence(t *testing.T) {
	e := setup(t)
	e.project("REC")
	ctx := context.Background()
	now := time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC) // Monday
	e.svc.SetClock(func() time.Time { return now })

	wd := 1
	r, err := e.svc.CreateRecurrence(ctx, e.admin, "REC", core.Recurrence{Title: "Weekly review", Freq: "weekly", Weekday: &wd, AtMinute: 9 * 60})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC).UnixMilli(); r.NextRunAt != want {
		t.Fatalf("first run %d, want %d", r.NextRunAt, want)
	}
	count := func() int {
		page, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Project: "REC", Q: "Weekly review"})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}

	if n := e.svc.RunDueRecurrences(ctx); n != 0 {
		t.Fatalf("ran %d before it was due", n)
	}
	now = now.Add(2 * time.Hour)
	if n := e.svc.RunDueRecurrences(ctx); n != 1 {
		t.Fatalf("ran %d, want 1", n)
	}
	if n := e.svc.RunDueRecurrences(ctx); n != 0 || count() != 1 {
		t.Fatalf("second run created a duplicate (ran %d, items %d)", n, count())
	}

	now = now.AddDate(0, 0, 30) // downtime: missed occurrences are skipped, not replayed
	if n := e.svc.RunDueRecurrences(ctx); n != 1 || count() != 2 {
		t.Fatalf("after downtime ran %d, items %d, want 1 and 2", n, count())
	}
}

func TestRecurrenceNeedsProjectAdmin(t *testing.T) {
	e := setup(t)
	e.project("REC")
	if _, err := e.svc.CreateRecurrence(context.Background(), e.bob, "REC", core.Recurrence{Title: "x", Freq: "daily"}); err == nil {
		t.Fatal("non-member created a recurrence")
	}
}

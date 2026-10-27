package benchmarks_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kees/atlas/benchmarks/seed"
	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run Insight -v
// Progress, timeline, activity and time summary at core level on the 10.000-item seed (ADR-0014).
func TestInsight(t *testing.T) {
	if testing.Short() || !benchEnabled() {
		t.Skip("set ATLAS_BENCH=1 to run")
	}
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(d.W, seed.Options{Items: 10000, Projects: 1, Seed: 1}); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(d)
	u, err := a.CreateUser("bench@x.io", "Bench", "password-123", true)
	if err != nil {
		t.Fatal(err)
	}
	svc := core.New(d, a)
	actor := auth.Actor{Kind: auth.KindHuman, UserID: u.ID, IsAdmin: true}
	ctx := context.Background()
	var project string
	var pid int64
	if err := d.R.QueryRow(`SELECT key, id FROM projects ORDER BY id LIMIT 1`).Scan(&project, &pid); err != nil {
		t.Fatal(err)
	}
	// 20.000 bookings and 10.000 audit entries, spread over a year.
	if _, err := d.W.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 20000)
		INSERT INTO time_entries (project_id, item_id, user_id, status_id, minutes, spent_on, created_at, updated_at)
		SELECT ?, (SELECT id FROM work_items WHERE project_id = ? AND id >= (SELECT MIN(id) FROM work_items) + (i * 7) % 9000 ORDER BY id LIMIT 1), ?, NULL, 30 + i % 90,
		  date('2026-01-01', '+' || (i % 365) || ' days'), 0, 0 FROM n`, pid, pid, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 10000)
		INSERT INTO audit_entries (actor, operation, target, changes, ts)
		SELECT 'human:user-1', 'item.update', ? || '-' || (1 + i % 10000), '{}', i FROM n`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE work_items SET due_at = 1790000000000 + (id % 365) * 86400000 WHERE id % 3 = 0`); err != nil {
		t.Fatal(err)
	}
	measure := func(name string, f func() error) {
		var runs []time.Duration
		for i := 0; i < 100; i++ {
			s := time.Now()
			if err := f(); err != nil {
				t.Fatal(err)
			}
			runs = append(runs, time.Since(s))
		}
		slices.Sort(runs)
		t.Logf("%s: p50=%v p95=%v", name, runs[50], runs[95])
	}
	measure("progress", func() error { _, err := svc.ProjectProgress(ctx, actor, project); return err })
	measure("timeline (90 days)", func() error {
		_, err := svc.ProjectTimeline(ctx, actor, project, 1790000000000, 1790000000000+90*86400000, 0)
		return err
	})
	measure("activity (50)", func() error { _, err := svc.ProjectActivity(ctx, actor, project, 0, 0); return err })
	measure("time summary", func() error { _, err := svc.TimeSummary(ctx, actor, project, core.TimeFilter{}); return err })
}

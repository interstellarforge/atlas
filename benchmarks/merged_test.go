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

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run MergedBoard -v
// Board reads (core level, no HTTP) on the 10.000-item seed, one column per status vs merged columns (ADR-0011).
func TestMergedBoard(t *testing.T) {
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
	var project string
	if err := d.R.QueryRow(`SELECT key FROM projects ORDER BY id LIMIT 1`).Scan(&project); err != nil {
		t.Fatal(err)
	}
	for _, merged := range []bool{false, true} {
		if err := svc.SetMergeColumns(context.Background(), actor, project, merged); err != nil {
			t.Fatal(err)
		}
		var runs []time.Duration
		for i := 0; i < 200; i++ {
			s := time.Now()
			b, err := svc.Board(context.Background(), actor, project, core.BoardFilter{Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, time.Since(s))
			if i == 0 {
				t.Logf("merged=%v: %d columns", merged, len(b.Columns))
			}
		}
		slices.Sort(runs)
		t.Logf("board read, merged=%v: p50=%v p95=%v", merged, runs[len(runs)/2], runs[len(runs)*95/100])
	}
}

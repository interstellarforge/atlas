package benchmarks_test

import (
	"context"
	"encoding/json"
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

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run Context -v
// Bootstrap, search and detail at core level on 10.000 items plus 200 context entries in one project (ADR-0015).
// Targets: p95 < 100 ms and a default payload of at most 4 KiB.
func TestContext(t *testing.T) {
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
	if err := seed.Run(d.W, seed.Options{Items: 10000, Projects: 1, Seed: 1, ContextEntries: 200}); err != nil {
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
	if err := d.R.QueryRow(`SELECT key FROM projects ORDER BY id LIMIT 1`).Scan(&project); err != nil {
		t.Fatal(err)
	}

	measure := func(name string, maxPayload int, f func() (any, error)) {
		var runs []time.Duration
		var size int
		for i := 0; i < 200; i++ {
			s := time.Now()
			v, err := f()
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, time.Since(s))
			b, _ := json.Marshal(v)
			size = len(b)
		}
		slices.Sort(runs)
		t.Logf("%s: p50=%v p95=%v payload=%d bytes", name, runs[100], runs[190], size)
		if runs[190] > 100*time.Millisecond {
			t.Errorf("%s: p95 %v is over the 100 ms target", name, runs[190])
		}
		if size > maxPayload {
			t.Errorf("%s: payload %d bytes is over the %d bytes allowed", name, size, maxPayload)
		}
	}
	measure("bootstrap (default budget)", 4096, func() (any, error) {
		return svc.ContextBootstrapFor(ctx, actor, project, nil, 0, "", 0)
	})
	measure("search 'module 7' (default budget)", 4096, func() (any, error) {
		return svc.ListContext(ctx, actor, project, core.ContextFilter{Q: "module 7", Compact: true, Limit: 10, MaxBytes: 4096})
	})
	measure("detail (3 entries, 16 KiB budget)", 16384, func() (any, error) {
		return svc.ContextDetails(ctx, actor, project, []string{"seed-1", "seed-2", "seed-3"}, "", 16384, nil)
	})
	measure("list (all 200, no body, no budget)", 1<<20, func() (any, error) {
		return svc.ListContext(ctx, actor, project, core.ContextFilter{MaxBytes: 0})
	})
}

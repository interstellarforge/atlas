package benchmarks_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kees/atlas/benchmarks/seed"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run Baseline -v
// Measures SQLite-level latency on the 10.000-item seed (no HTTP layer yet).
func TestBaseline(t *testing.T) {
	if testing.Short() || !benchEnabled() {
		t.Skip("set ATLAS_BENCH=1 to run")
	}
	for _, sync := range []string{"FULL", "NORMAL"} {
		t.Run(sync, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{Synchronous: sync})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			if err := seed.Run(d.W, seed.Options{Items: 10000, Projects: 3, Seed: 1}); err != nil {
				t.Fatal(err)
			}

			report(t, "column page (50 cards)", measure(500, func(i int) error {
				return drain(d.R.Query(`SELECT key, title, priority, version FROM work_items
					WHERE project_id = 1 AND status_id = 3 AND archived_at IS NULL ORDER BY rank LIMIT 50`))
			}))
			// Subtree read of the first epic's tree, three strategies (ADR-0003).
			var epicID int64
			var epicPath string
			if err := d.R.QueryRow(`SELECT id, path FROM work_items WHERE parent_id IS NULL ORDER BY id LIMIT 1`).Scan(&epicID, &epicPath); err != nil {
				t.Fatal(err)
			}
			report(t, "subtree: path LIKE prefix", measure(500, func(i int) error {
				return drain(d.R.Query(`SELECT key FROM work_items WHERE path LIKE ? || '%'`, epicPath))
			}))
			report(t, "subtree: path range scan", measure(500, func(i int) error {
				return drain(d.R.Query(`SELECT key FROM work_items WHERE path >= ? AND path < ?`,
					epicPath, strings.TrimSuffix(epicPath, "/")+"0"))
			}))
			report(t, "subtree: recursive CTE", measure(500, func(i int) error {
				return drain(d.R.Query(`WITH RECURSIVE s(id) AS (SELECT ? UNION ALL SELECT w.id FROM work_items w JOIN s ON w.parent_id = s.id)
					SELECT w.key FROM work_items w JOIN s ON s.id = w.id`, epicID))
			}))
			// Title search as items_search/ListItems runs it (ATL-291): substring LIKE over all 10.000 items.
			var common string
			if err := d.R.QueryRow(`SELECT substr(title, 1, instr(title || ' ', ' ') - 1) FROM work_items GROUP BY 1 ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&common); err != nil {
				t.Fatal(err)
			}
			for name, term := range map[string]string{"common word " + common: common, "no match": "zzqxj"} {
				report(t, "title search: "+name, measure(200, func(i int) error {
					return drain(d.R.Query(`SELECT key, title FROM work_items i WHERE i.title LIKE ? ESCAPE '\' AND i.archived_at IS NULL ORDER BY i.key LIMIT 50`, "%"+term+"%"))
				}))
			}
			report(t, "item update (tx, FULL/NORMAL)", measure(500, func(i int) error {
				_, err := d.W.Exec(`UPDATE work_items SET status_id = 4, version = version + 1, updated_at = ? WHERE id = ?`,
					time.Now().UnixMilli(), 10+i)
				return err
			}))
		})
	}
}

func drain(rows *sql.Rows, err error) error {
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func measure(n int, f func(i int) error) []time.Duration {
	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		s := time.Now()
		if err := f(i); err != nil {
			panic(err)
		}
		out = append(out, time.Since(s))
	}
	slices.Sort(out)
	return out
}

func report(t *testing.T, name string, d []time.Duration) {
	t.Helper()
	p := func(q float64) time.Duration { return d[int(float64(len(d)-1)*q)] }
	t.Log(fmt.Sprintf("%-34s p50=%-10v p95=%-10v max=%v", name, p(.5), p(.95), d[len(d)-1]))
}

package benchmarks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kees/atlas/benchmarks/seed"
	"github.com/kees/atlas/internal/api"
	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/events"
	"github.com/kees/atlas/migrations"
)

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run Concurrent -v
// Five clients hit the real HTTP handlers on the 10.000-item seed: each reads the board and updates its own items.
func TestConcurrentClients(t *testing.T) {
	if testing.Short() || !benchEnabled() {
		t.Skip("set ATLAS_BENCH=1 to run")
	}
	const clients, rounds = 5, 100
	for _, sync_ := range []string{"FULL", "NORMAL"} {
		t.Run(sync_, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{Synchronous: sync_})
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
			_, token, err := a.CreateToken(u.ID, "bench", []string{auth.ScopeRead, auth.ScopeWrite}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			svc := core.New(d, a)
			feed := events.New(d, svc)
			svc.SetNotifier(feed.Hub().Notify)
			srv := httptest.NewServer(api.New(api.Deps{DB: d, Auth: a, Core: svc, Feed: feed, SPA: fstest.MapFS{"index.html": {Data: []byte("x")}}}).Handler())
			defer srv.Close()

			var project string
			if err := d.R.QueryRow(`SELECT key FROM projects ORDER BY id LIMIT 1`).Scan(&project); err != nil {
				t.Fatal(err)
			}
			rows, err := d.R.Query(`SELECT key FROM work_items WHERE project_id = 1 ORDER BY id LIMIT ?`, clients*rounds)
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for rows.Next() {
				var k string
				rows.Scan(&k)
				keys = append(keys, k)
			}
			rows.Close()

			do := func(method, path string, body any) error {
				var buf bytes.Buffer
				if body != nil {
					json.NewEncoder(&buf).Encode(body)
				}
				req, _ := http.NewRequest(method, srv.URL+path, &buf)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return err
				}
				res.Body.Close()
				if res.StatusCode >= 300 {
					return fmt.Errorf("%s %s: %d", method, path, res.StatusCode)
				}
				return nil
			}

			var mu sync.Mutex
			var board, update []time.Duration
			var wg sync.WaitGroup
			for c := 0; c < clients; c++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < rounds; i++ {
						s := time.Now()
						if err := do("GET", "/api/v1/projects/"+project+"/board?limit=50", nil); err != nil {
							t.Error(err)
							return
						}
						b := time.Since(s)
						s = time.Now()
						// Each client owns its items, so versions are 1, 2, ... without conflicts (updates are the only writes).
						if err := do("PATCH", "/api/v1/items/"+keys[c*rounds+i], map[string]any{"version": 1, "priority": "high"}); err != nil {
							t.Error(err)
							return
						}
						u := time.Since(s)
						mu.Lock()
						board, update = append(board, b), append(update, u)
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			slices.Sort(board)
			slices.Sort(update)
			report(t, fmt.Sprintf("board GET, %d clients", clients), board)
			report(t, fmt.Sprintf("item PATCH, %d clients", clients), update)
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			t.Logf("go runtime memory from OS (server + client + seed, same process): %.0f MB", float64(ms.Sys)/(1<<20))
		})
	}
}

package core_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

type env struct {
	t                        *testing.T
	db                       *db.DB
	svc                      *core.Service
	admin, alice, bob, carol auth.Actor
	aliceID, bobID, carolID  int64
}

func human(id int64, admin bool) auth.Actor {
	return auth.Actor{Kind: auth.KindHuman, UserID: id, IsAdmin: admin}
}

func setup(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(d)
	mk := func(email string, admin bool) int64 {
		u, err := a.CreateUser(email, email, "password-123", admin)
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	e := &env{t: t, db: d, svc: core.New(d, a)}
	adminID := mk("admin@x.io", true)
	e.aliceID, e.bobID, e.carolID = mk("alice@x.io", false), mk("bob@x.io", false), mk("carol@x.io", false)
	e.admin, e.alice, e.bob, e.carol = human(adminID, true), human(e.aliceID, false), human(e.bobID, false), human(e.carolID, false)
	return e
}

func (e *env) project(key string) {
	e.t.Helper()
	if _, err := e.svc.CreateProject(context.Background(), e.admin, key, key+" project"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) item(a auth.Actor, in core.CreateItem) core.Item {
	e.t.Helper()
	it, err := e.svc.CreateItem(context.Background(), a, in)
	if err != nil {
		e.t.Fatalf("create %s %q: %v", in.Type, in.Title, err)
	}
	return it
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.R.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func isRule(err error) bool    { var r *core.RuleError; return errors.As(err, &r) }
func isInvalid(err error) bool { var r *core.InvalidError; return errors.As(err, &r) }
func isConflict(err error) bool {
	var r *core.ConflictError
	return errors.As(err, &r)
}

var ctx = context.Background()

func TestConcurrentUpdatesWithSameVersion(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "race"})

	const n = 12
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"title": "writer " + string(rune('a'+i))})
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !isConflict(err):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d updates succeeded, want exactly 1", wins)
	}
	got, _ := e.svc.GetItem(ctx, e.admin, it.Key)
	if got.Version != 2 {
		t.Fatalf("version = %d, want 2", got.Version)
	}
	if n := e.count(`SELECT COUNT(*) FROM change_events WHERE item_key = ? AND type = 'item.updated'`, it.Key); n != 1 {
		t.Fatalf("%d update events, want 1", n)
	}
}

func TestPermissions(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.project("OTH")
	for id, role := range map[int64]string{e.aliceID: "member", e.bobID: "viewer"} {
		if err := e.svc.SetMember(ctx, e.admin, "EXO", id, role); err != nil {
			t.Fatal(err)
		}
	}
	it := e.item(e.alice, core.CreateItem{Project: "EXO", Type: "story", Title: "visible"})

	// Not a member: the project does not exist for carol.
	if _, err := e.svc.GetItem(ctx, e.carol, it.Key); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("non-member read: %v", err)
	}
	if ps, _ := e.svc.ListProjects(ctx, e.carol); len(ps) != 0 {
		t.Fatalf("carol sees %d projects", len(ps))
	}
	// Viewer reads but cannot write.
	if _, err := e.svc.GetItem(ctx, e.bob, it.Key); err != nil {
		t.Fatalf("viewer read: %v", err)
	}
	if _, err := e.svc.UpdateItem(ctx, e.bob, it.Key, it.Version, map[string]any{"title": "x"}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("viewer write: %v", err)
	}
	if _, err := e.svc.CreateItem(ctx, e.bob, core.CreateItem{Project: "EXO", Type: "task", Title: "x"}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("viewer create: %v", err)
	}
	// Members cannot administer the project.
	if _, err := e.svc.AddStatus(ctx, e.alice, "EXO", "Blocked", "active", nil); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("member admin action: %v", err)
	}
	// Assignee must belong to the project.
	if _, err := e.svc.UpdateItem(ctx, e.alice, it.Key, it.Version, map[string]any{"assignee": e.carolID}); !isInvalid(err) {
		t.Fatalf("non-member assignee: %v", err)
	}

	// Tokens never exceed scope or project limits.
	readTok := auth.Actor{Kind: auth.KindToken, UserID: e.aliceID, TokenID: 1, Scopes: []string{auth.ScopeRead}}
	if _, err := e.svc.UpdateItem(ctx, readTok, it.Key, it.Version, map[string]any{"title": "x"}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("read-only token write: %v", err)
	}
	limited := auth.Actor{Kind: auth.KindToken, UserID: e.aliceID, TokenID: 2, Scopes: []string{auth.ScopeRead, auth.ScopeWrite}, Projects: []string{"OTH"}}
	if _, err := e.svc.GetItem(ctx, limited, it.Key); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("token outside its project list: %v", err)
	}

	// The last admin cannot be removed.
	if err := e.svc.RemoveMember(ctx, e.admin, "EXO", e.admin.UserID); !isRule(err) {
		t.Fatalf("remove last admin: %v", err)
	}
}

func TestHierarchyAndReparentKeepPathsConsistent(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	epic1 := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "epic", Title: "E1"})
	epic2 := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "epic", Title: "E2"})
	story := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "story", Title: "S", Parent: epic1.Key})
	task := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "T", Parent: story.Key})

	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "bad", Parent: epic1.Key}); !isRule(err) {
		t.Fatalf("task under epic: %v", err)
	}
	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "EXO", Type: "epic", Title: "bad", Parent: epic1.Key}); !isRule(err) {
		t.Fatalf("epic under epic: %v", err)
	}

	moved, err := e.svc.UpdateItem(ctx, e.admin, story.Key, story.Version, map[string]any{"parent": epic2.Key})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Parent == nil || *moved.Parent != epic2.Key {
		t.Fatalf("parent = %v", moved.Parent)
	}
	under, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Under: epic2.Key, Fields: []string{"key"}})
	if err != nil || len(under.Items) != 3 { // epic2 itself, story, task
		t.Fatalf("subtree of E2: %v %v", under.Items, err)
	}
	if got, _ := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Under: epic1.Key, Fields: []string{"key"}}); len(got.Items) != 1 {
		t.Fatalf("subtree of E1 still has %d items", len(got.Items))
	}
	_ = task

	rep, err := e.svc.CheckPaths(ctx, false)
	if err != nil || len(rep.Mismatches) != 0 || rep.Unreachable != 0 {
		t.Fatalf("paths after reparent: %+v %v", rep, err)
	}

	// A corrupted path is detected and repaired.
	if _, err := e.db.W.Exec(`UPDATE work_items SET path = '/999/' WHERE key = ?`, task.Key); err != nil {
		t.Fatal(err)
	}
	rep, _ = e.svc.CheckPaths(ctx, false)
	if len(rep.Mismatches) != 1 || rep.Mismatches[0].Key != task.Key {
		t.Fatalf("corruption not detected: %+v", rep)
	}
	if rep, _ = e.svc.CheckPaths(ctx, true); rep.Fixed != 1 {
		t.Fatalf("fix: %+v", rep)
	}
	if rep, _ = e.svc.CheckPaths(ctx, false); len(rep.Mismatches) != 0 {
		t.Fatalf("still inconsistent: %+v", rep)
	}
}

func TestWorkflowTransitionRules(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "t"}) // Backlog

	err := e.svc.SetTransitions(ctx, e.admin, "EXO", []core.Transition{{From: "Backlog", To: "Todo"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "In Progress"}); !isRule(err) {
		t.Fatalf("forbidden transition: %v", err)
	}
	it, err = e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "Todo"})
	if err != nil || it.Status != "Todo" {
		t.Fatalf("allowed transition: %v %v", it.Status, err)
	}
	if err := e.svc.SetTransitions(ctx, e.admin, "EXO", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"status": "Done"}); err != nil {
		t.Fatalf("free workflow: %v", err)
	}
}

func TestLinksBlockedAndCycles(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	a := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "A"})
	b := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "B"})
	c := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "C"})

	for _, l := range [][2]string{{a.Key, b.Key}, {b.Key, c.Key}} {
		if err := e.svc.AddLink(ctx, e.admin, l[0], l[1], "blocks"); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.AddLink(ctx, e.admin, c.Key, a.Key, "blocks"); !isRule(err) {
		t.Fatalf("indirect cycle accepted: %v", err)
	}
	if err := e.svc.AddLink(ctx, e.admin, a.Key, a.Key, "relates"); !isInvalid(err) {
		t.Fatalf("self link: %v", err)
	}

	blocked := func(key string) bool { it, _ := e.svc.GetItem(ctx, e.admin, key); return it.Blocked }
	if !blocked(b.Key) || blocked(a.Key) {
		t.Fatal("blocked state wrong before completion")
	}
	if _, err := e.svc.UpdateItem(ctx, e.admin, a.Key, a.Version, map[string]any{"status": "Done"}); err != nil {
		t.Fatal(err)
	}
	if blocked(b.Key) {
		t.Fatal("B still blocked after its blocker is Done")
	}
	links, err := e.svc.ListLinks(ctx, e.admin, b.Key)
	if err != nil || len(links.Incoming) != 1 || len(links.Outgoing) != 1 {
		t.Fatalf("links: %+v %v", links, err)
	}
}

func TestFailedMutationLeavesNoTraceAndSuccessIsJournaled(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	events := e.count(`SELECT COUNT(*) FROM change_events`)
	audits := e.count(`SELECT COUNT(*) FROM audit_entries`)

	// The label is rejected after the item row was inserted: everything must roll back.
	_, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "x", Labels: []string{strings.Repeat("l", 51)}})
	if !isInvalid(err) {
		t.Fatalf("expected invalid label, got %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM work_items`); n != 0 {
		t.Fatalf("%d items left behind", n)
	}
	if n := e.count(`SELECT next_item_number FROM projects WHERE key = 'EXO'`); n != 1 {
		t.Fatalf("item number consumed: %d", n)
	}
	if e.count(`SELECT COUNT(*) FROM change_events`) != events || e.count(`SELECT COUNT(*) FROM audit_entries`) != audits {
		t.Fatal("failed mutation wrote journal entries")
	}

	bctx := core.WithBatch(ctx, "batch-7")
	it, err := e.svc.CreateItem(bctx, e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Key != "EXO-1" {
		t.Fatalf("key = %s", it.Key)
	}
	var actor string
	var batch *string
	if err := e.db.R.QueryRow(`SELECT actor, batch_id FROM audit_entries WHERE target = 'EXO-1'`).Scan(&actor, &batch); err != nil {
		t.Fatal(err)
	}
	if actor != e.admin.String() || batch == nil || *batch != "batch-7" {
		t.Fatalf("audit actor=%s batch=%v", actor, batch)
	}
	if e.count(`SELECT COUNT(*) FROM change_events WHERE item_key = 'EXO-1' AND type = 'item.created'`) != 1 {
		t.Fatal("create event missing")
	}
}

func TestCustomFieldsValidation(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	if err := e.svc.AddCustomField(ctx, e.admin, "EXO", core.CustomField{Name: "Sprint", Type: "select", Options: []string{"s1", "s2"}, Required: true, Default: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AddCustomField(ctx, e.admin, "EXO", core.CustomField{Name: "Estimate", Type: "number"}); err != nil {
		t.Fatal(err)
	}

	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "t"})
	if it.CustomData["Sprint"] != "s1" {
		t.Fatalf("default not applied: %v", it.CustomData)
	}
	bad := []map[string]any{{"Nope": 1}, {"Estimate": "high"}, {"Sprint": "s9"}}
	for _, data := range bad {
		if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "x", CustomData: data}); !isInvalid(err) {
			t.Fatalf("custom_data %v accepted: %v", data, err)
		}
	}
	it, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"custom_data": map[string]any{"Estimate": 5.0}})
	if err != nil || it.CustomData["Estimate"] != 5.0 {
		t.Fatalf("merge: %v %v", it.CustomData, err)
	}
	// Required fields cannot be removed.
	if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"custom_data": map[string]any{"Sprint": nil}}); !isInvalid(err) {
		t.Fatalf("required field removed: %v", err)
	}
}

func TestImportCommentKeepsAuthorAndTimeForAdminsOnly(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.aliceID, "member"); err != nil {
		t.Fatal(err)
	}
	it := e.item(e.alice, core.CreateItem{Project: "EXO", Type: "story", Title: "migrated"})

	if _, err := e.svc.ImportComment(ctx, e.alice, it.Key, "old", "Dana", 1600000000000); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("member import: %v", err)
	}
	c, err := e.svc.ImportComment(ctx, e.admin, it.Key, "old", "Dana", 1600000000000)
	if err != nil {
		t.Fatal(err)
	}
	if c.CreatedAt != 1600000000000 || !strings.HasPrefix(c.Actor, "Dana (imported by ") {
		t.Fatalf("import kept %+v", c)
	}
	if _, err := e.svc.ImportComment(ctx, e.admin, it.Key, "old", "", 1); !isInvalid(err) {
		t.Fatalf("missing author: %v", err)
	}
}

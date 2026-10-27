package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

func TestCreateUserIsHumanAdminOnlyAndJournaled(t *testing.T) {
	e, ctx := setup(t), context.Background()
	pat := auth.Actor{Kind: auth.KindToken, UserID: e.admin.UserID, TokenID: 1, IsAdmin: true, Scopes: []string{auth.ScopeRead, auth.ScopeWrite}}
	if _, err := e.svc.CreateUser(ctx, pat, "evil@x.io", "Evil", "password-123", true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("token created a user: %v", err)
	}
	if _, err := e.svc.CreateUser(ctx, e.alice, "n@x.io", "N", "password-123", false); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("non-admin created a user: %v", err)
	}
	if _, err := e.svc.CreateUser(ctx, e.admin, "new@x.io", "New", "password-123", false); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_entries WHERE operation = 'user.create'`); n != 1 {
		t.Fatalf("audit entries: %d", n)
	}
}

func TestTokenListsOnlyUsersSharingItsProjects(t *testing.T) {
	e, ctx := setup(t), context.Background()
	e.project("EXO")
	e.project("OTH")
	for _, m := range []struct {
		key string
		id  int64
	}{{"EXO", e.aliceID}, {"EXO", e.bobID}, {"OTH", e.carolID}} {
		if err := e.svc.SetMember(ctx, e.admin, m.key, m.id, ""); err != nil {
			t.Fatal(err)
		}
	}
	tok := auth.Actor{Kind: auth.KindToken, UserID: e.aliceID, TokenID: 2, Scopes: []string{auth.ScopeRead}}
	list, err := e.svc.ListUsers(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, u := range list {
		got[u.ID] = true
		if u.IsAdmin {
			t.Fatal("token saw an admin flag")
		}
	}
	if !got[e.aliceID] || !got[e.bobID] || got[e.carolID] {
		t.Fatalf("visible users: %v", got)
	}
}

func TestIdempotentRollsBackAndReplays(t *testing.T) {
	e, ctx := setup(t), context.Background()
	e.project("EXO")
	it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: "one"})
	var out struct{ N int }
	run := func(key string, fail bool) (bool, error) {
		return e.svc.Idempotent(ctx, e.admin, key, "req", &out, func(ctx context.Context, s *core.Service) error {
			if _, err := s.AddChecklistItem(ctx, e.admin, it.Key, "step"); err != nil {
				return err
			}
			if fail {
				return errors.New("late failure")
			}
			out.N++
			return nil
		})
	}
	if _, err := run("k1", true); err == nil {
		t.Fatal("expected failure")
	}
	if n := e.count(`SELECT COUNT(*) FROM checklist_items`); n != 0 {
		t.Fatalf("partial mutation survived: %d", n)
	}
	if replayed, err := run("k1", false); err != nil || replayed {
		t.Fatalf("first run: %v %v", replayed, err)
	}
	if replayed, err := run("k1", false); err != nil || !replayed || out.N != 1 {
		t.Fatalf("retry: replayed=%v err=%v n=%d", replayed, err, out.N)
	}
	if n := e.count(`SELECT COUNT(*) FROM checklist_items`); n != 1 {
		t.Fatalf("retry repeated the write: %d", n)
	}
}

func TestDeleteAccountScrubsEmailFromAudit(t *testing.T) {
	e, ctx := setup(t), context.Background()
	if _, err := e.svc.ChangeEmail(ctx, e.alice, "alice.new@x.io", "password-123"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.CreateInvite(ctx, e.admin, "alice.new@x.io", false, 7); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteAccount(ctx, e.alice, "password-123"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_entries WHERE changes LIKE '%alice%'`); n != 0 {
		t.Fatalf("%d audit entries still hold the address", n)
	}
}

func TestCSVExportNeutralisesFormulas(t *testing.T) {
	var b strings.Builder
	if err := (core.Export{Items: []core.ExportItem{{Item: core.Item{Key: "A-1", Type: "task", Title: "=HYPERLINK(\"x\")", Description: "-1"}}}}).WriteCSV(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"'=HYPERLINK`) || !strings.Contains(b.String(), "'-1") {
		t.Fatalf("formula not neutralised:\n%s", b.String())
	}
	rows, err := core.ParseImportCSV(strings.NewReader(b.String()))
	if err != nil || rows[0].Title != `=HYPERLINK("x")` {
		t.Fatalf("round trip: %v %+v", err, rows)
	}
}

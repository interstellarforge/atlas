package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

// The database refuses to end up without an active superadmin, whoever writes (ATL-483).
func TestTriggerKeepsOneActiveSuperadmin(t *testing.T) {
	e := setup(t)
	other := mkUserWith(t, e, "second@x.io", true)
	exec := func(q string, args ...any) error { _, err := e.db.W.Exec(q, args...); return err }
	if err := exec(`UPDATE users SET is_superadmin = 0 WHERE id = ?`, other); err != nil {
		t.Fatalf("demoting one of two: %v", err)
	}
	if err := exec(`UPDATE users SET is_superadmin = 1 WHERE id = ?`, other); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"demote":     `UPDATE users SET is_superadmin = 0 WHERE id = %d`,
		"deactivate": `UPDATE users SET deactivated_at = 1 WHERE id = %d`,
		"delete":     `DELETE FROM users WHERE id = %d`,
	} {
		if err := exec(fmt.Sprintf(q, other)); err != nil { // the other one is still active: allowed once
			t.Fatalf("%s with two superadmins: %v", name, err)
		}
		last := e.admin.UserID
		if err := exec(fmt.Sprintf(q, last)); err == nil {
			t.Errorf("%s of the last active superadmin was accepted by the database", name)
		}
		// put the second one back for the next case
		if name != "delete" {
			if err := exec(`UPDATE users SET is_superadmin = 1, deactivated_at = NULL WHERE id = ?`, other); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// The core says why, in a rule error, before the trigger has to.
func TestLastSuperadminCoreGuard(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for name, err := range map[string]error{
		"demote":  e.svc.SetUserSuperadmin(ctx, e.admin, e.admin.UserID, false),
		"suspend": e.svc.SetUserActive(ctx, e.admin, mkUserWith(t, e, "x@x.io", false), false), // unrelated: allowed
	} {
		var re *core.RuleError
		switch name {
		case "demote":
			if !errors.As(err, &re) {
				t.Errorf("demoting the last superadmin: %v", err)
			}
		case "suspend":
			if err != nil {
				t.Errorf("suspending an ordinary user: %v", err)
			}
		}
	}
	var re *core.RuleError
	if err := e.svc.DeleteAccount(ctx, e.admin, "password-123"); !errors.As(err, &re) {
		t.Errorf("the last superadmin deleted their account: %v", err)
	}
	// with a second one the first may be demoted, and the second then cannot be
	second := human(mkUserWith(t, e, "second@x.io", true), true)
	if err := e.svc.SetUserSuperadmin(ctx, e.admin, e.admin.UserID, false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetUserSuperadmin(ctx, second, second.UserID, false); !errors.As(err, &re) {
		t.Errorf("the new last superadmin was demoted: %v", err)
	}
}

// A failing audit write rolls the change back, and a busy database changes nothing (ATL-483).
func TestSuperadminChangeRollsBackAndBusyChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atlas.db")
	open := func(busy time.Duration) (*core.Service, *db.DB) {
		d, err := db.Open(path, db.Options{BusyTimeout: busy})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		return core.New(d, auth.NewService(d)), d
	}
	svc, d := open(50 * time.Millisecond)
	holder, hd := open(0)
	au := auth.NewService(d)
	first, err := au.CreateUser("a@x.io", "a", "password-123", true)
	if err != nil {
		t.Fatal(err)
	}
	user, err := au.CreateUser("u@x.io", "u", "password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	actor := human(first.ID, true)
	ctx := context.Background()
	flag := func() bool {
		var v bool
		d.R.QueryRow(`SELECT is_superadmin FROM users WHERE id = ?`, user.ID).Scan(&v)
		return v
	}

	tx, err := hd.W.Begin() // another process holds the write lock
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SetUserSuperadmin(ctx, actor, user.ID, true)
	if !core.IsBusy(err) {
		t.Fatalf("a locked database must report busy, got %v", err)
	}
	tx.Rollback()
	_ = holder
	if flag() {
		t.Fatal("a busy database applied the change")
	}

	if _, err := d.W.Exec(`ALTER TABLE audit_entries RENAME TO audit_entries_gone`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetUserSuperadmin(ctx, actor, user.ID, true); err == nil || flag() {
		t.Fatalf("the grant survived a failing audit write: %v", err)
	}
}

// Two handles on one file demote or suspend each other at the same time: exactly one succeeds and one superadmin
// remains (the guard does not rely on a single connection).
func TestLastSuperadminConcurrent(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < 10; round++ {
		path := filepath.Join(t.TempDir(), "atlas.db")
		open := func() (*core.Service, *db.DB) {
			d, err := db.Open(path, db.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.Close() })
			if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			return core.New(d, auth.NewService(d)), d
		}
		one, d1 := open()
		two, _ := open()
		au := auth.NewService(d1)
		a, _ := au.CreateUser("a@x.io", "a", "password-123", true)
		b, _ := au.CreateUser("b@x.io", "b", "password-123", true)
		ha, hb := human(a.ID, true), human(b.ID, true)
		ops := []func() error{
			func() error { return one.SetUserSuperadmin(ctx, ha, b.ID, false) },
			func() error { return two.SetUserSuperadmin(ctx, hb, a.ID, false) },
		}
		if round%2 == 1 {
			ops = []func() error{
				func() error { return one.SetUserActive(ctx, ha, b.ID, false) },
				func() error { return two.SetUserActive(ctx, hb, a.ID, false) },
			}
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, op := range ops {
			wg.Add(1)
			go func() { defer wg.Done(); errs[i] = op() }()
		}
		wg.Wait()
		if (errs[0] == nil) == (errs[1] == nil) {
			t.Fatalf("round %d: errors %v and %v, want exactly one success", round, errs[0], errs[1])
		}
		var n int
		d1.R.QueryRow(`SELECT COUNT(*) FROM users WHERE is_superadmin = 1 AND deactivated_at IS NULL`).Scan(&n)
		if n != 1 {
			t.Fatalf("round %d: %d active superadmins", round, n)
		}
	}
}

// The operator path makes a SUPERADMIN for an active account only, and leaves an audit entry (ATL-437).
func TestOperatorGrantSuperadmin(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.svc.GrantSuperadmin(ctx, "nobody@x.io"); err == nil {
		t.Error("granted to an unknown email")
	}
	if err := e.svc.SetUserActive(ctx, e.admin, e.aliceID, false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.GrantSuperadmin(ctx, "alice@x.io"); err == nil {
		t.Error("granted to a suspended account")
	}
	if err := e.svc.GrantSuperadmin(ctx, "bob@x.io"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM users WHERE email = 'bob@x.io' AND is_superadmin = 1`); n != 1 {
		t.Fatal("the grant did not apply")
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_entries WHERE operation = 'user.superadmin' AND actor = 'system:cli'`); n != 1 {
		t.Fatalf("%d audit entries for the grant, want 1", n)
	}
}

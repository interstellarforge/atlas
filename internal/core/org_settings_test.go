package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kees/atlas/internal/core"
)

// ADR-0031 point 9: organization settings: only that organization's ADMIN, instance floors, strictest value across
// organizations, project creation by members, and the ADMIN's own audit log.
func TestOrganizationSettings(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	e.orgUser(e.aliceID)

	// who may read and write
	if _, err := e.svc.GetOrgSettings(ctx, e.carol, "MAIN"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization read the settings: %v", err)
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.carol, "MAIN", map[string]any{"require_2fa": true}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("the ADMIN of another organization changed the settings: %v", err)
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.alice, "MAIN", map[string]any{"require_2fa": true}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER changed the settings: %v", err)
	}

	// floors and shapes
	for name, patch := range map[string]map[string]any{
		"password below the instance floor": {"password_min_length": 7.0},
		"session days zero":                 {"session_days": 0.0},
		"token lifetime too long":           {"token_max_days": 400.0},
		"unknown creation rule":             {"project_creation": "everyone"},
		"unknown setting":                   {"colour": "red"},
		"not a boolean":                     {"require_2fa": "yes"},
	} {
		if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", patch); !isInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	st, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"password_min_length": 12.0, "session_days": 3.0})
	if err != nil || st.PasswordMinLength == nil || *st.PasswordMinLength != 12 || st.Instance.MinPasswordLength != 8 {
		t.Fatalf("update: %v %+v", err, st)
	}
	if st, _ = e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"session_days": nil}); st.SessionDays != nil {
		t.Fatalf("null did not clear the value: %+v", st)
	}

	// password minimum: the organization raises it; with two organizations the longest wins
	if err := e.svc.ChangePassword(ctx, e.alice, "password-123", "eleven-char"); !isInvalid(err) {
		t.Fatalf("a password below the organization minimum: %v", err)
	}
	if err := e.svc.ChangePassword(ctx, e.alice, "password-123", "twelve-chars"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetOrgMember(ctx, e.carol, "OTHER", e.aliceID, "user", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.carol, "OTHER", map[string]any{"password_min_length": 16.0}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ChangePassword(ctx, e.alice, "twelve-chars", "twelve-chars-2"); !isInvalid(err) {
		t.Fatalf("the strictest organization minimum must win: %v", err)
	}

	// project creation: ADMINs only by default; members when the organization says so
	if _, err := e.svc.CreateProjectIn(ctx, e.alice, "MAIN", "MEM", "Member project"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a member created a project by default: %v", err)
	}
	if _, err := e.svc.UpdateOrgSettings(ctx, e.admin, "MAIN", map[string]any{"project_creation": "members"}); err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.CreateProjectIn(ctx, e.alice, "MAIN", "MEM", "Member project")
	if err != nil || p.Role != "member" {
		t.Fatalf("a member could not create a project: %v %+v", err, p)
	}
	if _, err := e.svc.GetProject(ctx, e.alice, "MEM"); err != nil {
		t.Fatalf("the creator cannot open their project: %v", err)
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.bob, "MAIN", "NOPE", "Stranger"); err == nil {
		t.Fatal("a stranger created a project in the organization")
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.carol, "MAIN", "NOP2", "Other ADMIN"); err == nil {
		t.Fatal("the ADMIN of another organization created a project here")
	}
	if _, err := e.svc.CreateProjectIn(ctx, e.alice, "OTHER", "NOP3", "Wrong organization"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("the rule of one organization leaked into another: %v", err)
	}

	// the ADMIN reads the log of their own organization only
	if _, err := e.svc.CreateProjectIn(ctx, e.carol, "OTHER", "OTH", "Other project"); err != nil {
		t.Fatal(err)
	}
	log, err := e.svc.ListOrgAudit(ctx, e.admin, "MAIN", core.AuditFilter{})
	if err != nil || len(log) == 0 {
		t.Fatalf("audit: %v %d", err, len(log))
	}
	seen := map[string]bool{}
	for _, en := range log {
		seen[en.Target] = true
	}
	if !seen["MEM"] || !seen["org:MAIN"] || seen["OTH"] || seen["org:OTHER"] {
		t.Fatalf("the log is not scoped to the organization: %v", seen)
	}
	if _, err := e.svc.ListOrgAudit(ctx, e.carol, "MAIN", core.AuditFilter{}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another ADMIN read the log: %v", err)
	}
	if _, err := e.svc.ListOrgAudit(ctx, e.alice, "MAIN", core.AuditFilter{}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER read the log: %v", err)
	}
}

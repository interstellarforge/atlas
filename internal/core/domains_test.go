package core_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

// ADR-0031: email domains: claim and confirmation, free mail and a held domain refused alike, an unconfirmed domain
// gives nothing, an unverified email never auto-joins, fallback without mail, rejected requesters never auto-join,
// verification links are single use and expire.
func TestEmailDomains(t *testing.T) {
	e, _, _ := mailEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOrganizationFor(ctx, e.admin, "OTHER", "Other", "carol@x.io"); err != nil {
		t.Fatal(err)
	}
	joined := func(id int64, key string) bool {
		return e.count(`SELECT COUNT(*) FROM organization_memberships m JOIN organizations o ON o.id = m.organization_id WHERE m.user_id = ? AND o.key = ?`, id, key) == 1
	}
	waiting := func(id int64) int {
		return e.count(`SELECT COUNT(*) FROM organization_join_requests WHERE user_id = ? AND status = 'pending'`, id)
	}

	// claiming: ADMIN only, valid domain, no free mail, the same refusal for a held domain
	if _, err := e.svc.ClaimDomain(ctx, e.bob, "MAIN", "x.io", ""); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("a stranger claimed: %v", err)
	}
	if _, err := e.svc.ClaimDomain(ctx, e.admin, "MAIN", "not a domain", ""); !isInvalid(err) {
		t.Fatalf("an invalid domain: %v", err)
	}
	if _, err := e.svc.ClaimDomain(ctx, e.admin, "MAIN", "x.io", "sometimes"); !isInvalid(err) {
		t.Fatalf("an unknown mode: %v", err)
	}
	_, freeErr := e.svc.ClaimDomain(ctx, e.admin, "MAIN", "Gmail.com", "")
	if !isRule(freeErr) {
		t.Fatalf("a free-mail domain was claimed: %v", freeErr)
	}
	d, err := e.svc.ClaimDomain(ctx, e.admin, "MAIN", "X.io", "request")
	if err != nil || d.Domain != "x.io" || d.Status != "pending" {
		t.Fatalf("claim: %v %+v", err, d)
	}
	_, heldErr := e.svc.ClaimDomain(ctx, e.carol, "OTHER", "x.io", "")
	if !isRule(heldErr) || heldErr.Error() != freeErr.Error() {
		t.Fatalf("a held domain must be refused like free mail: %v vs %v", heldErr, freeErr)
	}

	// an unconfirmed domain gives nothing
	if err := e.svc.RequestJoin(ctx, e.alice, "MAIN"); err != nil || waiting(e.aliceID) != 0 {
		t.Fatalf("a pending claim let a request through: %v", err)
	}
	if _, err := e.svc.ListDomainClaims(ctx, e.alice, ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN read the claims: %v", err)
	}
	if err := e.svc.DecideDomain(ctx, e.alice, d.ID, "approve"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a non-SUPERADMIN confirmed: %v", err)
	}
	if q, err := e.svc.ListDomainClaims(ctx, e.admin, ""); err != nil || len(q) != 1 || q[0].OrganizationKey != "MAIN" {
		t.Fatalf("claims: %v %+v", err, q)
	}
	if err := e.svc.DecideDomain(ctx, e.admin, d.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DecideDomain(ctx, e.admin, d.ID, "approve"); !isRule(err) {
		t.Fatalf("confirming twice: %v", err)
	}

	// mode request: a request is filed, no join
	if err := e.svc.RequestJoin(ctx, e.alice, "MAIN"); err != nil || waiting(e.aliceID) != 1 || joined(e.aliceID, "MAIN") {
		t.Fatalf("mode request: %v", err)
	}

	// mode auto, mail not set up: nothing auto-joins, the request is filed instead
	if err := e.svc.SetDomainMode(ctx, e.admin, "MAIN", d.ID, "auto"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestJoin(ctx, e.bob, "MAIN"); err != nil || waiting(e.bobID) != 1 || joined(e.bobID, "MAIN") {
		t.Fatalf("auto without mail must fall back to a request: %v", err)
	}
	if err := e.svc.RequestEmailVerification(ctx, e.carol); err == nil {
		t.Fatal("requested a verification mail without outgoing mail")
	}
	saveMail(t, e, pw("secret"))

	// mail is set up but the email is unverified: still a request, and no join options are shown
	if opts, _ := e.svc.JoinOptions(ctx, e.carol); len(opts) != 0 {
		t.Fatalf("an unverified address saw join options: %+v", opts)
	}
	if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil || waiting(e.carolID) != 1 || joined(e.carolID, "MAIN") {
		t.Fatalf("an unverified email auto-joined: %v", err)
	}

	// a mailed link verifies once and joins (mode auto, mail set up)
	lastToken := func() string {
		var body string
		if err := e.db.R.QueryRow(`SELECT body FROM mail_outbox ORDER BY id DESC LIMIT 1`).Scan(&body); err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`/verify/(\S+)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no verification link in %q", body)
		}
		return m[1]
	}
	if err := e.svc.RequestEmailVerification(ctx, e.bob); err != nil { // kept for the expiry check below
		t.Fatal(err)
	}
	expiring := lastToken()
	if err := e.svc.RequestEmailVerification(ctx, e.alice); err != nil {
		t.Fatal(err)
	}
	token := lastToken()
	if err := e.svc.VerifyEmail(ctx, "wrong"); !isRule(err) {
		t.Fatalf("a wrong link: %v", err)
	}
	if err := e.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	if !joined(e.aliceID, "MAIN") || e.count(`SELECT COUNT(*) FROM users WHERE id = ? AND email_verified_at IS NOT NULL`, e.aliceID) != 1 {
		t.Fatal("a verified address of a confirmed auto domain did not join")
	}
	if err := e.svc.VerifyEmail(ctx, token); !isRule(err) {
		t.Fatalf("reusing a link: %v", err)
	}
	if err := e.svc.RequestEmailVerification(ctx, e.alice); err != nil || e.count(`SELECT COUNT(*) FROM mail_outbox`) != 2 {
		t.Fatalf("a verified account was mailed again: %v", err)
	}

	// an expired link does nothing; a rejected requester never auto-joins, even when verified
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now.Add(25 * time.Hour) })
	if err := e.svc.VerifyEmail(ctx, expiring); !isRule(err) {
		t.Fatalf("an expired link: %v", err)
	}
	e.svc.SetClock(func() time.Time { return now })
	pending, _ := e.svc.ListJoinRequests(ctx, e.admin, "MAIN", "pending")
	for _, r := range pending {
		if r.UserEmail == "bob@x.io" {
			if err := e.svc.DecideJoinRequest(ctx, e.admin, "MAIN", r.ID, "reject", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := e.db.W.Exec(`UPDATE users SET email_verified_at = 1 WHERE id IN (?, ?)`, e.bobID, e.carolID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestJoin(ctx, e.bob, "MAIN"); err != nil || joined(e.bobID, "MAIN") {
		t.Fatalf("a rejected requester auto-joined: %v", err)
	}
	if opts, err := e.svc.JoinOptions(ctx, e.carol); err != nil || len(opts) != 1 || opts[0].Key != "MAIN" || opts[0].Mode != "auto" {
		t.Fatalf("join options of a verified address: %v %+v", err, opts)
	}
	if err := e.svc.RequestJoin(ctx, e.carol, "MAIN"); err != nil || !joined(e.carolID, "MAIN") {
		t.Fatalf("a verified address did not join at once: %v", err)
	}

	// removing the domain ends it; the SUPERADMIN can also revoke a confirmed one
	if err := e.svc.RemoveDomain(ctx, e.alice, "MAIN", d.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("a USER removed the domain: %v", err)
	}
	if err := e.svc.DecideDomain(ctx, e.admin, d.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.svc.ListDomains(ctx, e.admin, "MAIN"); len(list) != 0 {
		t.Fatalf("domains after revoking: %+v", list)
	}
	if _, err := e.svc.ClaimDomain(ctx, e.carol, "OTHER", "x.io", ""); err != nil {
		t.Fatalf("a released domain could not be claimed: %v", err)
	}
}

package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Email domains (ADR-0031): the organization ADMIN claims a domain, the SUPERADMIN confirms it. Only a confirmed
// domain does anything: accounts with that email domain may ask to join (mode request), or join at once when their
// email is verified and outgoing mail is set up (mode auto). One domain belongs to one organization.

// freeMailDomains can never be claimed: anyone can hold such an address.
// shortcut: a fixed list of common providers; extend it when a provider is missed.
var freeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"yahoo.com": true, "ymail.com": true, "icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true,
	"proton.me": true, "protonmail.com": true, "pm.me": true, "gmx.com": true, "gmx.net": true, "gmx.de": true,
	"mail.com": true, "zoho.com": true, "yandex.com": true, "hey.com": true, "fastmail.com": true, "web.de": true,
	"t-online.de": true, "live.nl": true, "hotmail.nl": true, "ziggo.nl": true, "kpnmail.nl": true, "planet.nl": true, "home.nl": true,
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

// OrgDomain is a claimed domain. Status is pending (waiting for the SUPERADMIN) or confirmed.
type OrgDomain struct {
	ID     int64  `json:"id"`
	Domain string `json:"domain"`
	Mode   string `json:"mode"` // request or auto
	Status string `json:"status"`
}

// DomainClaim is a claim as the SUPERADMIN's queue shows it.
type DomainClaim struct {
	OrgDomain
	OrganizationKey  string `json:"organization_key"`
	OrganizationName string `json:"organization_name"`
	ClaimedAt        int64  `json:"claimed_at"`
}

// emailDomain is the lower-cased part after the last @.
func emailDomain(email string) string {
	i := strings.LastIndex(email, "@")
	return strings.ToLower(email[i+1:])
}

func errDomainRefused() error { return rule("this domain cannot be claimed") }

func cleanDomain(d string) (string, error) {
	d = strings.ToLower(strings.TrimSpace(d))
	if len(d) > 253 || !domainRe.MatchString(d) {
		return "", invalid("enter a domain like example.com")
	}
	return d, nil
}

// ClaimDomain records the claim as pending. A free-mail provider and a domain another organization holds are refused
// with the same message, so the claimant learns nothing about other organizations.
func (s *Service) ClaimDomain(ctx context.Context, a auth.Actor, orgKey, domain, mode string) (OrgDomain, error) {
	domain, err := cleanDomain(domain)
	if err != nil {
		return OrgDomain{}, err
	}
	if mode == "" {
		mode = "request"
	}
	if mode != "request" && mode != "auto" {
		return OrgDomain{}, invalid("mode must be request or auto")
	}
	if freeMailDomains[domain] {
		return OrgDomain{}, errDomainRefused()
	}
	d := OrgDomain{Domain: domain, Mode: mode, Status: "pending"}
	err = s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO organization_domains (organization_id, domain, mode, claimed_by, claimed_at) VALUES (?, ?, ?, ?, ?)`,
			o.ID, domain, mode, a.UserID, s.nowMs())
		if isUnique(err) {
			return errDomainRefused()
		}
		if err != nil {
			return err
		}
		d.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "org.domain.claim", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"domain": domain, "mode": mode}})
	})
	return d, err
}

func (s *Service) ListDomains(ctx context.Context, a auth.Actor, orgKey string) ([]OrgDomain, error) {
	o, err := s.adminOfOrg(s.db.R, a, orgKey)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, domain, mode, status FROM organization_domains WHERE organization_id = ? ORDER BY domain`, o.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgDomain{}
	for rows.Next() {
		var d OrgDomain
		if err := rows.Scan(&d.ID, &d.Domain, &d.Mode, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Service) SetDomainMode(ctx context.Context, a auth.Actor, orgKey string, id int64, mode string) error {
	if mode != "request" && mode != "auto" {
		return invalid("mode must be request or auto")
	}
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE organization_domains SET mode = ? WHERE id = ? AND organization_id = ?`, mode, id, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "org.domain.mode", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"domain_id": id, "mode": mode}})
	})
}

// RemoveDomain drops a claimed or confirmed domain; the domain can then be claimed again by any organization.
func (s *Service) RemoveDomain(ctx context.Context, a auth.Actor, orgKey string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM organization_domains WHERE id = ? AND organization_id = ?`, id, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "org.domain.remove", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"domain_id": id}})
	})
}

// ListDomainClaims is the SUPERADMIN's list; status pending (default) or confirmed.
func (s *Service) ListDomainClaims(ctx context.Context, a auth.Actor, status string) ([]DomainClaim, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	if status == "" {
		status = "pending"
	}
	rows, err := s.db.R.Query(`SELECT d.id, d.domain, d.mode, d.status, o.key, o.name, d.claimed_at FROM organization_domains d
		JOIN organizations o ON o.id = d.organization_id WHERE d.status = ? ORDER BY d.claimed_at`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DomainClaim{}
	for rows.Next() {
		var c DomainClaim
		if err := rows.Scan(&c.ID, &c.Domain, &c.Mode, &c.Status, &c.OrganizationKey, &c.OrganizationName, &c.ClaimedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DecideDomain is the SUPERADMIN's: approve confirms a pending claim (compare-and-set); reject deletes a pending or
// confirmed one.
func (s *Service) DecideDomain(ctx context.Context, a auth.Actor, id int64, decision string) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	if decision != "approve" && decision != "reject" {
		return invalid("decision must be approve or reject")
	}
	return s.tx(func(tx *sql.Tx) error {
		var key, domain string
		err := tx.QueryRow(`SELECT o.key, d.domain FROM organization_domains d JOIN organizations o ON o.id = d.organization_id WHERE d.id = ?`, id).Scan(&key, &domain)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if decision == "approve" {
			res, err := tx.Exec(`UPDATE organization_domains SET status = 'confirmed', confirmed_by = ?, confirmed_at = ? WHERE id = ? AND status = 'pending'`, a.UserID, s.nowMs(), id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return rule("this claim was already confirmed")
			}
		} else if _, err := tx.Exec(`DELETE FROM organization_domains WHERE id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.domain." + decision, Target: orgTarget(key), Event: "admin.updated", Changes: map[string]any{"domain": domain}})
	})
}

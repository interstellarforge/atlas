package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Organization applications (ADR-0031): with the instance setting organization_creation = apply, a signed-in
// account asks for a new organization; the SUPERADMIN approves (the organization is created with the applicant as
// its first ADMIN) or rejects. Compare-and-set, audited. A key already taken is only reported to the SUPERADMIN, at
// decision time, so an applicant learns nothing about other organizations.

const (
	applyCooldownMs = 24 * 3_600_000
	applyWindowMs   = 30 * 86_400_000
	applyMaxInWin   = 3
)

// Application is an organization application as its applicant sees it.
type Application struct {
	ID              int64  `json:"id"`
	Key             string `json:"key"`
	Name            string `json:"name"`
	Note            string `json:"note"`
	Status          string `json:"status"` // pending, approved or rejected
	CreatedAt       int64  `json:"created_at"`
	DecidedAt       *int64 `json:"decided_at"`
	DecisionNote    string `json:"decision_note,omitempty"`
	OrganizationKey string `json:"organization_key,omitempty"`
}

// AdminApplication adds the applicant for the SUPERADMIN's queue.
type AdminApplication struct {
	Application
	ApplicantEmail string `json:"applicant_email"`
	ApplicantName  string `json:"applicant_name"`
}

// AdminOrganization is what the Atlas admin sees of an organization: no projects, no member list.
type AdminOrganization struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	CreatedAt int64    `json:"created_at"`
	Admins    []string `json:"admins"`
	Members   int      `json:"members"`
}

const applicationCols = `a.id, a.key, a.name, a.note, a.status, a.created_at, a.decided_at, a.decision_note, COALESCE(o.key, '')`

func scanApplication(row interface{ Scan(...any) error }, extra ...any) (Application, error) {
	var ap Application
	var at sql.NullInt64
	dest := append([]any{&ap.ID, &ap.Key, &ap.Name, &ap.Note, &ap.Status, &ap.CreatedAt, &at, &ap.DecisionNote, &ap.OrganizationKey}, extra...)
	if err := row.Scan(dest...); err != nil {
		return Application{}, err
	}
	if at.Valid {
		ap.DecidedAt = &at.Int64
	}
	return ap, nil
}

// ApplyForOrganization submits an application. Human session, active account, setting organization_creation = apply.
// One application may wait at a time; after a rejection wait 24 h; at most 3 applications per 30 days.
func (s *Service) ApplyForOrganization(ctx context.Context, a auth.Actor, key, name, note string) (Application, error) {
	if a.Kind != auth.KindHuman {
		return Application{}, ErrForbidden
	}
	if st, err := s.standing(s.db.R, a); err != nil {
		return Application{}, err
	} else if !st.active {
		return Application{}, ErrForbidden
	}
	if s.Policy().OrganizationCreation != "apply" {
		return Application{}, ErrForbidden
	}
	key, name, err := cleanOrg(key, name)
	if err != nil {
		return Application{}, err
	}
	if note = strings.TrimSpace(note); len(note) > 500 {
		return Application{}, invalid("the note is too long (max 500 characters)")
	}
	var out Application
	err = s.tx(func(tx *sql.Tx) error {
		now := s.nowMs()
		var waiting, recent int
		var lastRejected sql.NullInt64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(status = 'pending'), 0), COALESCE(SUM(created_at > ?), 0), MAX(CASE WHEN status = 'rejected' THEN decided_at END)
			FROM organization_applications WHERE applicant_id = ?`, now-applyWindowMs, a.UserID).Scan(&waiting, &recent, &lastRejected); err != nil {
			return err
		}
		switch {
		case waiting > 0:
			return rule("you already have an application waiting for a decision")
		case lastRejected.Valid && now-lastRejected.Int64 < applyCooldownMs:
			return rule("wait 24 hours after a declined application before applying again")
		case recent >= applyMaxInWin:
			return rule("at most %d applications per 30 days", applyMaxInWin)
		}
		res, err := tx.Exec(`INSERT INTO organization_applications (applicant_id, name, key, note, created_at) VALUES (?, ?, ?, ?, ?)`, a.UserID, name, key, note, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		out = Application{ID: id, Key: key, Name: name, Note: note, Status: "pending", CreatedAt: now}
		return s.journal(ctx, tx, a, change{Operation: "org.apply", Target: "application-" + strconv.FormatInt(id, 10), Event: "admin.updated",
			Changes: map[string]any{"key": key, "name": name}})
	})
	return out, err
}

// ListMyApplications returns the caller's own applications, newest first.
func (s *Service) ListMyApplications(ctx context.Context, a auth.Actor) ([]Application, error) {
	if a.Kind != auth.KindHuman || !s.IsActive(a) {
		return nil, ErrForbidden
	}
	rows, err := s.db.R.Query(`SELECT `+applicationCols+` FROM organization_applications a LEFT JOIN organizations o ON o.id = a.organization_id
		WHERE a.applicant_id = ? ORDER BY a.id DESC LIMIT 20`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		ap, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ap)
	}
	return out, rows.Err()
}

// ListApplications is the SUPERADMIN's queue: status pending (default), approved or rejected.
func (s *Service) ListApplications(ctx context.Context, a auth.Actor, status string) ([]AdminApplication, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		return nil, invalid("status must be pending, approved or rejected")
	}
	rows, err := s.db.R.Query(`SELECT `+applicationCols+`, u.email, u.name FROM organization_applications a
		JOIN users u ON u.id = a.applicant_id LEFT JOIN organizations o ON o.id = a.organization_id
		WHERE a.status = ? ORDER BY a.id LIMIT 200`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminApplication{}
	for rows.Next() {
		var aa AdminApplication
		ap, err := scanApplication(rows, &aa.ApplicantEmail, &aa.ApplicantName)
		if err != nil {
			return nil, err
		}
		aa.Application = ap
		out = append(out, aa)
	}
	return out, rows.Err()
}

// DecideApplication approves (optionally under another key) or rejects a waiting application. SUPERADMIN only,
// human session, compare-and-set; approving creates the organization with the applicant as first ADMIN.
func (s *Service) DecideApplication(ctx context.Context, a auth.Actor, id int64, decision, note, key string) error {
	if decision != "approve" && decision != "reject" {
		return invalid("decision must be approve or reject")
	}
	if err := s.adminOnly(a); err != nil {
		return err
	}
	if note = strings.TrimSpace(note); len(note) > 500 {
		return invalid("the note is too long (max 500 characters)")
	}
	return s.tx(func(tx *sql.Tx) error {
		var applicant int64
		var status, appKey, name string
		if err := tx.QueryRow(`SELECT applicant_id, status, key, name FROM organization_applications WHERE id = ?`, id).Scan(&applicant, &status, &appKey, &name); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "pending" {
			return rule("the application was already decided")
		}
		now := s.nowMs()
		var orgID any
		to := "rejected"
		if decision == "approve" {
			if strings.TrimSpace(key) != "" {
				appKey = key
			}
			k, n, err := cleanOrg(appKey, name)
			if err != nil {
				return err
			}
			o, err := s.createOrganizationTx(ctx, tx, a, k, n, applicant)
			if err != nil {
				return err
			}
			orgID, to = o.ID, "approved"
		}
		res, err := tx.Exec(`UPDATE organization_applications SET status = ?, decided_at = ?, decided_by = ?, decision_note = ?, organization_id = ?
			WHERE id = ? AND status = 'pending'`, to, now, a.UserID, note, orgID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return rule("the application changed in the meantime: reload and try again")
		}
		return s.journal(ctx, tx, a, change{Operation: "org.application." + decision, Target: "application-" + strconv.FormatInt(id, 10), Event: "admin.updated",
			Changes: map[string]any{"to": to}}) // never the note
	})
}

// ListAdminOrganizations is the Atlas admin's view of all organizations: name, key, status, ADMIN emails, member count.
func (s *Service) ListAdminOrganizations(ctx context.Context, a auth.Actor) ([]AdminOrganization, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT o.key, o.name, o.status, o.created_at,
		(SELECT COUNT(*) FROM organization_memberships m WHERE m.organization_id = o.id),
		COALESCE((SELECT group_concat(u.email, ',') FROM organization_memberships m JOIN users u ON u.id = m.user_id
			WHERE m.organization_id = o.id AND m.role = 'admin' AND u.status = 'active'), '')
		FROM organizations o ORDER BY o.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminOrganization{}
	for rows.Next() {
		var o AdminOrganization
		var admins string
		if err := rows.Scan(&o.Key, &o.Name, &o.Status, &o.CreatedAt, &o.Members, &admins); err != nil {
			return nil, err
		}
		o.Admins = []string{}
		if admins != "" {
			o.Admins = strings.Split(admins, ",")
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

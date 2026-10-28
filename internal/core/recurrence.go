package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// Recurrences create an item on a schedule (ATL-72). Times are UTC.
// shortcut: UTC only and one item per run even after downtime (missed occurrences are skipped), upgrade when
// per-project time zones or catch-up are needed.

var recurrenceFreqs = []string{"daily", "weekly", "monthly"}

type Recurrence struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	Parent      string   `json:"parent"`
	Freq        string   `json:"freq"`
	Interval    int      `json:"interval"`
	Weekday     *int     `json:"weekday"`   // 0 = Sunday; weekly only
	MonthDay    *int     `json:"month_day"` // 1-31, clamped to the month's length; monthly only
	AtMinute    int      `json:"at_minute"` // minutes after 00:00 UTC
	NextRunAt   int64    `json:"next_run_at"`
	LastRunAt   *int64   `json:"last_run_at"`
	LastError   string   `json:"last_error"`
	Active      bool     `json:"active"`
}

func monthAt(y int, m time.Month, day, minute int) time.Time {
	if last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day(); day > last {
		day = last
	}
	return time.Date(y, m, day, 0, minute, 0, 0, time.UTC)
}

// firstRun is the first occurrence strictly after now.
func (r Recurrence) firstRun(now time.Time) time.Time {
	now = now.UTC()
	t := time.Date(now.Year(), now.Month(), now.Day(), 0, r.AtMinute, 0, 0, time.UTC)
	switch r.Freq {
	case "weekly":
		for int(t.Weekday()) != *r.Weekday || !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
	case "monthly":
		t = monthAt(now.Year(), now.Month(), *r.MonthDay, r.AtMinute)
		if !t.After(now) {
			t = monthAt(now.Year(), now.Month()+1, *r.MonthDay, r.AtMinute)
		}
	default:
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
	}
	return t
}

// following is the occurrence after due that lies in the future; missed ones in between are skipped.
func (r Recurrence) following(due, now time.Time) time.Time {
	for {
		switch r.Freq {
		case "weekly":
			due = due.AddDate(0, 0, 7*r.Interval)
		case "monthly":
			due = monthAt(due.Year(), due.Month()+time.Month(r.Interval), *r.MonthDay, r.AtMinute)
		default:
			due = due.AddDate(0, 0, r.Interval)
		}
		if due.After(now) {
			return due
		}
	}
}

func (r *Recurrence) validate() error {
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" || len(r.Title) > maxTitleLen {
		return invalid("title is required (max %d characters)", maxTitleLen)
	}
	if r.Type == "" {
		r.Type = "task"
	}
	if !slices.Contains(itemTypes, r.Type) {
		return invalid("type must be one of %s", strings.Join(itemTypes, ", "))
	}
	if r.Priority == "" {
		r.Priority = "normal"
	}
	if !slices.Contains(priorities, r.Priority) {
		return invalid("priority must be one of %s", strings.Join(priorities, ", "))
	}
	if !slices.Contains(recurrenceFreqs, r.Freq) {
		return invalid("freq must be one of %s", strings.Join(recurrenceFreqs, ", "))
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 52 {
		return invalid("interval must be between 1 and 52")
	}
	if r.AtMinute < 0 || r.AtMinute > 1439 {
		return invalid("at_minute must be between 0 and 1439")
	}
	if r.Freq == "weekly" && (r.Weekday == nil || *r.Weekday < 0 || *r.Weekday > 6) {
		return invalid("weekly needs weekday 0-6 (0 = Sunday)")
	}
	if r.Freq == "monthly" && (r.MonthDay == nil || *r.MonthDay < 1 || *r.MonthDay > 31) {
		return invalid("monthly needs month_day 1-31")
	}
	if r.Freq != "weekly" {
		r.Weekday = nil
	}
	if r.Freq != "monthly" {
		r.MonthDay = nil
	}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	return nil
}

const recurrenceCols = `id, title, item_type, description, priority, labels, parent, freq, interval_n, weekday, month_day, at_minute, next_run_at, last_run_at, last_error, active`

func scanRecurrence(sc interface{ Scan(...any) error }) (Recurrence, int64, error) {
	var r Recurrence
	var labels string
	var wd, md, last sql.NullInt64
	err := sc.Scan(&r.ID, &r.Title, &r.Type, &r.Description, &r.Priority, &labels, &r.Parent, &r.Freq, &r.Interval, &wd, &md, &r.AtMinute, &r.NextRunAt, &last, &r.LastError, &r.Active)
	if err != nil {
		return r, 0, err
	}
	_ = json.Unmarshal([]byte(labels), &r.Labels)
	if r.Labels == nil {
		r.Labels = []string{}
	}
	if wd.Valid {
		v := int(wd.Int64)
		r.Weekday = &v
	}
	if md.Valid {
		v := int(md.Int64)
		r.MonthDay = &v
	}
	if last.Valid {
		r.LastRunAt = &last.Int64
	}
	return r, 0, nil
}

func (s *Service) ListRecurrences(ctx context.Context, a auth.Actor, key string) ([]Recurrence, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT `+recurrenceCols+` FROM recurrences WHERE project_id = ? ORDER BY id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recurrence{}
	for rows.Next() {
		r, _, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRecurrence needs a project admin. Items are created later as the creating user.
func (s *Service) CreateRecurrence(ctx context.Context, a auth.Actor, key string, r Recurrence) (Recurrence, error) {
	if a.Kind != auth.KindHuman {
		return Recurrence{}, ErrForbidden // the scheduler acts as the creating user, which a token has no stable identity for
	}
	if err := r.validate(); err != nil {
		return Recurrence{}, err
	}
	r.NextRunAt = r.firstRun(s.now()).UnixMilli()
	r.Active = true
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		labels, _ := json.Marshal(r.Labels)
		res, err := tx.Exec(`INSERT INTO recurrences (project_id, title, item_type, description, priority, labels, parent, freq, interval_n, weekday, month_day, at_minute, next_run_at, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, r.Title, r.Type, r.Description, r.Priority, string(labels), r.Parent, r.Freq, r.Interval, r.Weekday, r.MonthDay, r.AtMinute, r.NextRunAt, a.UserID, s.nowMs())
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "recurrence.create", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": r.ID, "title": r.Title, "freq": r.Freq}})
	})
	return r, err
}

// SetRecurrenceActive pauses or resumes a recurrence. Resuming schedules the next occurrence from now.
func (s *Service) SetRecurrenceActive(ctx context.Context, a auth.Actor, key string, id int64, active bool) (Recurrence, error) {
	var out Recurrence
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		r, _, err := scanRecurrence(tx.QueryRow(`SELECT `+recurrenceCols+` FROM recurrences WHERE id = ? AND project_id = ?`, id, p.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if active && !r.Active {
			r.NextRunAt = r.firstRun(s.now()).UnixMilli()
		}
		r.Active = active
		if _, err := tx.Exec(`UPDATE recurrences SET active = ?, next_run_at = ? WHERE id = ?`, active, r.NextRunAt, id); err != nil {
			return err
		}
		out = r
		return s.journal(ctx, tx, a, change{Operation: "recurrence.update", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id, "active": active}})
	})
	return out, err
}

func (s *Service) DeleteRecurrence(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM recurrences WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "recurrence.delete", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id}})
	})
}

// RunDueRecurrences creates the item of every due recurrence and returns how many it created. Each run claims
// its occurrence by advancing next_run_at in the transaction that creates the item: a crash creates nothing
// and a concurrent runner finds the claim taken.
func (s *Service) RunDueRecurrences(ctx context.Context) int {
	now := s.now()
	rows, err := s.db.R.Query(`SELECT r.`+strings.ReplaceAll(recurrenceCols, ", ", ", r.")+`, p.key, r.created_by, u.is_admin
		FROM recurrences r JOIN projects p ON p.id = r.project_id JOIN users u ON u.id = r.created_by
		WHERE r.active = 1 AND r.next_run_at <= ? AND p.archived_at IS NULL ORDER BY r.next_run_at LIMIT 50`, now.UnixMilli())
	if err != nil {
		slog.Error("recurrences: list due", "error", err)
		return 0
	}
	type due struct {
		r       Recurrence
		project string
		actor   auth.Actor
	}
	var list []due
	for rows.Next() {
		var d due
		var labels string
		var wd, md, last sql.NullInt64
		var user int64
		var admin bool
		r := &d.r
		if err := rows.Scan(&r.ID, &r.Title, &r.Type, &r.Description, &r.Priority, &labels, &r.Parent, &r.Freq, &r.Interval, &wd, &md, &r.AtMinute, &r.NextRunAt, &last, &r.LastError, &r.Active, &d.project, &user, &admin); err != nil {
			slog.Error("recurrences: scan", "error", err)
			continue
		}
		_ = json.Unmarshal([]byte(labels), &r.Labels)
		if wd.Valid {
			v := int(wd.Int64)
			r.Weekday = &v
		}
		if md.Valid {
			v := int(md.Int64)
			r.MonthDay = &v
		}
		d.actor = auth.Actor{Kind: auth.KindHuman, UserID: user, IsAdmin: admin}
		list = append(list, d)
	}
	rows.Close()

	created := 0
	for _, d := range list {
		next := d.r.following(time.UnixMilli(d.r.NextRunAt).UTC(), now).UnixMilli()
		claim := func(bs *Service, errText string, ranAt any) error {
			return bs.tx(func(tx *sql.Tx) error {
				res, err := tx.Exec(`UPDATE recurrences SET next_run_at = ?, last_run_at = COALESCE(?, last_run_at), last_error = ? WHERE id = ? AND next_run_at = ?`,
					next, ranAt, errText, d.r.ID, d.r.NextRunAt)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n == 0 {
					return errClaimed
				}
				return nil
			})
		}
		_, err := s.Idempotent(ctx, d.actor, "", nil, nil, func(ctx context.Context, bs *Service) error {
			if err := claim(bs, "", now.UnixMilli()); err != nil {
				return err
			}
			_, err := bs.createItem(ctx, d.actor, CreateItem{Project: d.project, Type: d.r.Type, Title: d.r.Title, Description: d.r.Description,
				Priority: d.r.Priority, Parent: d.r.Parent, Labels: d.r.Labels}, nil)
			return err
		})
		switch {
		case err == nil:
			created++
		case errors.Is(err, errClaimed):
		default:
			slog.Error("recurrences: create item", "recurrence", d.r.ID, "project", d.project, "error", err)
			// Skip this occurrence and record why, so one broken recurrence does not retry every tick.
			if cerr := claim(s, err.Error(), nil); cerr != nil && !errors.Is(cerr, errClaimed) {
				slog.Error("recurrences: record failure", "recurrence", d.r.ID, "error", cerr)
			}
		}
	}
	return created
}

var errClaimed = errors.New("occurrence already claimed")

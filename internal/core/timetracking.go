package core

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

const (
	maxEntryMinutes = 1440 // one day per booking
	maxTimeNote     = 500
	summaryTop      = 100
	exportMax       = 10000
)

// TimeEntry is minutes booked on an item by one person on one day (ADR-0014).
type TimeEntry struct {
	ID        int64  `json:"id"`
	Item      string `json:"item"`
	Title     string `json:"title,omitempty"`
	UserID    int64  `json:"user_id"`
	User      string `json:"user"`
	Status    string `json:"status"` // the item's status when the time was booked
	Minutes   int    `json:"minutes"`
	SpentOn   string `json:"spent_on"` // YYYY-MM-DD
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
}

var durationRe = regexp.MustCompile(`^(?:(\d+(?:[.,]\d+)?)\s*h)?\s*(?:(\d+(?:[.,]\d+)?)\s*m?)?$`)

// ParseDuration reads "90", "45m", "1h", "1.5h" or "1h30m" and rounds to whole minutes.
func ParseDuration(v string) (int, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	m := durationRe.FindStringSubmatch(v)
	if v == "" || m == nil {
		return 0, invalid("duration must look like 90, 45m, 1.5h or 1h30m")
	}
	num := func(s string) float64 {
		f, _ := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
		return f
	}
	minutes := int(math.Round(num(m[1])*60 + num(m[2])))
	if minutes < 1 || minutes > maxEntryMinutes {
		return 0, invalid("duration must be between 1 minute and 24 hours")
	}
	return minutes, nil
}

func (s *Service) spentOn(v string) (string, error) {
	if v == "" {
		return s.now().UTC().Format(time.DateOnly), nil
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return "", invalid("date must be YYYY-MM-DD")
	}
	if d.After(s.now().UTC().AddDate(0, 0, 1)) { // a day of slack for time zones
		return "", invalid("time cannot be booked in the future")
	}
	return v, nil
}

func noteText(v string) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) > maxTimeNote {
		return "", invalid("note is at most %d characters", maxTimeNote)
	}
	return v, nil
}

// SetTimeTracking turns time tracking on or off for a project (needs the workflow action). Existing bookings are kept.
func (s *Service) SetTimeTracking(ctx context.Context, a auth.Actor, key string, on bool) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actWorkflow)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE projects SET time_tracking = ? WHERE id = ?`, on, p.ID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "workflow.time_tracking", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"time_tracking": on}})
	})
}

// LogTime books time on an item for the actor. date is YYYY-MM-DD, empty meaning today.
func (s *Service) LogTime(ctx context.Context, a auth.Actor, itemKey, duration, date, note string) (TimeEntry, error) {
	minutes, err := ParseDuration(duration)
	if err != nil {
		return TimeEntry{}, err
	}
	if date, err = s.spentOn(date); err != nil {
		return TimeEntry{}, err
	}
	if note, err = noteText(note); err != nil {
		return TimeEntry{}, err
	}
	pk, err := projectOfKey(itemKey)
	if err != nil {
		return TimeEntry{}, err
	}
	var e TimeEntry
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, pk, actTimeLog)
		if err != nil {
			return err
		}
		if !p.TimeTracking {
			return rule("time tracking is off for this project")
		}
		var itemID, statusID int64
		if err := tx.QueryRow(`SELECT id, status_id FROM work_items WHERE key = ?`, itemKey).Scan(&itemID, &statusID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		now := s.nowMs()
		res, err := tx.Exec(`INSERT INTO time_entries (project_id, item_id, user_id, status_id, minutes, spent_on, note, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, itemID, a.UserID, statusID, minutes, date, note, now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if e, err = timeEntry(tx, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "time.log", Target: itemKey, Event: "item.updated", Project: pk, ItemKey: itemKey,
			Changes: map[string]any{"entry": id, "minutes": minutes, "spent_on": date}})
	})
	return e, err
}

const timeSelect = `SELECT t.id, i.key, i.title, t.user_id, u.name, COALESCE(st.name, ''), t.minutes, t.spent_on, t.note, t.created_at
	FROM time_entries t JOIN work_items i ON i.id = t.item_id JOIN users u ON u.id = t.user_id
	LEFT JOIN project_statuses st ON st.id = t.status_id `

func scanEntries(rows *sql.Rows) ([]TimeEntry, error) {
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		var e TimeEntry
		if err := rows.Scan(&e.ID, &e.Item, &e.Title, &e.UserID, &e.User, &e.Status, &e.Minutes, &e.SpentOn, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func timeEntry(q queryer, id int64) (TimeEntry, error) {
	rows, err := q.Query(timeSelect+`WHERE t.id = ?`, id)
	if err != nil {
		return TimeEntry{}, err
	}
	list, err := scanEntries(rows)
	if err != nil || len(list) == 0 {
		return TimeEntry{}, errors.Join(err, ErrNotFound)
	}
	return list[0], nil
}

// ListItemTime returns the bookings on one item, newest day first. Time tracking being off hides nothing:
// bookings stay readable.
func (s *Service) ListItemTime(ctx context.Context, a auth.Actor, itemKey string) ([]TimeEntry, error) {
	pk, err := projectOfKey(itemKey)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.access(s.db.R, a, pk, actView); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(timeSelect+`WHERE i.key = ? ORDER BY t.spent_on DESC, t.id DESC`, itemKey)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// ownEntry loads the entry's owner and checks the actor may change it: the owner, or a project admin.
func ownEntry(tx *sql.Tx, projectID int64, id, userID int64, role string) (item string, err error) {
	var owner int64
	err = tx.QueryRow(`SELECT t.user_id, i.key FROM time_entries t JOIN work_items i ON i.id = t.item_id
		WHERE t.id = ? AND t.project_id = ?`, id, projectID).Scan(&owner, &item)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if owner != userID && role != "admin" {
		return "", ErrForbidden
	}
	return item, nil
}

// UpdateTime edits an own booking (any booking for a project admin). patch: duration, date, note.
func (s *Service) UpdateTime(ctx context.Context, a auth.Actor, key string, id int64, patch map[string]any) (TimeEntry, error) {
	if len(patch) == 0 {
		return TimeEntry{}, invalid("no fields to update")
	}
	var out TimeEntry
	err := s.tx(func(tx *sql.Tx) error {
		p, role, err := s.access(tx, a, key, actTimeLog)
		if err != nil {
			return err
		}
		item, err := ownEntry(tx, p.ID, id, a.UserID, role)
		if err != nil {
			return err
		}
		for k, v := range patch {
			str, isStr := v.(string)
			var q string
			var arg any
			switch k {
			case "duration":
				if n, ok := asInt(v); ok { // a bare number means minutes
					str, isStr = strconv.Itoa(n), true
				}
				if !isStr {
					return invalid("duration must be a string or number of minutes")
				}
				if arg, err = ParseDuration(str); err != nil {
					return err
				}
				q = `UPDATE time_entries SET minutes = ?, updated_at = ? WHERE id = ?`
			case "date":
				if !isStr || str == "" {
					return invalid("date must be YYYY-MM-DD")
				}
				if arg, err = s.spentOn(str); err != nil {
					return err
				}
				q = `UPDATE time_entries SET spent_on = ?, updated_at = ? WHERE id = ?`
			case "note":
				if !isStr {
					return invalid("note must be a string")
				}
				if arg, err = noteText(str); err != nil {
					return err
				}
				q = `UPDATE time_entries SET note = ?, updated_at = ? WHERE id = ?`
			default:
				return invalid("unknown field %q", k)
			}
			if _, err := tx.Exec(q, arg, s.nowMs(), id); err != nil {
				return err
			}
		}
		if out, err = timeEntry(tx, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "time.update", Target: item, Event: "item.updated", Project: key, ItemKey: item,
			Changes: map[string]any{"entry": id, "fields": patch}})
	})
	return out, err
}

// DeleteTime removes an own booking (any booking for a project admin).
func (s *Service) DeleteTime(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, role, err := s.access(tx, a, key, actTimeLog)
		if err != nil {
			return err
		}
		item, err := ownEntry(tx, p.ID, id, a.UserID, role)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM time_entries WHERE id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "time.delete", Target: item, Event: "item.updated", Project: key, ItemKey: item,
			Changes: map[string]any{"entry": id}})
	})
}

// TimeFilter narrows a summary or export. From and To are inclusive YYYY-MM-DD dates.
type TimeFilter struct {
	From, To string
	UserID   *int64
	Item     string
	Period   string // day, week or month; default week
}

func (f TimeFilter) where(projectID int64) (string, []any, error) {
	w, args := ` WHERE t.project_id = ?`, []any{projectID}
	for _, d := range []struct{ v, op string }{{f.From, ">="}, {f.To, "<="}} {
		if d.v == "" {
			continue
		}
		if _, err := time.Parse(time.DateOnly, d.v); err != nil {
			return "", nil, invalid("dates must be YYYY-MM-DD")
		}
		w += ` AND t.spent_on ` + d.op + ` ?`
		args = append(args, d.v)
	}
	if f.UserID != nil {
		w += ` AND t.user_id = ?`
		args = append(args, *f.UserID)
	}
	if f.Item != "" {
		w += ` AND t.item_id = (SELECT id FROM work_items WHERE key = ?)`
		args = append(args, f.Item)
	}
	return w, args, nil
}

// Bucket is minutes under one label (a ticket, person, status or period).
type Bucket struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Minutes int    `json:"minutes"`
}

// TimeSummary totals a project's bookings per ticket (top 100), person, status and period.
type TimeSummary struct {
	TotalMinutes int      `json:"total_minutes"`
	Entries      int      `json:"entries"`
	Items        []Bucket `json:"items"`
	People       []Bucket `json:"people"`
	Statuses     []Bucket `json:"statuses"`
	Periods      []Bucket `json:"periods"`
}

func (s *Service) TimeSummary(ctx context.Context, a auth.Actor, key string, f TimeFilter) (TimeSummary, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return TimeSummary{}, err
	}
	period := map[string]string{"day": `t.spent_on`, "": `strftime('%Y-W%W', t.spent_on)`, "week": `strftime('%Y-W%W', t.spent_on)`,
		"month": `substr(t.spent_on, 1, 7)`}[f.Period]
	if period == "" {
		return TimeSummary{}, invalid("period must be day, week or month")
	}
	where, args, err := f.where(p.ID)
	if err != nil {
		return TimeSummary{}, err
	}
	from := ` FROM time_entries t` + where
	var out TimeSummary
	if err := s.db.R.QueryRow(`SELECT COALESCE(SUM(t.minutes), 0), COUNT(*)`+from, args...).Scan(&out.TotalMinutes, &out.Entries); err != nil {
		return out, err
	}
	// Aggregate on the raw column first and join names for the (at most limit) groups after, so the cost
	// follows the number of bookings once rather than once per joined table.
	group := func(dim, join, keyExpr, labelExpr, order string, limit int) ([]Bucket, error) {
		rows, err := s.db.R.Query(`SELECT `+keyExpr+`, `+labelExpr+`, x.m FROM (SELECT `+dim+` AS d, SUM(t.minutes) AS m`+from+
			` GROUP BY d ORDER BY `+order+` LIMIT ?) x `+join+` ORDER BY `+order,
			append(append([]any{}, args...), limit)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list := []Bucket{}
		for rows.Next() {
			var b Bucket
			if err := rows.Scan(&b.Key, &b.Label, &b.Minutes); err != nil {
				return nil, err
			}
			list = append(list, b)
		}
		return list, rows.Err()
	}
	if out.Items, err = group(`t.item_id`, `JOIN work_items i ON i.id = x.d`, `i.key`, `i.title`, `m DESC, d`, summaryTop); err != nil {
		return out, err
	}
	if out.People, err = group(`t.user_id`, `JOIN users u ON u.id = x.d`, `CAST(u.id AS TEXT)`, `u.name`, `m DESC, d`, summaryTop); err != nil {
		return out, err
	}
	if out.Statuses, err = group(`COALESCE(t.status_id, 0)`, `LEFT JOIN project_statuses st ON st.id = x.d`, `COALESCE(st.name, '')`, `''`, `m DESC, d`, summaryTop); err != nil {
		return out, err
	}
	out.Periods, err = group(period, ``, `x.d`, `''`, `d DESC`, 200)
	return out, err
}

// ExportTime lists the bookings behind a summary, oldest first, for CSV export (needs the export action).
func (s *Service) ExportTime(ctx context.Context, a auth.Actor, key string, f TimeFilter) ([]TimeEntry, error) {
	p, _, err := s.access(s.db.R, a, key, actExport)
	if err != nil {
		return nil, err
	}
	where, args, err := f.where(p.ID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(timeSelect+where+` ORDER BY t.spent_on, t.id LIMIT ?`, append(args, exportMax)...)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

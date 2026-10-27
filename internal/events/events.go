// Package events serves the change journal to clients: a catch-up read (delta sync) and a hub that
// wakes live subscribers. The persistent change_events table is authoritative; the hub carries no data.
package events

import (
	"database/sql"
	"sync"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
)

type Event struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"`
	Project string `json:"project"`
	Key     string `json:"key,omitempty"`
}

type Batch struct {
	Events []Event `json:"events"`
	// Seq is the position to continue from: the last journal entry looked at, including entries the actor may not see.
	Seq int64 `json:"seq"`
	// Resync means the client's cursor is unusable (journal pruned, or the database was replaced) and it must reload.
	Resync bool `json:"resync_required"`
	More   bool `json:"more"`
}

type Feed struct {
	db   *db.DB
	core *core.Service
	hub  *Hub
}

func New(d *db.DB, c *core.Service) *Feed { return &Feed{db: d, core: c, hub: NewHub()} }

func (f *Feed) Hub() *Hub { return f.hub }

func (f *Feed) MaxSeq() (int64, error) {
	var n int64
	err := f.db.R.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM change_events`).Scan(&n)
	return n, err
}

// Generation identifies the database lifetime; it changes after a restore so clients reload everything.
func (f *Feed) Generation() (string, error) {
	var g string
	err := f.db.R.QueryRow(`SELECT value FROM meta WHERE key = 'generation'`).Scan(&g)
	return g, err
}

// Since returns the events after a cursor that the actor may see.
func (f *Feed) Since(a auth.Actor, since int64, limit int) (Batch, error) {
	b := Batch{Events: []Event{}, Seq: since}
	var minSeq, maxSeq int64
	if err := f.db.R.QueryRow(`SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0) FROM change_events`).Scan(&minSeq, &maxSeq); err != nil {
		return b, err
	}
	// Ahead of the journal: the database was replaced. Behind its oldest entry: the history was pruned.
	if since > maxSeq || (since > 0 && minSeq > since+1) {
		b.Resync, b.Seq = true, maxSeq
		return b, nil
	}

	readable, err := f.core.ReadableProjectKeys(a)
	if err != nil {
		return b, err
	}
	rows, err := f.db.R.Query(`SELECT seq, type, project, item_key, target_user FROM change_events WHERE seq > ? ORDER BY seq LIMIT ?`, since, limit+1)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var e Event
		var target sql.NullInt64
		if err := rows.Scan(&e.Seq, &e.Type, &e.Project, &e.Key, &target); err != nil {
			return b, err
		}
		if n++; n > limit {
			b.More = true
			break
		}
		b.Seq = e.Seq
		// Notifications are signals for one person only.
		if readable[e.Project] && (!target.Valid || target.Int64 == a.UserID) {
			b.Events = append(b.Events, e)
		}
	}
	return b, rows.Err()
}

// Prune drops journal entries older than maxAge but always keeps the newest keepLast, so a short
// outage never forces every client to resync.
func (f *Feed) Prune(maxAge time.Duration, keepLast int64) (int64, error) {
	res, err := f.db.W.Exec(`DELETE FROM change_events WHERE ts < ? AND seq <= (SELECT COALESCE(MAX(seq), 0) FROM change_events) - ?`,
		time.Now().Add(-maxAge).UnixMilli(), keepLast)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Hub wakes subscribers when something was committed. Wake-ups coalesce: a subscriber that is
// busy gets one pending signal, and then reads everything new from the journal.
type Hub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan struct{}]struct{}{}} }

func (h *Hub) Notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *Hub) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Package logbuf keeps the most recent log records in memory so administrators can read them in the app
// (ADR-0016). It wraps the normal handler: stdout stays the durable log, this is a bounded window onto it.
package logbuf

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// Size is how many records are kept. Memory is bounded: records beyond it replace the oldest.
const Size = 1000

// Entry is one log record; Attrs holds its attributes rendered as text.
type Entry struct {
	TS    int64             `json:"ts"`
	Level string            `json:"level"`
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Buffer is a ring of the last Size entries, shared by every handler derived from the same Handler.
type Buffer struct {
	mu   sync.Mutex
	ring []Entry
	next int
}

func New() *Buffer { return &Buffer{} }

func (b *Buffer) add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.ring) < Size {
		b.ring = append(b.ring, e)
		return
	}
	b.ring[b.next] = e
	b.next = (b.next + 1) % Size
}

// Filter selects entries: at least MinLevel, and Query (case-insensitive) in the message or an attribute.
type Filter struct {
	MinLevel slog.Level
	Query    string
	Limit    int
}

// List returns matching entries, newest first.
func (b *Buffer) List(f Filter) []Entry {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := strings.ToLower(f.Query)
	b.mu.Lock()
	all := slices.Clone(b.ring)
	next := b.next
	b.mu.Unlock()
	out := []Entry{}
	for i := range all {
		e := all[(next+len(all)-1-i)%len(all)]
		var lv slog.Level
		_ = lv.UnmarshalText([]byte(e.Level))
		if lv < f.MinLevel || (q != "" && !e.matches(q)) {
			continue
		}
		if out = append(out, e); len(out) == f.Limit {
			break
		}
	}
	return out
}

func (e Entry) matches(q string) bool {
	if strings.Contains(strings.ToLower(e.Msg), q) {
		return true
	}
	for k, v := range e.Attrs {
		if strings.Contains(strings.ToLower(k+"="+v), q) {
			return true
		}
	}
	return false
}

// Handler forwards every record to next and keeps a copy in the buffer.
func (b *Buffer) Handler(next slog.Handler) slog.Handler { return &handler{next: next, buf: b} }

type handler struct {
	next  slog.Handler
	buf   *Buffer
	attrs []slog.Attr
	group string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	e := Entry{TS: r.Time.UnixMilli(), Level: r.Level.String(), Msg: r.Message, Attrs: map[string]string{}}
	if r.Time.IsZero() {
		e.TS = time.Now().UnixMilli()
	}
	for _, a := range h.attrs {
		e.Attrs[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		e.Attrs[h.group+a.Key] = a.Value.String()
		return true
	})
	h.buf.add(e)
	return h.next.Handle(ctx, r)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.next = h.next.WithAttrs(as)
	for _, a := range as {
		c.attrs = append(slices.Clone(c.attrs), slog.Attr{Key: h.group + a.Key, Value: a.Value})
	}
	return &c
}

func (h *handler) WithGroup(name string) slog.Handler {
	c := *h
	c.next = h.next.WithGroup(name)
	c.group = h.group + name + "."
	return &c
}

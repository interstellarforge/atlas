package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	keepAliveEvery = 20 * time.Second
	// A stream is closed after this long and the client reconnects with Last-Event-ID. That makes every
	// connection re-check credentials, so a revoked token or ended session stops receiving events.
	maxStreamAge = 10 * time.Minute
	batchSize    = 500
)

// events streams the change journal as Server-Sent Events. Reconnects resume from Last-Event-ID; a
// cursor the journal can no longer serve produces a resync_required event.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}
	a := actor(r)

	last, hasLast := int64(0), false
	for _, v := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("since")} {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && v != "" {
			last, hasLast = n, true
			break
		}
	}
	gen, err := s.feed.Generation()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	head, err := s.feed.MaxSeq()
	if err != nil {
		s.internal(w, r, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, id int64, data any) bool {
		b, _ := json.Marshal(data)
		var err error
		if id > 0 {
			_, err = fmt.Fprintf(w, "id: %d\n", id)
		}
		if err == nil && event != "" {
			_, err = fmt.Fprintf(w, "event: %s\n", event)
		}
		if err == nil {
			_, err = fmt.Fprintf(w, "data: %s\n\n", b)
		}
		flusher.Flush()
		return err == nil
	}

	cursor := head
	if hasLast {
		cursor = last
	}
	wake, cancel := s.feed.Hub().Subscribe()
	defer cancel()
	if !send("hello", 0, map[string]any{"generation": gen, "seq": head}) {
		return
	}

	// drain delivers everything after the cursor; false means the client went away or must resync.
	drain := func() bool {
		for {
			b, err := s.feed.Since(a, cursor, batchSize)
			if err != nil {
				return false
			}
			if b.Resync {
				send("resync_required", 0, map[string]any{"seq": b.Seq})
				cursor = b.Seq
				return true
			}
			for _, e := range b.Events {
				if !send("", e.Seq, e) {
					return false
				}
			}
			cursor = b.Seq
			if !b.More {
				return true
			}
		}
	}

	if !drain() {
		return
	}
	keepAlive := time.NewTicker(keepAliveEvery)
	defer keepAlive.Stop()
	deadline := time.NewTimer(maxStreamAge)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-wake:
			if !drain() {
				return
			}
		}
	}
}

// changes is the polling form of the same journal: GET /changes?since=<seq>.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil || since < 0 {
		writeError(w, http.StatusBadRequest, "invalid", "since must be a non-negative number")
		return
	}
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, batchSize)
	}
	b, err := s.feed.Since(actor(r), since, limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

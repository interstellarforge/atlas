package notifications

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/kees/atlas/internal/db"
)

const (
	maxAttempts = 5
	batchSize   = 20
	keepDays    = 14
	pollEvery   = 5 * time.Second
	sendTimeout = 30 * time.Second
)

// backoff[i] is the wait after the (i+1)th failed attempt.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// Worker sends the mail_outbox rows that are due. Sending happens here, never inside a mutation: a failed
// or slow mail server cannot break or delay a mutation, and a restart loses nothing.
type Worker struct {
	DB     *db.DB
	Sender Sender
	// Load returns the effective settings; ok is false when email is not configured.
	Load func() (s Settings, ok bool, err error)
	Now  func() time.Time
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Run processes the outbox until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		w.ProcessOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type outboxRow struct {
	id       int64
	attempts int
	msg      Message
}

// ProcessOnce sends the due messages and returns how many were delivered.
func (w *Worker) ProcessOnce(ctx context.Context) int {
	settings, ok, err := w.Load()
	if err != nil {
		slog.Error("mail: load settings", "error", redact(err.Error(), settings))
		return 0
	}
	now := w.now().UnixMilli()
	if ok {
		sent := 0
		for _, r := range w.due(now) {
			sctx, cancel := context.WithTimeout(ctx, sendTimeout)
			err := w.Sender.Send(sctx, settings, r.msg)
			cancel()
			if err == nil {
				w.exec(`UPDATE mail_outbox SET sent_at = ?, body = '', last_error = '' WHERE id = ?`, now, r.id)
				sent++
				continue
			}
			w.fail(r, now, redact(err.Error(), settings))
		}
		if sent > 0 {
			slog.Info("mail sent", "count", sent)
		}
		w.prune(now)
		return sent
	}
	w.prune(now)
	return 0
}

func (w *Worker) due(now int64) []outboxRow {
	rows, err := w.DB.R.Query(`SELECT id, attempts, to_addr, subject, body FROM mail_outbox
		WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= ? ORDER BY id LIMIT ?`, now, batchSize)
	if err != nil {
		slog.Error("mail: read outbox", "error", err)
		return nil
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.attempts, &r.msg.To, &r.msg.Subject, &r.msg.Body); err != nil {
			slog.Error("mail: read outbox", "error", err)
			return out
		}
		out = append(out, r)
	}
	return out
}

func (w *Worker) fail(r outboxRow, now int64, msg string) {
	attempts := r.attempts + 1
	if len(msg) > 300 {
		msg = msg[:300]
	}
	if attempts >= maxAttempts {
		slog.Warn("mail given up", "id", r.id, "attempts", attempts, "error", msg)
		w.exec(`UPDATE mail_outbox SET attempts = ?, failed_at = ?, body = '', last_error = ? WHERE id = ?`, attempts, now, msg, r.id)
		return
	}
	slog.Warn("mail failed, will retry", "id", r.id, "attempts", attempts, "error", msg)
	w.exec(`UPDATE mail_outbox SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		attempts, now+backoff[min(attempts-1, len(backoff)-1)].Milliseconds(), msg, r.id)
}

func (w *Worker) prune(now int64) {
	w.exec(`DELETE FROM mail_outbox WHERE COALESCE(sent_at, failed_at) < ?`, now-keepDays*24*time.Hour.Milliseconds())
}

func (w *Worker) exec(q string, args ...any) {
	if _, err := w.DB.W.Exec(q, args...); err != nil && err != sql.ErrNoRows {
		slog.Error("mail: update outbox", "error", err)
	}
}

// redact removes the password from text that may end up in a log or an API answer.
func redact(msg string, s Settings) string {
	if s.Password != "" {
		msg = strings.ReplaceAll(msg, s.Password, "***")
	}
	return msg
}

// Redact is the exported form for callers that show send errors to an admin.
func Redact(msg string, s Settings) string { return redact(msg, s) }

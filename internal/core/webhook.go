package core

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// Webhooks send change events of a project to an external URL (ATL-77, ADR-0022). A delivery is queued in
// webhook_outbox by the transaction of the change; deliver sends it later, outside any transaction, with
// retries and an HMAC-SHA256 signature.

const (
	maxWebhooksPerProject = 10
	maxWebhookURL         = 500
	maxWebhookPayload     = 64 << 10
	webhookBatch          = 20
	webhookKeepDays       = 14
)

// webhookBackoff[i] is the wait after the (i+1)th failed attempt; the last failure gives up.
var webhookBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}

// WebhookPoster sends one signed delivery; an error means it must be retried.
type WebhookPoster interface {
	Post(ctx context.Context, url string, header map[string]string, body []byte) error
}

// SetWebhooks enables delivery. allowHTTP also accepts plain http URLs (private networks).
func (s *Service) SetWebhooks(p WebhookPoster, allowHTTP bool) { s.hooks, s.hooksHTTP = p, allowHTTP }

type Webhook struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"` // empty = every event
	Active    bool     `json:"active"`
	CreatedAt int64    `json:"created_at"`
	// Secret is only returned by CreateWebhook; keep it, it cannot be read again.
	Secret string `json:"secret,omitempty"`
}

func (s *Service) validWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxWebhookURL || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && s.hooksHTTP)) {
		return invalid("url must be a plain https URL of at most %d characters", maxWebhookURL)
	}
	return nil
}

func (s *Service) ListWebhooks(ctx context.Context, a auth.Actor, key string) ([]Webhook, error) {
	p, _, err := s.access(s.db.R, a, key, actProjectUpdate)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, url, events, active, created_at FROM webhooks WHERE project_id = ? ORDER BY id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		var ev string
		if err := rows.Scan(&w.ID, &w.URL, &ev, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.Events = splitEvents(ev)
		out = append(out, w)
	}
	return out, rows.Err()
}

func splitEvents(v string) []string {
	if v == "" {
		return []string{}
	}
	return strings.Split(v, ",")
}

// CreateWebhook needs a project admin. The signing secret is generated here and shown once.
func (s *Service) CreateWebhook(ctx context.Context, a auth.Actor, key, rawURL string, events []string) (Webhook, error) {
	if err := s.validWebhookURL(rawURL); err != nil {
		return Webhook{}, err
	}
	for _, e := range events {
		if e == "" || strings.ContainsAny(e, ", ") || len(e) > 50 {
			return Webhook{}, invalid("invalid event type %q", e)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Webhook{}, err
	}
	w := Webhook{URL: rawURL, Events: events, Active: true, CreatedAt: s.nowMs(), Secret: hex.EncodeToString(raw)}
	if w.Events == nil {
		w.Events = []string{}
	}
	enc, err := s.encryptSecret(w.Secret)
	if err != nil {
		return Webhook{}, err
	}
	err = s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM webhooks WHERE project_id = ?`, p.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxWebhooksPerProject {
			return rule("a project has at most %d webhooks", maxWebhooksPerProject)
		}
		res, err := tx.Exec(`INSERT INTO webhooks (project_id, url, secret_enc, events, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, rawURL, enc, strings.Join(w.Events, ","), a.String(), w.CreatedAt)
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "webhook.create", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": w.ID, "url": rawURL}})
	})
	return w, err
}

func (s *Service) SetWebhookActive(ctx context.Context, a auth.Actor, key string, id int64, active bool) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE webhooks SET active = ? WHERE id = ? AND project_id = ?`, active, id, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "webhook.update", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id, "active": active}})
	})
}

func (s *Service) DeleteWebhook(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM webhooks WHERE id = ? AND project_id = ?`, id, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "webhook.delete", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"id": id}})
	})
}

// enqueueWebhooks queues the change for every active webhook of its project that listens to the event.
// It runs inside the transaction of the change, so the delivery exists exactly when the change committed.
func (s *Service) enqueueWebhooks(tx *sql.Tx, a auth.Actor, c change, now int64) error {
	if c.Project == "" || s.hooks == nil {
		return nil
	}
	rows, err := tx.Query(`SELECT w.id, w.events FROM webhooks w JOIN projects p ON p.id = w.project_id WHERE p.key = ? AND w.active = 1`, c.Project)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var ev string
		if err := rows.Scan(&id, &ev); err != nil {
			rows.Close()
			return err
		}
		if ev == "" || slicesContains(splitEvents(ev), c.Event) {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"event": c.Event, "operation": c.Operation, "project": c.Project, "item": c.ItemKey,
		"actor": a.String(), "ts": now, "changes": c.Changes})
	if err != nil {
		return err
	}
	if len(payload) > maxWebhookPayload { // the receiver can read the full change through the API
		payload, _ = json.Marshal(map[string]any{"event": c.Event, "operation": c.Operation, "project": c.Project, "item": c.ItemKey,
			"actor": a.String(), "ts": now, "truncated": true})
	}
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT INTO webhook_outbox (webhook_id, event, payload, next_attempt_at, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, c.Event, string(payload), now, now); err != nil {
			return err
		}
	}
	return nil
}

func slicesContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// signWebhook is the value of X-Atlas-Signature: "sha256=" + hex HMAC of "<timestamp>.<body>".
func signWebhook(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// DeliverWebhooks sends the due deliveries and returns how many succeeded.
func (s *Service) DeliverWebhooks(ctx context.Context) int {
	if s.hooks == nil {
		return 0
	}
	now := s.nowMs()
	rows, err := s.db.R.Query(`SELECT o.id, o.attempts, o.event, o.payload, w.url, w.secret_enc FROM webhook_outbox o JOIN webhooks w ON w.id = o.webhook_id
		WHERE w.active = 1 AND o.delivered_at IS NULL AND o.failed_at IS NULL AND o.next_attempt_at <= ? ORDER BY o.id LIMIT ?`, now, webhookBatch)
	if err != nil {
		slog.Error("webhooks: read outbox", "error", err)
		return 0
	}
	type job struct {
		id       int64
		attempts int
		event    string
		payload  string
		url      string
		enc      []byte
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.attempts, &j.event, &j.payload, &j.url, &j.enc); err != nil {
			slog.Error("webhooks: read outbox", "error", err)
			break
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	sent := 0
	for _, j := range jobs {
		err := s.deliverOne(ctx, j.id, j.event, j.url, j.enc, []byte(j.payload))
		if err == nil {
			s.hookExec(`UPDATE webhook_outbox SET delivered_at = ?, last_error = '' WHERE id = ?`, now, j.id)
			sent++
			continue
		}
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		attempts := j.attempts + 1
		if attempts > len(webhookBackoff) {
			slog.Warn("webhook given up", "delivery", j.id, "attempts", attempts, "error", msg)
			s.hookExec(`UPDATE webhook_outbox SET attempts = ?, failed_at = ?, last_error = ? WHERE id = ?`, attempts, now, msg, j.id)
			continue
		}
		slog.Warn("webhook failed, will retry", "delivery", j.id, "attempts", attempts, "error", msg)
		s.hookExec(`UPDATE webhook_outbox SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ?`,
			attempts, now+webhookBackoff[attempts-1].Milliseconds(), msg, j.id)
	}
	s.hookExec(`DELETE FROM webhook_outbox WHERE COALESCE(delivered_at, failed_at) < ?`, now-webhookKeepDays*24*time.Hour.Milliseconds())
	return sent
}

func (s *Service) deliverOne(ctx context.Context, id int64, event, url string, enc, body []byte) error {
	secret, err := s.decryptSecret(enc)
	if err != nil {
		return errors.New("webhook secret cannot be read")
	}
	ts := strconv.FormatInt(s.now().Unix(), 10)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.hooks.Post(ctx, url, map[string]string{
		"X-Atlas-Event": event, "X-Atlas-Delivery": strconv.FormatInt(id, 10), "X-Atlas-Timestamp": ts,
		"X-Atlas-Signature": signWebhook(secret, ts, body),
	}, body)
}

func (s *Service) hookExec(q string, args ...any) {
	if _, err := s.db.W.Exec(q, args...); err != nil {
		slog.Error("webhooks: update outbox", "error", err)
	}
}

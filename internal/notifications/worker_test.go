package notifications_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/notifications"
	"github.com/kees/atlas/migrations"
)

type fakeSender struct {
	mu   sync.Mutex
	err  error
	sent []notifications.Message
}

func (f *fakeSender) Send(_ context.Context, _ notifications.Settings, m notifications.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func setup(t *testing.T) (*db.DB, *notifications.Worker, *fakeSender, *time.Time) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	f := &fakeSender{}
	w := &notifications.Worker{DB: d, Sender: f, Now: func() time.Time { return clock },
		Load: func() (notifications.Settings, bool, error) {
			return notifications.Settings{Host: "h", Port: 25, Security: "none", FromAddr: "a@x.io", Password: "s3cret-pw"}, true, nil
		}}
	return d, w, f, &clock
}

func enqueue(t *testing.T, d *db.DB, body string) {
	t.Helper()
	if _, err := d.W.Exec(`INSERT INTO mail_outbox (to_addr, subject, body, next_attempt_at, created_at) VALUES ('u@x.io', 's', ?, 0, 0)`, body); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerSendsAndBlanksBody(t *testing.T) {
	d, w, f, _ := setup(t)
	enqueue(t, d, "invite link with a code")
	if n := w.ProcessOnce(context.Background()); n != 1 || len(f.sent) != 1 {
		t.Fatalf("sent %d / %d", n, len(f.sent))
	}
	var body string
	var sentAt *int64
	if err := d.R.QueryRow(`SELECT body, sent_at FROM mail_outbox`).Scan(&body, &sentAt); err != nil || body != "" || sentAt == nil {
		t.Fatalf("after send: %q %v %v", body, sentAt, err)
	}
	if n := w.ProcessOnce(context.Background()); n != 0 {
		t.Fatal("a sent mail was sent again")
	}
}

func TestWorkerRetriesThenGivesUpWithoutLeakingThePassword(t *testing.T) {
	d, w, f, clock := setup(t)
	f.err = errors.New("535 login failed for password s3cret-pw")
	enqueue(t, d, "body")

	for attempt := 1; attempt <= 5; attempt++ {
		w.ProcessOnce(context.Background())
		var attempts int
		var failedAt *int64
		var lastErr, body string
		if err := d.R.QueryRow(`SELECT attempts, failed_at, last_error, body FROM mail_outbox`).Scan(&attempts, &failedAt, &lastErr, &body); err != nil {
			t.Fatal(err)
		}
		if attempts != attempt {
			t.Fatalf("attempt %d: counted %d", attempt, attempts)
		}
		if strings.Contains(lastErr, "s3cret-pw") || !strings.Contains(lastErr, "***") {
			t.Fatalf("password in stored error: %q", lastErr)
		}
		if attempt < 5 && (failedAt != nil || body == "") {
			t.Fatalf("attempt %d: gave up too early", attempt)
		}
		if attempt == 5 && (failedAt == nil || body != "") {
			t.Fatalf("not given up after 5 attempts (failed_at=%v, body kept=%v)", failedAt, body != "")
		}
		// Not due again before the backoff has passed.
		if attempt < 5 {
			if n := w.ProcessOnce(context.Background()); n != 0 {
				t.Fatal("retried before the backoff")
			}
			*clock = clock.Add(2 * time.Hour)
		}
	}
	// Once given up it stays down, even when the server recovers.
	f.err = nil
	*clock = clock.Add(24 * time.Hour)
	if n := w.ProcessOnce(context.Background()); n != 0 || len(f.sent) != 0 {
		t.Fatal("a failed mail was sent after giving up")
	}
}

func TestSMTPRefusesHeaderInjection(t *testing.T) {
	st := notifications.Settings{Host: "127.0.0.1", Port: 1, Security: "none", FromAddr: "a@x.io"}
	err := notifications.SMTP{}.Send(context.Background(), st, notifications.Message{To: "u@x.io", Subject: "hi\r\nBcc: evil@x.io", Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("subject with a line break: %v", err)
	}
}

func TestRenderTemplates(t *testing.T) {
	sub, body, err := notifications.Render("xx", "invite", map[string]any{"Inviter": "Ann", "Code": "C0DE", "Days": 7, "Link": ""})
	if err != nil || !strings.Contains(sub, "invited") || !strings.Contains(body, "C0DE") || strings.Contains(body, "<no value>") {
		t.Fatalf("%q %q %v", sub, body, err)
	}
}

package core_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
)

type post struct {
	url    string
	header map[string]string
	body   []byte
}

type fakePoster struct {
	got  []post
	fail bool
}

func (f *fakePoster) Post(_ context.Context, url string, header map[string]string, body []byte) error {
	if f.fail {
		return errors.New("boom")
	}
	f.got = append(f.got, post{url, header, body})
	return nil
}

func TestWebhookDeliversSignedWithRetry(t *testing.T) {
	e := setup(t)
	e.project("HK")
	ctx := context.Background()
	e.svc.SetMail(core.MailConfig{KeyPath: filepath.Join(t.TempDir(), "k")})
	f := &fakePoster{}
	e.svc.SetWebhooks(f, false)
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now })

	if _, err := e.svc.CreateWebhook(ctx, e.admin, "HK", "http://plain.example/x", nil); err == nil {
		t.Fatal("plain http accepted")
	}
	if _, err := e.svc.CreateWebhook(ctx, e.bob, "HK", "https://hook.example/x", nil); err == nil {
		t.Fatal("non-member created a webhook")
	}
	hook, err := e.svc.CreateWebhook(ctx, e.admin, "HK", "https://hook.example/x", []string{"item.created"})
	if err != nil || hook.Secret == "" {
		t.Fatalf("create: %v %q", err, hook.Secret)
	}
	// project.updated (the create above) is not subscribed; item.created is.
	if _, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "HK", Type: "task", Title: "first"}); err != nil {
		t.Fatal(err)
	}

	f.fail = true
	if n := e.svc.DeliverWebhooks(ctx); n != 0 {
		t.Fatalf("delivered %d while failing", n)
	}
	f.fail = false
	if n := e.svc.DeliverWebhooks(ctx); n != 0 {
		t.Fatalf("retried before the backoff passed (%d)", n)
	}
	now = now.Add(2 * time.Minute)
	if n := e.svc.DeliverWebhooks(ctx); n != 1 || len(f.got) != 1 {
		t.Fatalf("delivered %d, posts %d, want 1", n, len(f.got))
	}
	p := f.got[0]
	m := hmac.New(sha256.New, []byte(hook.Secret))
	m.Write([]byte(p.header["X-Atlas-Timestamp"] + "."))
	m.Write(p.body)
	if want := "sha256=" + hex.EncodeToString(m.Sum(nil)); p.header["X-Atlas-Signature"] != want {
		t.Fatalf("signature %q, want %q", p.header["X-Atlas-Signature"], want)
	}
	if n := e.svc.DeliverWebhooks(ctx); n != 0 {
		t.Fatal("delivered twice")
	}
}

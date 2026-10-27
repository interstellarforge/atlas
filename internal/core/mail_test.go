package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/notifications"
)

type fakeMail struct {
	mu   sync.Mutex
	err  error
	sent []notifications.Message
	last notifications.Settings
}

func (f *fakeMail) Send(_ context.Context, s notifications.Settings, m notifications.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = s
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func mailEnv(t *testing.T) (*env, *fakeMail, string) {
	e := setup(t)
	f := &fakeMail{}
	key := filepath.Join(t.TempDir(), "mail.key")
	e.svc.SetMail(core.MailConfig{KeyPath: key, PublicURL: "https://atlas.test", Sender: f})
	return e, f, key
}

func pw(s string) *string { return &s }

func saveMail(t *testing.T, e *env, password *string) core.MailSettings {
	t.Helper()
	m, err := e.svc.SetMailSettings(ctx, e.admin, core.MailInput{Host: "smtp.x.io", Port: 587, Security: "starttls", Username: "u",
		Password: password, FromAddr: "atlas@x.io", FromName: "Atlas"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMailSettingsKeepThePasswordSecret(t *testing.T) {
	e, f, key := mailEnv(t)
	const secret = "hunter2-smtp-password"
	m := saveMail(t, e, pw(secret))
	if !m.PasswordSet || !m.Configured || m.Source != "database" {
		t.Fatalf("view: %+v", m)
	}

	// The API shape, the stored value and the audit log never hold the password.
	out, _ := json.Marshal(m)
	var enc []byte
	if err := e.db.R.QueryRow(`SELECT password_enc FROM mail_settings`).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	var audit string
	if err := e.db.R.QueryRow(`SELECT group_concat(changes) FROM audit_entries WHERE operation = 'mail.settings'`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	for name, hay := range map[string]string{"view": string(out), "password_enc": string(enc), "audit": audit} {
		if strings.Contains(hay, secret) {
			t.Fatalf("password found in %s", name)
		}
	}
	if st, err := os.Stat(key); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}

	// Omitting the password keeps it; the sender receives the decrypted one.
	saveMail(t, e, nil)
	if err := e.svc.SendTestMail(ctx, e.admin); err != nil || f.last.Password != secret || len(f.sent) != 1 || f.sent[0].To != "admin@x.io" {
		t.Fatalf("test mail: %v %q %+v", err, f.last.Password, f.sent)
	}
	// An empty password removes it; clearing the settings drops back to "not configured".
	if m = saveMail(t, e, pw("")); m.PasswordSet {
		t.Fatal("password not removed")
	}
	if m, _ = e.svc.ClearMailSettings(ctx, e.admin); m.Configured || m.Source != "none" {
		t.Fatalf("after clear: %+v", m)
	}
	if err := e.svc.SendTestMail(ctx, e.admin); !isRule(err) {
		t.Fatalf("test mail without settings: %v", err)
	}
}

func TestMailSettingsPermissionsAndValidation(t *testing.T) {
	e, _, _ := mailEnv(t)
	in := core.MailInput{Host: "smtp.x.io", Port: 587, Security: "starttls", FromAddr: "atlas@x.io"}
	token := auth.Actor{Kind: auth.KindToken, UserID: e.admin.UserID, IsAdmin: true, Scopes: []string{"write"}}
	for name, a := range map[string]auth.Actor{"member": e.alice, "token": token} {
		if _, err := e.svc.SetMailSettings(ctx, a, in); !errors.Is(err, core.ErrForbidden) {
			t.Fatalf("%s saving: %v", name, err)
		}
		if _, err := e.svc.GetMailSettings(ctx, a); !errors.Is(err, core.ErrForbidden) {
			t.Fatalf("%s reading: %v", name, err)
		}
		if err := e.svc.SendTestMail(ctx, a); !errors.Is(err, core.ErrForbidden) {
			t.Fatalf("%s test mail: %v", name, err)
		}
	}
	for _, bad := range []core.MailInput{
		{Host: "", Port: 587, Security: "starttls", FromAddr: "a@x.io"},
		{Host: "h", Port: 0, Security: "starttls", FromAddr: "a@x.io"},
		{Host: "h", Port: 587, Security: "ssl", FromAddr: "a@x.io"},
		{Host: "h", Port: 587, Security: "tls", FromAddr: "not-an-address"},
	} {
		if _, err := e.svc.SetMailSettings(ctx, e.admin, bad); !isInvalid(err) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

func TestTestMailErrorHidesThePassword(t *testing.T) {
	e, f, _ := mailEnv(t)
	saveMail(t, e, pw("topsecret"))
	f.err = errors.New("535 bad login topsecret")
	err := e.svc.SendTestMail(ctx, e.admin)
	if !isRule(err) || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaks the password or is not a rule error: %v", err)
	}
}

func TestEnvironmentDefaultsAndDatabaseOverride(t *testing.T) {
	e := setup(t)
	e.svc.SetMail(core.MailConfig{KeyPath: filepath.Join(t.TempDir(), "k"), Sender: &fakeMail{},
		Env: notifications.Settings{Host: "env.x.io", Port: 25, Security: "none", FromAddr: "env@x.io", Password: "envpw"}})
	m, _ := e.svc.GetMailSettings(ctx, e.admin)
	if m.Source != "environment" || m.Host != "env.x.io" || !m.PasswordSet {
		t.Fatalf("env: %+v", m)
	}
	saveMail(t, e, pw("dbpw"))
	if m, _ = e.svc.GetMailSettings(ctx, e.admin); m.Source != "database" || m.Host != "smtp.x.io" {
		t.Fatalf("override: %+v", m)
	}
	if m, _ = e.svc.ClearMailSettings(ctx, e.admin); m.Source != "environment" {
		t.Fatalf("back to env: %+v", m)
	}
}

func outbox(e *env) (n int, subject, body string) {
	e.t.Helper()
	n = e.count(`SELECT COUNT(*) FROM mail_outbox`)
	_ = e.db.R.QueryRow(`SELECT COALESCE(MAX(subject), ''), COALESCE(MAX(body), '') FROM mail_outbox`).Scan(&subject, &body)
	return
}

func TestInviteMail(t *testing.T) {
	e, _, _ := mailEnv(t)
	// Not configured: behaviour is unchanged, nothing queued.
	if _, _, err := e.svc.CreateInvite(ctx, e.admin, "new@x.io", false, 7); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := outbox(e); n != 0 {
		t.Fatalf("%d mails queued without configuration", n)
	}
	saveMail(t, e, nil)
	_, code, err := e.svc.CreateInvite(ctx, e.admin, "other@x.io", false, 7)
	if err != nil {
		t.Fatal(err)
	}
	n, subject, body := outbox(e)
	if n != 1 || !strings.Contains(subject, "invited") || !strings.Contains(body, "https://atlas.test/#/register?invite="+code) {
		t.Fatalf("queued: %d %q %q", n, subject, body)
	}
	// An invite without an address is not mailed.
	if _, _, err := e.svc.CreateInvite(ctx, e.admin, "", false, 7); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := outbox(e); n != 1 {
		t.Fatalf("%d mails", n)
	}
}

func TestNotificationMailFollowsThePreference(t *testing.T) {
	e, _, _ := mailEnv(t)
	e.project("EXO")
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.aliceID, "member"); err != nil {
		t.Fatal(err)
	}
	saveMail(t, e, nil)
	assign := func(title string) {
		it := e.item(e.admin, core.CreateItem{Project: "EXO", Type: "task", Title: title})
		if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"assignee": e.aliceID}); err != nil {
			t.Fatal(err)
		}
	}
	assign("one") // preference off (default): a notification, no mail
	if e.count(`SELECT COUNT(*) FROM notifications`) != 1 {
		t.Fatal("expected one in-app notification")
	}
	if n, _, _ := outbox(e); n != 0 {
		t.Fatalf("%d mails with the preference off", n)
	}
	if _, err := e.svc.SetPreferences(ctx, e.alice, core.Preferences{Theme: "system", DefaultView: "board", EmailNotifications: true}); err != nil {
		t.Fatal(err)
	}
	assign("two")
	n, subject, body := outbox(e)
	if n != 1 || !strings.Contains(subject, "assigned EXO-2") || !strings.Contains(body, "https://atlas.test/#/p/EXO/EXO-2") {
		t.Fatalf("queued: %d %q %q", n, subject, body)
	}
	var to string
	_ = e.db.R.QueryRow(`SELECT to_addr FROM mail_outbox`).Scan(&to)
	if to != "alice@x.io" {
		t.Fatalf("mailed %s", to)
	}
}

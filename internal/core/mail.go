package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/notifications"
)

// MailConfig wires outgoing email (ADR-0012). Without it, or without settings, nothing is ever mailed.
type MailConfig struct {
	// Env are the ATLAS_SMTP_* defaults; a row in mail_settings overrides them as a whole.
	Env notifications.Settings
	// KeyPath is the file with the key that encrypts the stored SMTP password; created on first use.
	KeyPath string
	// PublicURL (no trailing slash) is used to put links in mails; without it mails carry codes and keys only.
	PublicURL string
	Sender    notifications.Sender
}

func (s *Service) SetMail(c MailConfig) { s.mail = c }

// MailSettings is the admin view of the effective settings. The password is never part of it.
type MailSettings struct {
	Configured  bool   `json:"configured"`
	Source      string `json:"source"` // database, environment or none
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Security    string `json:"security"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	FromAddr    string `json:"from_addr"`
	FromName    string `json:"from_name"`
}

func viewOf(st notifications.Settings, source string) MailSettings {
	return MailSettings{Configured: st.Configured(), Source: source, Host: st.Host, Port: st.Port, Security: st.Security,
		Username: st.Username, PasswordSet: st.Password != "", FromAddr: st.FromAddr, FromName: st.FromName}
}

// MailEffective returns the settings mail is sent with, and where they come from.
func (s *Service) MailEffective(q queryer) (notifications.Settings, string, error) {
	var st notifications.Settings
	var enc []byte
	err := q.QueryRow(`SELECT host, port, security, username, password_enc, from_addr, from_name FROM mail_settings WHERE id = 1`).
		Scan(&st.Host, &st.Port, &st.Security, &st.Username, &enc, &st.FromAddr, &st.FromName)
	if errors.Is(err, sql.ErrNoRows) {
		if s.mail.Env.Configured() {
			return s.mail.Env, "environment", nil
		}
		return notifications.Settings{}, "none", nil
	}
	if err != nil {
		return st, "", err
	}
	if len(enc) > 0 {
		pw, err := s.decryptSecret(enc)
		if err != nil {
			// e.g. a restore without mail.key: treat as no password and let the admin enter it again.
			pw = ""
		}
		st.Password = pw
	}
	return st, "database", nil
}

// MailWorkerSettings adapts MailEffective for the background worker.
func (s *Service) MailWorkerSettings() (notifications.Settings, bool, error) {
	st, _, err := s.MailEffective(s.db.R)
	return st, err == nil && st.Configured(), err
}

func (s *Service) GetMailSettings(ctx context.Context, a auth.Actor) (MailSettings, error) {
	if err := adminOnly(a); err != nil {
		return MailSettings{}, err
	}
	st, source, err := s.MailEffective(s.db.R)
	return viewOf(st, source), err
}

// MailInput is what an admin submits. Password nil keeps the stored one; an empty string removes it.
type MailInput struct {
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Security string  `json:"security"`
	Username string  `json:"username"`
	Password *string `json:"password"`
	FromAddr string  `json:"from_addr"`
	FromName string  `json:"from_name"`
}

func (s *Service) SetMailSettings(ctx context.Context, a auth.Actor, in MailInput) (MailSettings, error) {
	if err := adminOnly(a); err != nil {
		return MailSettings{}, err
	}
	st := notifications.Settings{Host: strings.TrimSpace(in.Host), Port: in.Port, Security: in.Security, Username: strings.TrimSpace(in.Username),
		FromAddr: strings.TrimSpace(in.FromAddr), FromName: strings.TrimSpace(in.FromName)}
	if err := st.Validate(); err != nil {
		return MailSettings{}, invalid("%v", err)
	}
	err := s.tx(func(tx *sql.Tx) error {
		var enc []byte
		if in.Password == nil {
			_ = tx.QueryRow(`SELECT password_enc FROM mail_settings WHERE id = 1`).Scan(&enc)
		} else if *in.Password != "" {
			var err error
			if enc, err = s.encryptSecret(*in.Password); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO mail_settings (id, host, port, security, username, password_enc, from_addr, from_name, updated_at)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET host = excluded.host, port = excluded.port, security = excluded.security, username = excluded.username,
			password_enc = excluded.password_enc, from_addr = excluded.from_addr, from_name = excluded.from_name, updated_at = excluded.updated_at`,
			st.Host, st.Port, st.Security, st.Username, enc, st.FromAddr, st.FromName, s.nowMs()); err != nil {
			return err
		}
		// The audit entry never holds the password, only whether it changed.
		return s.journal(ctx, tx, a, change{Operation: "mail.settings", Target: "mail", Event: "admin.updated", Changes: map[string]any{
			"host": st.Host, "port": st.Port, "security": st.Security, "username": st.Username, "from_addr": st.FromAddr,
			"password_changed": in.Password != nil}})
	})
	if err != nil {
		return MailSettings{}, err
	}
	return s.GetMailSettings(ctx, a)
}

// ClearMailSettings drops the stored settings, so the environment defaults (if any) apply again.
func (s *Service) ClearMailSettings(ctx context.Context, a auth.Actor) (MailSettings, error) {
	if err := adminOnly(a); err != nil {
		return MailSettings{}, err
	}
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM mail_settings WHERE id = 1`); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "mail.settings.clear", Target: "mail", Event: "admin.updated", Changes: map[string]any{}})
	})
	if err != nil {
		return MailSettings{}, err
	}
	return s.GetMailSettings(ctx, a)
}

// SendTestMail sends straight away (not through the outbox) to the admin's own address, so a wrong setting
// is reported right where it was entered.
func (s *Service) SendTestMail(ctx context.Context, a auth.Actor) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	st, _, err := s.MailEffective(s.db.R)
	if err != nil {
		return err
	}
	if !st.Configured() || s.mail.Sender == nil {
		return rule("email is not configured")
	}
	var to string
	if err := s.db.R.QueryRow(`SELECT email FROM users WHERE id = ?`, a.UserID).Scan(&to); err != nil {
		return err
	}
	subject, body, err := notifications.Render("en", "test", nil)
	if err != nil {
		return err
	}
	if err := s.mail.Sender.Send(ctx, st, notifications.Message{To: to, Subject: subject, Body: body}); err != nil {
		return rule("could not send the test email: %s", notifications.Redact(err.Error(), st))
	}
	return nil
}

// enqueueMail adds a message to the outbox inside the caller's transaction. It does nothing when email is
// not configured, so the behaviour without email is exactly what it was.
func (s *Service) enqueueMail(tx *sql.Tx, to, name string, data any) error {
	st, _, err := s.MailEffective(tx)
	if err != nil || !st.Configured() || to == "" {
		return err
	}
	subject, body, err := notifications.Render("en", name, data)
	if err != nil {
		return err
	}
	now := s.nowMs()
	_, err = tx.Exec(`INSERT INTO mail_outbox (to_addr, subject, body, next_attempt_at, created_at) VALUES (?, ?, ?, ?, ?)`, to, subject, body, now, now)
	return err
}

func (s *Service) link(path string) string {
	if s.mail.PublicURL == "" {
		return ""
	}
	return s.mail.PublicURL + "/#" + path
}

// ---- secret at rest ----

func (s *Service) secretKey(create bool) ([]byte, error) {
	if s.mail.KeyPath == "" {
		return nil, errors.New("no key file configured")
	}
	key, err := os.ReadFile(s.mail.KeyPath)
	if err == nil && len(key) == 32 {
		return key, nil
	}
	if !create {
		return nil, errors.New("mail key missing")
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.mail.KeyPath, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Service) encryptSecret(plain string) ([]byte, error) {
	key, err := s.secretKey(true)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (s *Service) decryptSecret(enc []byte) (string, error) {
	key, err := s.secretKey(false)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(enc) < gcm.NonceSize() {
		return "", fmt.Errorf("stored secret is too short")
	}
	plain, err := gcm.Open(nil, enc[:gcm.NonceSize()], enc[gcm.NonceSize():], nil)
	return string(plain), err
}

var notifyPhrases = map[string]string{
	NotifyAssigned:    "assigned %s to you",
	NotifyMention:     "mentioned you on %s",
	NotifyBlockerDone: "finished the item that blocked %s",
	NotifyFollowed:    "changed %s, which you follow",
}

// mailNotification mails a notification to a user who switched that on. A failure here fails the mutation
// only if the outbox itself cannot be written; delivery problems never reach it.
func (s *Service) mailNotification(tx *sql.Tx, a auth.Actor, n Notice, project, key string) error {
	p, err := loadPrefs(tx, n.User)
	if err != nil || !p.EmailNotifications {
		return nil
	}
	var email string
	if err := tx.QueryRow(`SELECT email FROM users WHERE id = ? AND deactivated_at IS NULL`, n.User).Scan(&email); err != nil {
		return nil
	}
	phrase, ok := notifyPhrases[n.Type]
	if !ok {
		phrase = "updated %s"
	}
	who := a.String()
	if a.Kind == auth.KindHuman {
		_ = tx.QueryRow(`SELECT name FROM users WHERE id = ?`, a.UserID).Scan(&who)
	}
	return s.enqueueMail(tx, email, "notification", map[string]any{
		"Summary": who + " " + fmt.Sprintf(phrase, key), "Link": s.link("/p/" + project + "/" + key)})
}

// Package notifications sends email: a Sender (SMTP) and a Worker that drains the mail_outbox table.
package notifications

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Settings is a complete SMTP configuration. Password is only ever held in memory; never log or marshal it.
type Settings struct {
	Host     string
	Port     int
	Security string // tls (implicit TLS), starttls or none
	Username string
	Password string
	FromAddr string
	FromName string
}

// Configured reports whether there is enough to send mail.
func (s Settings) Configured() bool { return s.Host != "" && s.Port > 0 && s.FromAddr != "" }

// Validate checks the fields an admin can set.
func (s Settings) Validate() error {
	if strings.TrimSpace(s.Host) == "" || strings.ContainsAny(s.Host, " /@\r\n") {
		return errors.New("host is required")
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("port must be 1-65535")
	}
	if s.Security != "tls" && s.Security != "starttls" && s.Security != "none" {
		return errors.New("security must be tls, starttls or none")
	}
	if _, err := mail.ParseAddress(s.FromAddr); err != nil || strings.ContainsAny(s.FromAddr, "\r\n") {
		return errors.New("from address is not a valid email address")
	}
	if strings.ContainsAny(s.FromName+s.Username, "\r\n") {
		return errors.New("from name and username cannot contain line breaks")
	}
	return nil
}

type Message struct{ To, Subject, Body string }

// Sender delivers one message. The SMTP implementation is the only one; tests use a fake.
type Sender interface {
	Send(ctx context.Context, s Settings, m Message) error
}

type SMTP struct{}

const dialTimeout = 15 * time.Second

func (SMTP) Send(ctx context.Context, s Settings, m Message) error {
	if !s.Configured() {
		return errors.New("email is not configured")
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil || strings.ContainsAny(m.To+m.Subject, "\r\n") {
		return errors.New("invalid recipient or subject")
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	d := net.Dialer{Timeout: dialTimeout}
	var conn net.Conn
	if s.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(2 * dialTimeout))
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()
	if s.Security == "starttls" {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if s.Username != "" {
		// net/smtp refuses PLAIN over an unencrypted connection to a non-local host, which is what we want.
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp login: %w", err)
		}
	}
	from := mail.Address{Name: s.FromName, Address: s.FromAddr}
	if err := c.Mail(s.FromAddr); err != nil {
		return fmt.Errorf("smtp sender: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(buildMessage(from, *to, m)); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	return c.Quit()
}

func buildMessage(from, to mail.Address, m Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	const sep = "atlas-alt"
	crlf := func(v string) string { return strings.ReplaceAll(strings.ReplaceAll(v, "\r\n", "\n"), "\n", "\r\n") }
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=" + sep + "\r\n\r\n")
	b.WriteString("--" + sep + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + crlf(m.Body) + "\r\n")
	if h := HTML(m.Body); h != "" {
		b.WriteString("--" + sep + "\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + crlf(h) + "\r\n")
	}
	b.WriteString("--" + sep + "--\r\n")
	return []byte(b.String())
}

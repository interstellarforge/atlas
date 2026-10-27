package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/notifications"
)

type Config struct {
	Addr          string
	DataDir       string
	LogLevel      slog.Level
	Synchronous   string
	CookieSecure  bool
	AdminEmail    string
	AdminPassword string
	// EventRetention is how long change events are kept for reconnecting clients.
	EventRetention time.Duration
	// Upload limits: bytes per file and megapixels per image.
	UploadMaxBytes      int64
	UploadMaxMegapixels int64
	// MCPWritesPerHour is the mutation quota per MCP token.
	MCPWritesPerHour int
	// SMTP are the defaults for outgoing email; settings saved in the admin screen override them (ADR-0012).
	SMTP notifications.Settings
	// RepoAllowPrivate lets Atlas fetch READMEs from servers on private addresses (self-hosted git on the LAN).
	RepoAllowPrivate bool
	// PublicURL is the address users reach Atlas at, for links in emails. Optional.
	PublicURL string
}

func Load() (Config, error) {
	c := Config{
		Addr:          env("ATLAS_ADDR", ":8080"),
		DataDir:       env("ATLAS_DATA_DIR", "./data"),
		Synchronous:   strings.ToUpper(env("ATLAS_SYNCHRONOUS", "FULL")),
		CookieSecure:  env("ATLAS_COOKIE_SECURE", "true") != "false",
		AdminEmail:    os.Getenv("ATLAS_ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ATLAS_ADMIN_PASSWORD"),
	}
	if c.Synchronous != "FULL" && c.Synchronous != "NORMAL" {
		return c, fmt.Errorf("ATLAS_SYNCHRONOUS must be FULL or NORMAL, got %q", c.Synchronous)
	}
	days, err := strconv.Atoi(env("ATLAS_EVENT_RETENTION_DAYS", "7"))
	if err != nil || days < 1 {
		return c, fmt.Errorf("ATLAS_EVENT_RETENTION_DAYS must be a positive number of days")
	}
	c.EventRetention = time.Duration(days) * 24 * time.Hour
	mb, err := strconv.ParseInt(env("ATLAS_UPLOAD_MAX_MB", "10"), 10, 64)
	if err != nil || mb < 1 {
		return c, fmt.Errorf("ATLAS_UPLOAD_MAX_MB must be a positive number")
	}
	mp, err := strconv.ParseInt(env("ATLAS_UPLOAD_MAX_MEGAPIXELS", "20"), 10, 64)
	if err != nil || mp < 1 {
		return c, fmt.Errorf("ATLAS_UPLOAD_MAX_MEGAPIXELS must be a positive number")
	}
	c.UploadMaxBytes, c.UploadMaxMegapixels = mb<<20, mp*1_000_000
	q, err := strconv.Atoi(env("ATLAS_MCP_WRITES_PER_HOUR", "500"))
	if err != nil || q < 1 {
		return c, fmt.Errorf("ATLAS_MCP_WRITES_PER_HOUR must be a positive number")
	}
	c.MCPWritesPerHour = q
	c.RepoAllowPrivate = os.Getenv("ATLAS_REPO_ALLOW_PRIVATE") == "true"
	c.PublicURL = strings.TrimRight(os.Getenv("ATLAS_PUBLIC_URL"), "/")
	port, err := strconv.Atoi(env("ATLAS_SMTP_PORT", "587"))
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("ATLAS_SMTP_PORT must be a port number")
	}
	c.SMTP = notifications.Settings{Host: os.Getenv("ATLAS_SMTP_HOST"), Port: port, Security: env("ATLAS_SMTP_SECURITY", "starttls"),
		Username: os.Getenv("ATLAS_SMTP_USER"), Password: os.Getenv("ATLAS_SMTP_PASSWORD"),
		FromAddr: os.Getenv("ATLAS_SMTP_FROM"), FromName: env("ATLAS_SMTP_FROM_NAME", "Atlas")}
	if c.SMTP.Host != "" {
		if err := c.SMTP.Validate(); err != nil {
			return c, fmt.Errorf("ATLAS_SMTP_*: %w", err)
		}
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("ATLAS_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("ATLAS_LOG_LEVEL: %w", err)
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

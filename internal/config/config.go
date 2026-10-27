package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
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

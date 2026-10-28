package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kees/atlas/internal/api"
	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/config"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/events"
	"github.com/kees/atlas/internal/logbuf"
	"github.com/kees/atlas/internal/mcp"
	"github.com/kees/atlas/internal/media"
	"github.com/kees/atlas/internal/notifications"
	"github.com/kees/atlas/internal/repo"
	"github.com/kees/atlas/internal/scheduler"
	"github.com/kees/atlas/internal/update"
	"github.com/kees/atlas/migrations"
	"github.com/kees/atlas/web"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3"; "dev" never reports an update.
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// pruneJournal trims old change events at startup and then daily; the newest entries are always kept.
func pruneJournal(ctx context.Context, feed *events.Feed, retention time.Duration) {
	const keepLast = 1000
	for {
		if n, err := feed.Prune(retention, keepLast); err != nil {
			slog.Error("prune change events", "error", err)
		} else if n > 0 {
			slog.Info("pruned change events", "deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logs := logbuf.New()
	slog.SetDefault(slog.New(logs.Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))))

	if len(os.Args) == 3 && os.Args[1] == "restore" {
		// Offline by design: the database must not be open while it is replaced.
		m, err := backup.Restore(os.Args[2], cfg.DataDir)
		if err != nil {
			return err
		}
		slog.Info("restored", "from", os.Args[2], "files", len(m.Files), "generation", m.Generation)
		return nil
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "uploads"), 0o750); err != nil {
		return err
	}
	database, err := db.Open(filepath.Join(cfg.DataDir, "atlas.db"), db.Options{Synchronous: cfg.Synchronous})
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.Migrate(migrations.FS, filepath.Join(cfg.DataDir, "backups")); err != nil {
		return err
	}
	if err := database.QuickCheck(); err != nil {
		return err
	}

	authSvc := auth.NewService(database)
	if err := authSvc.PurgeExpiredSessions(); err != nil {
		return err
	}
	created, err := authSvc.EnsureAdmin(cfg.AdminEmail, cfg.AdminPassword)
	if err != nil {
		return err
	}
	if created {
		slog.Info("admin user created", "email", cfg.AdminEmail)
	}

	svc := core.New(database, authSvc)
	uploads := media.NewStore(filepath.Join(cfg.DataDir, "uploads"), media.Limits{MaxBytes: cfg.UploadMaxBytes, MaxPixels: cfg.UploadMaxMegapixels})
	svc.SetMedia(uploads)
	svc.SetRepoFetcher(repo.NewFetcher(cfg.RepoAllowPrivate))
	svc.SetWebhooks(repo.NewFetcher(cfg.RepoAllowPrivate), cfg.RepoAllowPrivate)
	svc.SetMail(core.MailConfig{Env: cfg.SMTP, KeyPath: filepath.Join(cfg.DataDir, "mail.key"), PublicURL: cfg.PublicURL, Sender: notifications.SMTP{}})
	svc.ApplyPolicy()
	if len(os.Args) > 1 {
		return runCommand(svc, database, cfg.DataDir, os.Args[1:])
	}
	feed := events.New(database, svc)
	svc.SetNotifier(feed.Hub().Notify)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go pruneJournal(ctx, feed, cfg.EventRetention)
	go scheduler.Every(ctx, time.Minute, func(ctx context.Context) { svc.RunDueRecurrences(ctx) })
	go scheduler.Every(ctx, 5*time.Second, func(ctx context.Context) { svc.DeliverWebhooks(ctx) })
	go (&notifications.Worker{DB: database, Sender: notifications.SMTP{}, Load: svc.MailWorkerSettings}).Run(ctx)

	var updates *update.Checker
	if cfg.UpdateCheck {
		updates = &update.Checker{Current: version, URL: update.LatestURL, Client: &http.Client{}}
	}
	mcpMetrics := mcp.NewMetrics()
	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.New(api.Deps{DB: database, Auth: authSvc, Core: svc, Feed: feed, Media: uploads, MaxUpload: cfg.UploadMaxBytes, CookieSecure: cfg.CookieSecure, TrustedProxies: cfg.TrustedProxies,
			SPA: web.Dist(), MCP: mcp.Handler(svc, feed, mcp.Options{WritesPerHour: cfg.MCPWritesPerHour, Metrics: mcpMetrics, Label: tokenLabel(database)}), MCPStats: mcpMetrics, Logs: logs, Update: updates,
			Instance: map[string]any{"upload_max_mb": cfg.UploadMaxBytes >> 20, "upload_max_megapixels": cfg.UploadMaxMegapixels / 1_000_000,
				"mcp_writes_per_hour": cfg.MCPWritesPerHour, "event_retention_days": int(cfg.EventRetention.Hours() / 24), "cookie_secure": cfg.CookieSecure,
				"synchronous": cfg.Synchronous}}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // headers and body; slow authenticated senders cannot hold a connection open

		MaxHeaderBytes: 64 << 10,
		IdleTimeout:    120 * time.Second,
		// Open event streams watch their request context; cancelling it on SIGTERM lets Shutdown finish promptly.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "synchronous", cfg.Synchronous)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// tokenLabel names an MCP actor for the admin connection list: "token name (owner)".
func tokenLabel(d *db.DB) func(auth.Actor) string {
	return func(a auth.Actor) string {
		var name, owner string
		if err := d.R.QueryRow(`SELECT t.name, u.name FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.id = ?`, a.TokenID).Scan(&name, &owner); err != nil {
			return ""
		}
		return name + " (" + owner + ")"
	}
}

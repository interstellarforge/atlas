package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kees/atlas/internal/api"
	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/config"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/mcp"
	"github.com/kees/atlas/migrations"
	"github.com/kees/atlas/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

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
	if len(os.Args) > 1 {
		return runCommand(svc, os.Args[1:])
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(database, authSvc, svc, cfg.CookieSecure, web.Dist(), mcp.Handler(svc)).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

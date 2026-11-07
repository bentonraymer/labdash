package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bentonraymer/labdash/internal/alerts"
	"github.com/bentonraymer/labdash/internal/api"
	"github.com/bentonraymer/labdash/internal/auth"
	"github.com/bentonraymer/labdash/internal/config"
	"github.com/bentonraymer/labdash/internal/db"
	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/icons"
	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/pushover"
	"github.com/bentonraymer/labdash/internal/secrets"
	"github.com/bentonraymer/labdash/web"
)

// version is set at build time with -ldflags "-X main.version=..."
var version = "dev"

func main() {
	level := slog.LevelInfo
	if os.Getenv("LABDASH_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer database.Close()

	box, err := secrets.New(cfg.Secret)
	if err != nil {
		return err
	}
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}

	authSvc := auth.New(database)
	hub := events.NewHub()
	store := &integrations.Store{DB: database, Box: box}
	po := pushover.New()
	engine := monitor.NewEngine(database, hub, &alerts.Pushover{Store: store, Client: po})
	srv := &api.Server{
		DB:           database,
		Cfg:          cfg,
		Auth:         authSvc,
		Events:       hub,
		Monitor:      engine,
		Integrations: store,
		Pushover:     po,
		Secrets:      box,
		Icons:        icons.New(cfg.DataDir + "/icons"),
		Web:          dist,
		Version:      version,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go engine.Run(ctx)

	// Background housekeeping.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			authSvc.PruneSessions(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("LabDash listening", "addr", httpSrv.Addr, "version", version, "data", cfg.DataDir)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(sctx)
	}
	return nil
}

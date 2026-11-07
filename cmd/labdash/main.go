package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	"github.com/bentonraymer/labdash/internal/backup"
	"github.com/bentonraymer/labdash/internal/config"
	"github.com/bentonraymer/labdash/internal/db"
	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/icons"
	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/npm"
	"github.com/bentonraymer/labdash/internal/proxmox"
	"github.com/bentonraymer/labdash/internal/pushover"
	"github.com/bentonraymer/labdash/internal/scanner"
	"github.com/bentonraymer/labdash/internal/secrets"
	"github.com/bentonraymer/labdash/web"
)

// version is set at build time with -ldflags "-X main.version=..."
var version = "dev"

// restartRequested makes the process exit non-zero after shutdown so the
// container restart policy brings it back (used to apply a restore).
var restartRequested bool

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
	if restartRequested {
		slog.Info("exiting so the container restarts and applies the restore")
		os.Exit(3)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if applied, err := backup.ApplyPending(cfg.DataDir); err != nil {
		return fmt.Errorf("apply staged restore: %w", err)
	} else if applied {
		slog.Info("restored data from backup")
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
	pushAlerts := &alerts.Pushover{Store: store, Client: po}
	engine := monitor.NewEngine(database, hub, pushAlerts)
	iconCache := icons.New(cfg.DataDir + "/icons")
	scan := scanner.NewService(database, hub, iconCache, func(ctx context.Context, devs []scanner.NewDevice) {
		pushAlerts.NewDevices(ctx, func(ctx context.Context) bool {
			return settingOn(ctx, database, "new_device_alerts", true) && !settingOn(ctx, database, "alerts_paused", false)
		}, devs)
	})
	srv := &api.Server{
		DB:           database,
		Cfg:          cfg,
		Auth:         authSvc,
		Events:       hub,
		Monitor:      engine,
		Integrations: store,
		Pushover:     po,
		Scanner:      scan,
		NPM:          npm.New(),
		Secrets:      box,
		Icons:        iconCache,
		Web:          dist,
		Version:      version,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv.Proxmox = proxmox.NewPoller(srv.ProxmoxLoader, hub)
	srv.Restart = func() {
		restartRequested = true
		stop()
	}
	go srv.Proxmox.Run(ctx)
	go nightlyBackups(ctx, database, cfg.DataDir)

	go engine.Run(ctx)
	go scan.Run(ctx)

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

// settingOn reads a boolean setting, falling back to def when unset.
func settingOn(ctx context.Context, db *sql.DB, key string, def bool) bool {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return def
	}
	return raw == "true"
}

// nightlyBackups writes a backup zip at 03:30 local time when enabled.
func nightlyBackups(ctx context.Context, db *sql.DB, dataDir string) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 30, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		if !settingOn(ctx, db, "backup_auto", true) {
			continue
		}
		keep := 7
		var raw string
		if db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'backup_keep'`).Scan(&raw) == nil {
			fmt.Sscanf(raw, "%d", &keep)
		}
		if name, err := backup.SaveTo(ctx, db, dataDir, version, keep); err != nil {
			slog.Error("nightly backup failed", "err", err)
		} else {
			slog.Info("nightly backup written", "file", name)
		}
	}
}

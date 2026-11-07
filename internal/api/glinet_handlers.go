package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/glinet"
	"github.com/bentonraymer/labdash/internal/integrations"
)

// RouterSnapshot is the latest data read from the GL.iNet router.
type RouterSnapshot struct {
	Enabled      bool                `json:"enabled"`
	Error        string              `json:"error"`
	FetchedAt    string              `json:"fetchedAt"`
	Reservations []glinet.StaticBind `json:"reservations"`
	Clients      []glinet.ClientInfo `json:"clients"`
}

type routerState struct {
	mu   sync.RWMutex
	snap RouterSnapshot
	kick chan struct{}
}

func (s *Server) routerCreds(ctx context.Context) (glinet.Config, glinet.Secret, bool, error) {
	var cfg glinet.Config
	var sec glinet.Secret
	enabled, err := s.Integrations.Get(ctx, "glinet", &cfg, &sec)
	if errors.Is(err, integrations.ErrNotConfigured) {
		return cfg, sec, false, nil
	}
	return cfg, sec, enabled, err
}

func (s *Server) routerSnapshot() RouterSnapshot {
	s.router.mu.RLock()
	defer s.router.mu.RUnlock()
	return s.router.snap
}

// RunRouterPoller refreshes router data every minute while enabled.
func (s *Server) RunRouterPoller(ctx context.Context) {
	s.router.mu.Lock()
	s.router.kick = make(chan struct{}, 1)
	kick := s.router.kick
	s.router.mu.Unlock()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.pollRouter(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
		}
	}
}

func (s *Server) refreshRouter() {
	s.router.mu.RLock()
	k := s.router.kick
	s.router.mu.RUnlock()
	if k != nil {
		select {
		case k <- struct{}{}:
		default:
		}
	}
}

func (s *Server) pollRouter(ctx context.Context) {
	cfg, sec, enabled, err := s.routerCreds(ctx)
	snap := RouterSnapshot{Enabled: enabled, FetchedAt: time.Now().UTC().Format(time.RFC3339), Reservations: []glinet.StaticBind{}, Clients: []glinet.ClientInfo{}}
	if err == nil && enabled {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		snap.Reservations, err = s.GLiNet.StaticBinds(pctx, cfg, sec)
		if err == nil {
			snap.Clients, err = s.GLiNet.Clients(pctx, cfg, sec)
		}
		cancel()
	}
	if err != nil && enabled {
		snap.Error = err.Error()
		slog.Debug("router poll failed", "err", err)
		prev := s.routerSnapshot() // keep last good data visible
		snap.Reservations, snap.Clients = prev.Reservations, prev.Clients
	}
	if snap.Reservations == nil {
		snap.Reservations = []glinet.StaticBind{}
	}
	if snap.Clients == nil {
		snap.Clients = []glinet.ClientInfo{}
	}
	s.router.mu.Lock()
	s.router.snap = snap
	s.router.mu.Unlock()
	if enabled && snap.Error == "" {
		if err := s.syncRouterClients(ctx, snap.Clients); err != nil {
			slog.Warn("router client sync failed", "err", err)
		}
		s.Events.Publish("devices", nil)
	}
}

// syncRouterClients records the router's name for each client and adds
// devices the scanner hasn't seen (e.g. on unscanned subnets). They're
// marked acknowledged: the router already knows them, so no alerts.
func (s *Server) syncRouterClients(ctx context.Context, clients []glinet.ClientInfo) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	for _, c := range clients {
		if c.MAC == "" || c.Remote {
			continue
		}
		key := "mac:" + c.MAC
		res, err := tx.ExecContext(ctx, `UPDATE devices SET router_name = ?,
			ip = CASE WHEN ? != '' THEN ? ELSE ip END,
			is_up = CASE WHEN ? THEN 1 ELSE is_up END,
			last_seen = CASE WHEN ? THEN ? ELSE last_seen END
			WHERE key = ?`, c.DisplayName(), c.IP, c.IP, c.Online, c.Online, ts, key)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 && c.IP != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO devices(key, mac, ip, router_name, device_type, is_up, acknowledged, first_seen, last_seen)
				VALUES (?, ?, ?, ?, 'Client', ?, 1, ?, ?)`, key, c.MAC, c.IP, c.DisplayName(), c.Online, ts, ts); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ── settings ──────────────────────────────────────────────────────────────

func (s *Server) handleGetGLiNet(w http.ResponseWriter, r *http.Request) {
	cfg, sec, enabled, err := s.routerCreds(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": cfg.URL != "" && sec.Password != "", "enabled": enabled,
		"url": cfg.URL, "username": cfg.Username, "hasPassword": sec.Password != "",
	})
}

func (s *Server) handlePutGLiNet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool   `json:"enabled"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"` // '' keeps the stored one
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, sec, _, err := s.routerCreds(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.Password != "" {
		sec.Password = body.Password
	}
	user := strings.TrimSpace(body.Username)
	if user == "" {
		user = "root"
	}
	if err := s.Integrations.Put(r.Context(), "glinet", body.Enabled, glinet.Config{URL: strings.TrimSpace(body.URL), Username: user}, sec); err != nil {
		serverErr(w, err)
		return
	}
	s.refreshRouter()
	s.handleGetGLiNet(w, r)
}

func (s *Server) handleTestGLiNet(w http.ResponseWriter, r *http.Request) {
	cfg, sec, _, err := s.routerCreds(r.Context())
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		var binds []glinet.StaticBind
		var clients []glinet.ClientInfo
		if binds, err = s.GLiNet.StaticBinds(ctx, cfg, sec); err == nil {
			if clients, err = s.GLiNet.Clients(ctx, cfg, sec); err == nil {
				writeJSON(w, http.StatusOK, map[string]int{"reservations": len(binds), "clients": len(clients)})
				return
			}
		}
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

func (s *Server) handleRouterData(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.routerSnapshot())
}

// reserveOnRouter adds a DHCP reservation unless one already exists for
// that MAC or IP.
func (s *Server) reserveOnRouter(ctx context.Context, b glinet.StaticBind) (string, error) {
	cfg, sec, enabled, err := s.routerCreds(ctx)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", errors.New("GL.iNet integration is disabled")
	}
	mac := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(b.MAC), "-", ":"))
	existing, err := s.GLiNet.StaticBinds(ctx, cfg, sec)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if e.MAC == mac && e.IP == b.IP {
			return "already reserved on the router", nil
		}
		if e.IP == b.IP {
			return "", errors.New(b.IP + " is already reserved on the router for " + e.MAC + " (" + e.Name + ")")
		}
		if e.MAC == mac {
			return "", errors.New(mac + " already has a reservation for " + e.IP + "; change it in the router UI")
		}
	}
	if err := s.GLiNet.AddStaticBind(ctx, cfg, sec, b); err != nil {
		return "", err
	}
	s.refreshRouter()
	return "reserved " + b.IP + " for " + mac + " on the router", nil
}

func (s *Server) handleAddReservation(w http.ResponseWriter, r *http.Request) {
	var b glinet.StaticBind
	if err := readJSON(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	detail, err := s.reserveOnRouter(ctx, b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"detail": detail})
}

// Package api wires the HTTP routes for LabDash.
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bentonraymer/labdash/internal/auth"
	"github.com/bentonraymer/labdash/internal/config"
	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/glinet"
	"github.com/bentonraymer/labdash/internal/icons"
	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/npm"
	"github.com/bentonraymer/labdash/internal/proxmox"
	"github.com/bentonraymer/labdash/internal/pushover"
	"github.com/bentonraymer/labdash/internal/scanner"
	"github.com/bentonraymer/labdash/internal/secrets"
	"github.com/bentonraymer/labdash/internal/vuln"
)

type Server struct {
	DB           *sql.DB
	Cfg          *config.Config
	Auth         *auth.Service
	Events       *events.Hub
	Secrets      *secrets.Box
	Icons        *icons.Cache
	Monitor      *monitor.Engine
	Integrations *integrations.Store
	Pushover     *pushover.Client
	Scanner      *scanner.Service
	NPM          *npm.Client
	Proxmox      *proxmox.Poller
	GLiNet       *glinet.Client
	Vuln         *vuln.Service
	// Restart exits the process so Docker restarts it (used to apply a restore).
	Restart func()

	npmCache     npmCache
	nucleiVer    sync.Once
	nucleiVerStr string
	router       routerState
	Web          fs.FS // built frontend (web/dist)
	Version      string
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Recoverer, requestLogger, securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Handle("/icons/di/*", s.Icons)
	r.Get("/icons/custom/*", s.Icons.ServeCustom)
	r.Handle("/media/wallpapers/*", http.StripPrefix("/media/wallpapers/", noDirListing(http.Dir(filepath.Join(s.Cfg.DataDir, "wallpapers")))))

	// Note attachments need a session (they may contain configs/secrets).
	r.With(s.Auth.Middleware).Get("/media/uploads/*", s.handleServeUpload)

	r.Route("/api", func(r chi.Router) {
		r.Use(jsonOnly, csrfHeader)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Middleware)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangePassword)
			r.Get("/events", s.handleEvents)

			r.Get("/settings", s.handleGetSettings)
			r.Patch("/settings", s.handlePatchSettings)
			r.Post("/wallpaper", s.handleWallpaperUpload)

			r.Get("/launchpad", s.handleLaunchpad)
			r.Put("/launchpad/layout", s.handleLayout)

			r.Get("/sections", s.handleListSections)
			r.Post("/sections", s.handleCreateSection)
			r.Patch("/sections/{id}", s.handleUpdateSection)
			r.Delete("/sections/{id}", s.handleDeleteSection)

			r.Get("/services", s.handleListServices)
			r.Post("/services", s.handleCreateService)
			r.Get("/services/{id}", s.handleGetService)
			r.Patch("/services/{id}", s.handleUpdateService)
			r.Delete("/services/{id}", s.handleDeleteService)

			r.Get("/icons/index", s.handleIconIndex)
			r.Post("/icons/upload", s.handleIconUpload)
			r.Post("/icons/favicon", s.handleIconFavicon)

			r.Post("/import/dashy", s.handleImportDashy)

			r.Post("/services/wizard", s.handleServiceWizard)
			r.Put("/services/{id}/deployment", s.handlePutDeployment)

			r.Get("/hosts", s.handleListHosts)
			r.Post("/hosts", s.handleCreateHost)
			r.Patch("/hosts/{id}", s.handleUpdateHost)
			r.Delete("/hosts/{id}", s.handleDeleteHost)

			r.Get("/subnets", s.handleListSubnets)
			r.Post("/subnets", s.handleSaveSubnet)
			r.Put("/subnets/{id}", s.handleSaveSubnet)
			r.Delete("/subnets/{id}", s.handleDeleteSubnet)
			r.Get("/subnets/{id}/map", s.handleSubnetMap)
			r.Get("/ips/check", s.handleIPCheck)
			r.Get("/ips/{ip}", s.handleIPDetail)
			r.Get("/ips/{ip}/free-port", s.handleFreePort)
			r.Put("/ip-assignments", s.handleSaveAssignment)
			r.Delete("/ip-assignments/{ip}", s.handleDeleteAssignment)
			r.Post("/port-claims", s.handleCreatePortClaim)
			r.Delete("/port-claims/{id}", s.handleDeletePortClaim)

			r.Get("/integrations/npm", s.handleGetNPM)
			r.Put("/integrations/npm", s.handlePutNPM)
			r.Post("/integrations/npm/test", s.handleTestNPM)
			r.Get("/npm/status", s.handleNPMStatus)
			r.Post("/services/{id}/npm", s.handleNPMCreate)
			r.Get("/integrations/proxmox", s.handleGetProxmox)
			r.Put("/integrations/proxmox", s.handlePutProxmox)
			r.Post("/integrations/proxmox/test", s.handleTestProxmox)
			r.Get("/proxmox/resources", s.handleProxmoxResources)
			r.Post("/wol", s.handleWOL)
			r.Get("/integrations/glinet", s.handleGetGLiNet)
			r.Put("/integrations/glinet", s.handlePutGLiNet)
			r.Post("/integrations/glinet/test", s.handleTestGLiNet)
			r.Get("/router", s.handleRouterData)
			r.Post("/router/reservations", s.handleAddReservation)
			r.Get("/backup", s.handleBackupDownload)
			r.Get("/backups", s.handleListBackups)
			r.Post("/backups", s.handleCreateBackup)
			r.Get("/backups/{name}", s.handleStoredBackup)
			r.Delete("/backups/{name}", s.handleStoredBackup)
			r.Post("/restore", s.handleRestore)

			r.Get("/folders", s.handleListFolders)
			r.Post("/folders", s.handleSaveFolder)
			r.Patch("/folders/{id}", s.handleSaveFolder)
			r.Delete("/folders/{id}", s.handleDeleteFolder)
			r.Get("/notes", s.handleListNotes)
			r.Post("/notes", s.handleCreateNote)
			r.Get("/notes/search", s.handleSearchNotes)
			r.Get("/notes/export", s.handleExportDocs)
			r.Get("/notes/{id}", s.handleGetNote)
			r.Patch("/notes/{id}", s.handleUpdateNote)
			r.Delete("/notes/{id}", s.handleDeleteNote)
			r.Post("/notes/{id}/snapshot", s.handleSnapshot)
			r.Get("/notes/{id}/versions", s.handleListVersions)
			r.Get("/notes/{id}/versions/{vid}", s.handleGetVersion)
			r.Post("/notes/{id}/versions/{vid}/restore", s.handleRestoreVersion)
			r.Post("/notes/{id}/attachments", s.handleUploadAttachment)
			r.Get("/tags", s.handleListTags)

			r.Get("/scan-targets", s.handleListScanTargets)
			r.Post("/scan-targets", s.handleSaveScanTarget)
			r.Put("/scan-targets/{id}", s.handleSaveScanTarget)
			r.Delete("/scan-targets/{id}", s.handleDeleteScanTarget)
			r.Post("/scan-targets/{id}/run", s.handleRunScanTarget)
			r.Get("/scans", s.handleScanStatus)
			r.Post("/scan-targets/{id}/vuln-scan", s.handleRunVulnScan)
			r.Get("/vulns", s.handleListVulns)
			r.Get("/vulns/status", s.handleVulnStatus)
			r.Post("/vulns/sync", s.handleVulnSync)
			r.Patch("/vulns/{id}", s.handleUpdateVuln)
			r.Get("/devices", s.handleListDevices)
			r.Post("/devices/acknowledge-all", s.handleAcknowledgeAll)
			r.Patch("/devices/{id}", s.handleUpdateDevice)
			r.Delete("/devices/{id}", s.handleDeleteDevice)
			r.Get("/devices/{id}/events", s.handleDeviceEvents)

			r.Get("/monitors", s.handleListMonitors)
			r.Post("/monitors", s.handleCreateMonitor)
			r.Patch("/monitors/{id}", s.handleUpdateMonitor)
			r.Delete("/monitors/{id}", s.handleDeleteMonitor)
			r.Post("/monitors/{id}/check", s.handleCheckMonitor)
			r.Get("/monitors/{id}/history", s.handleMonitorHistory)
			r.Get("/incidents", s.handleListIncidents)

			r.Get("/maintenance", s.handleListMaintenance)
			r.Post("/maintenance", s.handleSaveMaintenance)
			r.Put("/maintenance/{id}", s.handleSaveMaintenance)
			r.Delete("/maintenance/{id}", s.handleDeleteMaintenance)

			r.Get("/integrations/pushover", s.handleGetPushover)
			r.Put("/integrations/pushover", s.handlePutPushover)
			r.Post("/integrations/pushover/test", s.handleTestPushover)

			r.Get("/spotlight/usage", s.handleSpotlightUsage)
			r.Post("/spotlight/usage", s.handleSpotlightRecord)
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	})

	r.NotFound(s.spa().ServeHTTP)
	return r
}

// spa serves the embedded frontend, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	fileServer := http.FileServer(http.FS(s.Web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if st, err := fs.Stat(s.Web, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		idx, err := fs.ReadFile(s.Web, "index.html")
		if err != nil {
			http.Error(w, "frontend not built — run `npm run build` in web/", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(idx)
	})
}

// ── middleware ────────────────────────────────────────────────────────────

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/events" {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start))
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

func jsonOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csrfHeader requires a custom header on state-changing requests. Browsers
// cannot send it cross-site without a CORS preflight, which we never allow.
func csrfHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-LabDash") != "1" {
				writeErr(w, http.StatusForbidden, "missing X-LabDash header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type noDirFS struct{ fs http.FileSystem }

func (n noDirFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if st, _ := f.Stat(); st != nil && st.IsDir() {
		f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}

func noDirListing(fsys http.FileSystem) http.Handler { return http.FileServer(noDirFS{fsys}) }

// ── helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func serverErr(w http.ResponseWriter, err error) {
	slog.Error("handler error", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

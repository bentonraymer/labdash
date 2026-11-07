package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bentonraymer/labdash/internal/backup"
	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/ipam"
	"github.com/bentonraymer/labdash/internal/npm"
	"github.com/bentonraymer/labdash/internal/proxmox"
	"github.com/bentonraymer/labdash/internal/wol"
)

// ── Nginx Proxy Manager ───────────────────────────────────────────────────

type npmCache struct {
	mu    sync.Mutex
	at    time.Time
	hosts []npm.ProxyHost
	certs []npm.Certificate
	err   error
}

func (s *Server) npmCreds(ctx context.Context) (npm.Config, npm.Secret, bool, error) {
	var cfg npm.Config
	var sec npm.Secret
	enabled, err := s.Integrations.Get(ctx, "npm", &cfg, &sec)
	return cfg, sec, enabled, err
}

// npmData returns proxy hosts/certs, cached for 30s (force bypasses the cache).
func (s *Server) npmData(ctx context.Context, force bool) ([]npm.ProxyHost, []npm.Certificate, error) {
	c := &s.npmCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && time.Since(c.at) < 30*time.Second {
		return c.hosts, c.certs, c.err
	}
	cfg, sec, enabled, err := s.npmCreds(ctx)
	if err == nil && !enabled {
		err = errors.New("NPM integration is disabled")
	}
	var hosts []npm.ProxyHost
	var certs []npm.Certificate
	if err == nil {
		hosts, err = s.NPM.ProxyHosts(ctx, cfg, sec)
	}
	if err == nil {
		certs, err = s.NPM.Certificates(ctx, cfg, sec)
	}
	c.at, c.hosts, c.certs, c.err = time.Now(), hosts, certs, err
	return hosts, certs, err
}

func (s *Server) handleGetNPM(w http.ResponseWriter, r *http.Request) {
	cfg, sec, enabled, err := s.npmCreds(r.Context())
	if err != nil && !errors.Is(err, integrations.ErrNotConfigured) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": cfg.URL != "" && sec.Email != "" && sec.Password != "", "enabled": enabled,
		"url": cfg.URL, "verifyTls": cfg.VerifyTLS, "email": sec.Email, "hasPassword": sec.Password != "",
	})
}

func (s *Server) handlePutNPM(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled   bool   `json:"enabled"`
		URL       string `json:"url"`
		VerifyTLS bool   `json:"verifyTls"`
		Email     string `json:"email"`
		Password  string `json:"password"` // '' keeps the stored one
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, sec, _, err := s.npmCreds(r.Context())
	if err != nil && !errors.Is(err, integrations.ErrNotConfigured) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	sec.Email = strings.TrimSpace(body.Email)
	if body.Password != "" {
		sec.Password = body.Password
	}
	if err := s.Integrations.Put(r.Context(), "npm", body.Enabled, npm.Config{URL: strings.TrimSpace(body.URL), VerifyTLS: body.VerifyTLS}, sec); err != nil {
		serverErr(w, err)
		return
	}
	s.npmCache.mu.Lock()
	s.npmCache.at = time.Time{}
	s.npmCache.mu.Unlock()
	s.handleGetNPM(w, r)
}

func (s *Server) handleTestNPM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	hosts, certs, err := s.npmData(ctx, true)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"proxyHosts": len(hosts), "certificates": len(certs)})
}

type npmCert struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Domains   []string `json:"domains"`
	ExpiresOn string   `json:"expiresOn"`
}

// handleNPMStatus reports, for every service with a DNS URL, whether NPM has
// a matching proxy host and whether it forwards to the service's IP:port.
func (s *Server) handleNPMStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, _, enabled, cerr := s.npmCreds(ctx)
	if errors.Is(cerr, integrations.ErrNotConfigured) || !enabled {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	hosts, certs, err := s.npmData(ctx, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "error": err.Error()})
		return
	}
	svcs, err := s.listServices(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	statuses := map[int64]npm.Status{}
	for _, sv := range svcs {
		port := 0
		if sv.DirectPort != nil {
			port = *sv.DirectPort
		}
		statuses[sv.ID] = npm.Compare(hosts, hostOf(sv.DNSURL), sv.DirectIP, port)
	}
	cs := make([]npmCert, 0, len(certs))
	for _, c := range certs {
		cs = append(cs, npmCert{ID: c.ID, Name: c.NiceName, Domains: c.DomainNames, ExpiresOn: c.ExpiresOn})
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "services": statuses, "certificates": cs, "proxyHosts": len(hosts)})
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

type npmCreateInput struct {
	CertificateID int  `json:"certificateId"`
	SSLForced     bool `json:"sslForced"`
	Websockets    bool `json:"websockets"`
}

// createProxyHostFor creates an NPM proxy host for a service's DNS name,
// forwarding to its direct IP:port.
func (s *Server) createProxyHostFor(ctx context.Context, serviceID int64, in npmCreateInput) (string, error) {
	sv, err := s.getService(ctx, serviceID)
	if err != nil {
		return "", err
	}
	domain := hostOf(sv.DNSURL)
	if domain == "" {
		return "", errors.New("the service has no DNS URL")
	}
	if sv.DirectIP == "" || sv.DirectPort == nil {
		return "", errors.New("the service needs a direct IP and port for NPM to forward to")
	}
	cfg, sec, enabled, err := s.npmCreds(ctx)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", errors.New("NPM integration is disabled")
	}
	hosts, _, err := s.npmData(ctx, true)
	if err != nil {
		return "", err
	}
	if st := npm.Compare(hosts, domain, "", 0); st.State != "missing" {
		return "", fmt.Errorf("NPM already has a proxy host for %s (→ %s)", domain, st.Forward)
	}
	ph, err := s.NPM.CreateProxyHost(ctx, cfg, sec, npm.NewProxyHost{
		Domain: domain, ForwardScheme: sv.DirectScheme, ForwardHost: sv.DirectIP, ForwardPort: *sv.DirectPort,
		CertificateID: in.CertificateID, SSLForced: in.SSLForced, Websockets: in.Websockets, BlockExploits: true,
	})
	s.npmCache.mu.Lock()
	s.npmCache.at = time.Time{}
	s.npmCache.mu.Unlock()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s → %s://%s:%d (proxy host #%d)", domain, sv.DirectScheme, sv.DirectIP, *sv.DirectPort, ph.ID), nil
}

func (s *Server) handleNPMCreate(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in npmCreateInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	detail, err := s.createProxyHostFor(ctx, id, in)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"detail": detail})
}

// ── Proxmox ───────────────────────────────────────────────────────────────

func (s *Server) ProxmoxLoader(ctx context.Context) (proxmox.Config, proxmox.Secret, bool, error) {
	var cfg proxmox.Config
	var sec proxmox.Secret
	enabled, err := s.Integrations.Get(ctx, "proxmox", &cfg, &sec)
	if errors.Is(err, integrations.ErrNotConfigured) {
		return cfg, sec, false, nil
	}
	return cfg, sec, enabled, err
}

func (s *Server) handleGetProxmox(w http.ResponseWriter, r *http.Request) {
	cfg, sec, enabled, err := s.ProxmoxLoader(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": cfg.URL != "" && sec.TokenID != "" && sec.TokenSecret != "", "enabled": enabled,
		"url": cfg.URL, "verifyTls": cfg.VerifyTLS, "tokenId": sec.TokenID, "hasSecret": sec.TokenSecret != "",
	})
}

func (s *Server) handlePutProxmox(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled     bool   `json:"enabled"`
		URL         string `json:"url"`
		VerifyTLS   bool   `json:"verifyTls"`
		TokenID     string `json:"tokenId"`
		TokenSecret string `json:"tokenSecret"` // '' keeps the stored one
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, sec, _, err := s.ProxmoxLoader(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	sec.TokenID = strings.TrimSpace(body.TokenID)
	if body.TokenSecret != "" {
		sec.TokenSecret = strings.TrimSpace(body.TokenSecret)
	}
	if err := s.Integrations.Put(r.Context(), "proxmox", body.Enabled, proxmox.Config{URL: strings.TrimSpace(body.URL), VerifyTLS: body.VerifyTLS}, sec); err != nil {
		serverErr(w, err)
		return
	}
	s.Proxmox.Refresh()
	s.handleGetProxmox(w, r)
}

func (s *Server) handleTestProxmox(w http.ResponseWriter, r *http.Request) {
	cfg, sec, _, err := s.ProxmoxLoader(r.Context())
	if err == nil {
		var res []proxmox.Resource
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		res, err = proxmox.Fetch(ctx, cfg, sec)
		if err == nil {
			nodes, guests := 0, 0
			for _, x := range res {
				if x.Type == "node" {
					nodes++
				} else {
					guests++
				}
			}
			writeJSON(w, http.StatusOK, map[string]int{"nodes": nodes, "guests": guests})
			return
		}
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

func (s *Server) handleProxmoxResources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Proxmox.Snapshot())
}

// ── Wake-on-LAN ───────────────────────────────────────────────────────────

func (s *Server) handleWOL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string `json:"mac"`
		IP  string `json:"ip"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Also target the subnet broadcast of the device's subnet, if known.
	var extra []string
	if a, err := netip.ParseAddr(body.IP); err == nil {
		subnets, _ := s.listSubnets(r.Context())
		for _, sv := range subnets {
			if sn, err := ipam.Parse(sv.CIDR, "", "", ""); err == nil && sn.Prefix.Contains(a) {
				extra = append(extra, sn.Broadcast().String())
			}
		}
	}
	if err := wol.Send(body.MAC, extra...); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sentTo": append([]string{"255.255.255.255"}, extra...)})
}

// ── Backup & restore ──────────────────────────────────────────────────────

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := "labdash-" + time.Now().Format("20060102-150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if err := backup.Write(r.Context(), s.DB, s.Cfg.DataDir, s.Version, w); err != nil {
		// Headers are already sent; the truncated zip will fail to open.
		serverErr(w, err)
	}
}

func (s *Server) handleListBackups(w http.ResponseWriter, _ *http.Request) {
	list, err := backup.List(s.Cfg.DataDir)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": list, "restorePending": backup.HasPending(s.Cfg.DataDir)})
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	keep := int(s.settingNumber(r.Context(), "backup_keep", 7))
	name, err := backup.SaveTo(r.Context(), s.DB, s.Cfg.DataDir, s.Version, keep)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (s *Server) handleStoredBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !backup.ValidName(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(backup.Dir(s.Cfg.DataDir), name)
	if r.Method == http.MethodDelete {
		if err := os.Remove(path); err != nil {
			writeErr(w, http.StatusNotFound, "backup not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}

// handleRestore stages a backup (uploaded, or one of the stored ones) and
// restarts LabDash so it's applied before the database is opened.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var zipPath string
	if name := r.URL.Query().Get("name"); name != "" {
		if !backup.ValidName(name) {
			writeErr(w, http.StatusBadRequest, "invalid backup name")
			return
		}
		zipPath = filepath.Join(backup.Dir(s.Cfg.DataDir), name)
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "upload a LabDash backup .zip")
			return
		}
		defer f.Close()
		tmp, err := os.CreateTemp(s.Cfg.DataDir, ".restore-*.zip")
		if err != nil {
			serverErr(w, err)
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := io.Copy(tmp, f); err != nil {
			tmp.Close()
			writeErr(w, http.StatusBadRequest, "upload failed")
			return
		}
		tmp.Close()
		zipPath = tmp.Name()
	}
	m, err := backup.Stage(s.Cfg.DataDir, zipPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backupCreatedAt": m.CreatedAt, "restarting": s.Restart != nil})
	if s.Restart != nil {
		go func() {
			time.Sleep(time.Second)
			s.Restart()
		}()
	}
}

func (s *Server) settingNumber(ctx context.Context, key string, def float64) float64 {
	st, err := s.loadSettings(ctx)
	if err != nil {
		return def
	}
	if v, ok := st[key].(float64); ok {
		return v
	}
	return def
}

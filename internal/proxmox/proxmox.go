// Package proxmox polls a Proxmox VE cluster (read-only API token) for node
// and guest stats. LabDash never creates anything from it: services are
// linked to a VMID by hand and only *enriched* with these numbers.
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/events"
)

// Secret is stored encrypted.
type Secret struct {
	TokenID     string `json:"tokenId"`     // e.g. labdash@pve!dash
	TokenSecret string `json:"tokenSecret"` // UUID
}

type Config struct {
	URL       string `json:"url"` // https://192.168.8.11:8006 (any node in the cluster)
	VerifyTLS bool   `json:"verifyTls"`
}

// Resource is one row of /cluster/resources (node, qemu or lxc).
type Resource struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"` // node | qemu | lxc
	Node    string  `json:"node"`
	VMID    int     `json:"vmid,omitempty"`
	Name    string  `json:"name,omitempty"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     float64 `json:"mem"`
	MaxMem  float64 `json:"maxmem"`
	Disk    float64 `json:"disk"`
	MaxDisk float64 `json:"maxdisk"`
	Uptime  float64 `json:"uptime"`
	NetIn   float64 `json:"netin,omitempty"`
	NetOut  float64 `json:"netout,omitempty"`
	Tags    string  `json:"tags,omitempty"`
	// Connection is the name of the LabDash connection that reported it.
	Connection string `json:"connection"`
}

// Fetch reads every node and guest in one call.
func Fetch(ctx context.Context, cfg Config, sec Secret) ([]Resource, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return nil, errors.New("Proxmox URL must look like https://192.168.8.11:8006")
	}
	base = strings.TrimSuffix(base, "/api2/json")
	if sec.TokenID == "" || sec.TokenSecret == "" {
		return nil, errors.New("API token ID and secret are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api2/json/cluster/resources", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", sec.TokenID, sec.TokenSecret))
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.VerifyTLS}}, //nolint:gosec // Proxmox uses self-signed certs by default
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't reach Proxmox: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("Proxmox rejected the API token (check token ID/secret and that it has PVEAuditor)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Proxmox: %s", resp.Status)
	}
	var out struct {
		Data []Resource `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unexpected Proxmox response: %w", err)
	}
	res := out.Data[:0]
	for _, r := range out.Data {
		if r.Type == "node" || r.Type == "qemu" || r.Type == "lxc" {
			res = append(res, r)
		}
	}
	return res, nil
}

// ── multiple connections ──────────────────────────────────────────────────

// Connection is one Proxmox endpoint: a standalone node, or any node of a
// cluster (one connection per cluster is enough).
type Connection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	VerifyTLS bool   `json:"verifyTls"`
	TokenID   string `json:"tokenId"`
}

// StoredConfig is the plain-JSON part of the integration. URL/VerifyTLS are
// the legacy single-connection fields, still read for older installs.
type StoredConfig struct {
	Connections []Connection `json:"connections"`
	URL         string       `json:"url,omitempty"`
	VerifyTLS   bool         `json:"verifyTls,omitempty"`
}

// StoredSecret holds token secrets keyed by connection ID (plus legacy fields).
type StoredSecret struct {
	Tokens      map[string]string `json:"tokens"`
	TokenID     string            `json:"tokenId,omitempty"`
	TokenSecret string            `json:"tokenSecret,omitempty"`
}

// Target is a connection with its secret, ready to poll.
type Target struct {
	Connection
	Secret string
}

// Normalize converts legacy single-connection settings into the list form.
func Normalize(cfg StoredConfig, sec StoredSecret) (StoredConfig, StoredSecret) {
	if sec.Tokens == nil {
		sec.Tokens = map[string]string{}
	}
	if len(cfg.Connections) == 0 && cfg.URL != "" {
		cfg.Connections = []Connection{{ID: "legacy", Name: "Proxmox", URL: cfg.URL, VerifyTLS: cfg.VerifyTLS, TokenID: sec.TokenID}}
		sec.Tokens["legacy"] = sec.TokenSecret
	}
	cfg.URL, cfg.VerifyTLS, sec.TokenID, sec.TokenSecret = "", false, "", ""
	return cfg, sec
}

// Targets pairs each connection with its secret.
func Targets(cfg StoredConfig, sec StoredSecret) []Target {
	cfg, sec = Normalize(cfg, sec)
	out := make([]Target, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		out = append(out, Target{Connection: c, Secret: sec.Tokens[c.ID]})
	}
	return out
}

// FetchAll polls every target concurrently and merges the results. A
// cluster reachable through two connections is only counted once.
func FetchAll(ctx context.Context, targets []Target) ([]Resource, []ConnError) {
	type result struct {
		res []Resource
		err error
		t   Target
	}
	ch := make(chan result, len(targets))
	for _, t := range targets {
		go func(t Target) {
			res, err := Fetch(ctx, Config{URL: t.URL, VerifyTLS: t.VerifyTLS}, Secret{TokenID: t.TokenID, TokenSecret: t.Secret})
			ch <- result{res, err, t}
		}(t)
	}
	seen := map[string]bool{}
	var all []Resource
	var errs []ConnError
	results := make([]result, 0, len(targets))
	for range targets {
		results = append(results, <-ch)
	}
	// Stable order: same as the configured connections.
	for _, t := range targets {
		for _, r := range results {
			if r.t.ID != t.ID {
				continue
			}
			if r.err != nil {
				errs = append(errs, ConnError{Name: t.Name, Error: r.err.Error()})
				continue
			}
			for _, x := range r.res {
				key := x.ID + "@" + x.Node
				if seen[key] {
					continue
				}
				seen[key] = true
				x.Connection = t.Name
				all = append(all, x)
			}
		}
	}
	return all, errs
}

type ConnError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// Snapshot is the poller's latest view.
type Snapshot struct {
	Resources []Resource  `json:"resources"`
	FetchedAt string      `json:"fetchedAt"`
	Error     string      `json:"error"` // summary of Errors
	Errors    []ConnError `json:"errors"`
	Enabled   bool        `json:"enabled"`
}

// Loader returns the targets to poll (and whether polling is enabled).
type Loader func(ctx context.Context) ([]Target, bool, error)

// Poller refreshes stats every interval while the integration is enabled.
type Poller struct {
	Load     Loader
	Hub      *events.Hub
	Interval time.Duration

	mu   sync.RWMutex
	snap Snapshot
	kick chan struct{}
}

func NewPoller(load Loader, hub *events.Hub) *Poller {
	return &Poller{Load: load, Hub: hub, Interval: 30 * time.Second, kick: make(chan struct{}, 1)}
}

func (p *Poller) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snap
}

// Refresh polls immediately (e.g. after settings change).
func (p *Poller) Refresh() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		p.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-p.kick:
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	targets, enabled, err := p.Load(ctx)
	snap := Snapshot{Enabled: enabled && len(targets) > 0, FetchedAt: time.Now().UTC().Format(time.RFC3339), Errors: []ConnError{}}
	if err != nil {
		snap.Errors = append(snap.Errors, ConnError{Name: "settings", Error: err.Error()})
	} else if snap.Enabled {
		fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		snap.Resources, snap.Errors = FetchAll(fctx, targets)
		cancel()
		if snap.Errors == nil {
			snap.Errors = []ConnError{}
		}
	}
	if len(snap.Errors) > 0 {
		parts := make([]string, len(snap.Errors))
		for i, e := range snap.Errors {
			parts[i] = e.Name + ": " + e.Error
		}
		snap.Error = strings.Join(parts, "; ")
		slog.Debug("proxmox poll errors", "err", snap.Error)
		if len(snap.Resources) == 0 {
			// Keep the last good numbers visible, marked stale by the error.
			p.mu.RLock()
			snap.Resources = p.snap.Resources
			p.mu.RUnlock()
		}
	}
	if snap.Resources == nil {
		snap.Resources = []Resource{}
	}
	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
	if snap.Enabled {
		p.Hub.Publish("proxmox", nil)
	}
}

// ── pending package updates (for the Updates page) ───────────────────────

// AptPackage is one pending update on a node (GET /nodes/{node}/apt/update).
type AptPackage struct {
	Package    string `json:"package"`
	OldVersion string `json:"oldVersion"`
	Version    string `json:"version"`
	Title      string `json:"title"`
	Priority   string `json:"priority"`
}

// NodeUpdates is the pending-update list for one node.
type NodeUpdates struct {
	Node       string       `json:"node"`
	Connection string       `json:"connection"`
	PVEVersion string       `json:"pveVersion"`
	Packages   []AptPackage `json:"packages"`
	Error      string       `json:"error,omitempty"`
}

func get(ctx context.Context, t Target, path string, out any) error {
	base := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(t.URL), "/"), "/api2/json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api2/json"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", t.TokenID, t.Secret))
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !t.VerifyTLS}}, //nolint:gosec // self-signed by default
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach Proxmox: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return errors.New("the API token isn't allowed to list updates (needs Sys.Modify on /nodes, or use a separate token for this)")
	default:
		return fmt.Errorf("Proxmox: %s", resp.Status)
	}
	return json.Unmarshal(body, &struct {
		Data any `json:"data"`
	}{Data: out})
}

// FetchNodeUpdates lists pending apt updates on every node the targets can see.
// Proxmox refreshes this list itself once a day (pve-daily-update).
func FetchNodeUpdates(ctx context.Context, targets []Target) []NodeUpdates {
	var out []NodeUpdates
	seen := map[string]bool{}
	for _, t := range targets {
		var nodes []struct {
			Node   string `json:"node"`
			Status string `json:"status"`
		}
		if err := get(ctx, t, "/nodes", &nodes); err != nil {
			out = append(out, NodeUpdates{Connection: t.Name, Error: err.Error()})
			continue
		}
		for _, n := range nodes {
			if seen[n.Node] {
				continue
			}
			seen[n.Node] = true
			nu := NodeUpdates{Node: n.Node, Connection: t.Name, Packages: []AptPackage{}}
			if n.Status != "" && n.Status != "online" {
				nu.Error = "node is " + n.Status
				out = append(out, nu)
				continue
			}
			var ver struct {
				Version string `json:"version"`
				Release string `json:"release"`
			}
			if get(ctx, t, "/nodes/"+n.Node+"/version", &ver) == nil {
				nu.PVEVersion = ver.Version
			}
			if err := get(ctx, t, "/nodes/"+n.Node+"/apt/update", &nu.Packages); err != nil {
				nu.Error = err.Error()
			}
			out = append(out, nu)
		}
	}
	return out
}

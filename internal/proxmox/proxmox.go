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

// Snapshot is the poller's latest view.
type Snapshot struct {
	Resources []Resource `json:"resources"`
	FetchedAt string     `json:"fetchedAt"`
	Error     string     `json:"error"`
	Enabled   bool       `json:"enabled"`
}

// Loader returns the current config (and whether polling is enabled).
type Loader func(ctx context.Context) (Config, Secret, bool, error)

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
	cfg, sec, enabled, err := p.Load(ctx)
	snap := Snapshot{Enabled: enabled, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	if err == nil && enabled {
		fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		snap.Resources, err = Fetch(fctx, cfg, sec)
		cancel()
	}
	if err != nil && enabled {
		snap.Error = err.Error()
		slog.Debug("proxmox poll failed", "err", err)
		// Keep showing the last good numbers, marked stale by the error.
		p.mu.RLock()
		snap.Resources = p.snap.Resources
		p.mu.RUnlock()
	}
	if snap.Resources == nil {
		snap.Resources = []Resource{}
	}
	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
	if enabled {
		p.Hub.Publish("proxmox", nil)
	}
}

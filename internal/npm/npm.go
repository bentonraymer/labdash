// Package npm talks to the Nginx Proxy Manager API: listing proxy hosts and
// certificates, and creating proxy hosts for new services.
package npm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Secret is stored encrypted in the integrations table.
type Secret struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Config is stored in plain JSON.
type Config struct {
	URL       string `json:"url"` // e.g. http://192.168.8.5:81
	VerifyTLS bool   `json:"verifyTls"`
}

type ProxyHost struct {
	ID             int      `json:"id"`
	DomainNames    []string `json:"domain_names"`
	ForwardScheme  string   `json:"forward_scheme"`
	ForwardHost    string   `json:"forward_host"`
	ForwardPort    int      `json:"forward_port"`
	CertificateID  any      `json:"certificate_id"` // int, 0, or "new"
	SSLForced      any      `json:"ssl_forced"`     // bool or 0/1 depending on NPM version
	Enabled        any      `json:"enabled"`
	AllowWebsocket any      `json:"allow_websocket_upgrade"`
}

type Certificate struct {
	ID          int      `json:"id"`
	NiceName    string   `json:"nice_name"`
	Provider    string   `json:"provider"`
	DomainNames []string `json:"domain_names"`
	ExpiresOn   string   `json:"expires_on"`
}

// NewProxyHost is what LabDash sends to create a host.
type NewProxyHost struct {
	Domain        string
	ForwardScheme string
	ForwardHost   string
	ForwardPort   int
	CertificateID int // 0 = no certificate
	SSLForced     bool
	Websockets    bool
	BlockExploits bool
}

// Client caches a token per credential set.
type Client struct {
	mu       sync.Mutex
	token    string
	expires  time.Time
	tokenKey string
}

func New() *Client { return &Client{} }

func httpClient(cfg Config) *http.Client {
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.VerifyTLS}}, //nolint:gosec // opt-in for self-signed
	}
}

func base(cfg Config) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", errors.New("NPM URL must start with http:// or https://")
	}
	return strings.TrimSuffix(u, "/api"), nil
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, cfg Config, sec Secret, method, path string, body, out any) error {
	b, err := base(cfg)
	if err != nil {
		return err
	}
	tok, err := c.tokenFor(ctx, cfg, sec)
	if err != nil {
		return err
	}
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, b+"/api"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient(cfg).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = "" // force re-login next time
		c.mu.Unlock()
	}
	if resp.StatusCode >= 300 {
		var ae apiError
		if json.Unmarshal(data, &ae) == nil && ae.Error.Message != "" {
			return fmt.Errorf("NPM: %s", ae.Error.Message)
		}
		return fmt.Errorf("NPM: %s", resp.Status)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) tokenFor(ctx context.Context, cfg Config, sec Secret) (string, error) {
	key := cfg.URL + "|" + sec.Email + "|" + sec.Password
	c.mu.Lock()
	if c.token != "" && c.tokenKey == key && time.Until(c.expires) > time.Minute {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	b, err := base(cfg)
	if err != nil {
		return "", err
	}
	if sec.Email == "" || sec.Password == "" {
		return "", errors.New("NPM email and password are required")
	}
	buf, _ := json.Marshal(map[string]string{"identity": sec.Email, "secret": sec.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b+"/api/tokens", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient(cfg).Do(req)
	if err != nil {
		return "", fmt.Errorf("can't reach NPM: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
		apiError
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK || out.Token == "" {
		if out.Error.Message != "" {
			return "", fmt.Errorf("NPM login failed: %s", out.Error.Message)
		}
		return "", fmt.Errorf("NPM login failed: %s", resp.Status)
	}
	exp, err := time.Parse(time.RFC3339, out.Expires)
	if err != nil {
		exp = time.Now().Add(time.Hour)
	}
	c.mu.Lock()
	c.token, c.expires, c.tokenKey = out.Token, exp, key
	c.mu.Unlock()
	return out.Token, nil
}

func (c *Client) ProxyHosts(ctx context.Context, cfg Config, sec Secret) ([]ProxyHost, error) {
	var out []ProxyHost
	err := c.do(ctx, cfg, sec, http.MethodGet, "/nginx/proxy-hosts", nil, &out)
	return out, err
}

func (c *Client) Certificates(ctx context.Context, cfg Config, sec Secret) ([]Certificate, error) {
	var out []Certificate
	err := c.do(ctx, cfg, sec, http.MethodGet, "/nginx/certificates", nil, &out)
	return out, err
}

func (c *Client) CreateProxyHost(ctx context.Context, cfg Config, sec Secret, h NewProxyHost) (ProxyHost, error) {
	if h.Domain == "" || h.ForwardHost == "" || h.ForwardPort < 1 || h.ForwardPort > 65535 {
		return ProxyHost{}, errors.New("domain, forward host and port are required")
	}
	if h.ForwardScheme != "https" {
		h.ForwardScheme = "http"
	}
	body := map[string]any{
		"domain_names":            []string{strings.ToLower(h.Domain)},
		"forward_scheme":          h.ForwardScheme,
		"forward_host":            h.ForwardHost,
		"forward_port":            h.ForwardPort,
		"access_list_id":          0,
		"certificate_id":          h.CertificateID,
		"ssl_forced":              h.SSLForced && h.CertificateID > 0,
		"http2_support":           h.CertificateID > 0,
		"hsts_enabled":            false,
		"hsts_subdomains":         false,
		"block_exploits":          h.BlockExploits,
		"caching_enabled":         false,
		"allow_websocket_upgrade": h.Websockets,
		"advanced_config":         "",
		"locations":               []any{},
		"meta":                    map[string]any{"letsencrypt_agree": false, "dns_challenge": false},
	}
	var out ProxyHost
	err := c.do(ctx, cfg, sec, http.MethodPost, "/nginx/proxy-hosts", body, &out)
	return out, err
}

// Test logs in and counts proxy hosts.
func (c *Client) Test(ctx context.Context, cfg Config, sec Secret) (int, error) {
	hosts, err := c.ProxyHosts(ctx, cfg, sec)
	return len(hosts), err
}

// CertCovers reports whether a certificate covers domain (exact or wildcard).
func CertCovers(cert Certificate, domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range cert.DomainNames {
		d = strings.ToLower(d)
		if d == domain {
			return true
		}
		if strings.HasPrefix(d, "*.") {
			suffix := d[1:] // ".example.com"
			if strings.HasSuffix(domain, suffix) && !strings.Contains(strings.TrimSuffix(domain, suffix), ".") {
				return true
			}
		}
	}
	return false
}

// Status compares a service with the proxy hosts NPM has.
type Status struct {
	State       string `json:"state"` // ok | mismatch | missing | none
	ProxyHostID int    `json:"proxyHostId,omitempty"`
	Forward     string `json:"forward,omitempty"` // what NPM forwards to
	Detail      string `json:"detail,omitempty"`
}

// Compare finds the proxy host for domain and checks it forwards to ip:port.
func Compare(hosts []ProxyHost, domain, ip string, port int) Status {
	if domain == "" {
		return Status{State: "none"}
	}
	domain = strings.ToLower(domain)
	for _, h := range hosts {
		for _, d := range h.DomainNames {
			if strings.ToLower(d) != domain {
				continue
			}
			fwd := fmt.Sprintf("%s://%s:%d", h.ForwardScheme, h.ForwardHost, h.ForwardPort)
			st := Status{State: "ok", ProxyHostID: h.ID, Forward: fwd}
			if ip != "" && port > 0 && (h.ForwardHost != ip || h.ForwardPort != port) {
				st.State = "mismatch"
				st.Detail = fmt.Sprintf("NPM forwards to %s:%d, service is %s:%d", h.ForwardHost, h.ForwardPort, ip, port)
			}
			return st
		}
	}
	return Status{State: "missing", Detail: "no proxy host for " + domain}
}

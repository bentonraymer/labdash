package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeNPM mimics the endpoints LabDash uses.
func fakeNPM(t *testing.T) (*httptest.Server, *atomic.Int32, *map[string]any) {
	var logins atomic.Int32
	created := map[string]any{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tokens", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["identity"] != "admin@example.com" || b["secret"] != "pw" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"Invalid email or password"}}`))
			return
		}
		logins.Add(1)
		w.Write([]byte(`{"token":"tok123","expires":"2099-01-01T00:00:00Z"}`))
	})
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok123" {
				w.WriteHeader(401)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/nginx/proxy-hosts", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"id":1,"domain_names":["jellyfin.home.example.com"],"forward_scheme":"http","forward_host":"192.168.8.22","forward_port":8096,"certificate_id":3,"ssl_forced":1,"enabled":1}]`))
	}))
	mux.HandleFunc("GET /api/nginx/certificates", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"id":3,"nice_name":"wildcard","provider":"letsencrypt","domain_names":["*.home.example.com","home.example.com"],"expires_on":"2027-01-01 00:00:00"}]`))
	}))
	mux.HandleFunc("POST /api/nginx/proxy-hosts", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&created)
		w.WriteHeader(201)
		w.Write([]byte(`{"id":2,"domain_names":["grafana.home.example.com"],"forward_scheme":"http","forward_host":"192.168.8.3","forward_port":3000}`))
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &logins, &created
}

func TestClient(t *testing.T) {
	srv, logins, created := fakeNPM(t)
	c := New()
	ctx := context.Background()
	cfg := Config{URL: srv.URL + "/"}
	sec := Secret{Email: "admin@example.com", Password: "pw"}

	hosts, err := c.ProxyHosts(ctx, cfg, sec)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts: %v %v", hosts, err)
	}
	certs, err := c.Certificates(ctx, cfg, sec)
	if err != nil || len(certs) != 1 {
		t.Fatalf("certs: %v %v", certs, err)
	}
	if logins.Load() != 1 {
		t.Fatalf("token should be cached, logged in %d times", logins.Load())
	}
	ph, err := c.CreateProxyHost(ctx, cfg, sec, NewProxyHost{Domain: "Grafana.home.example.com", ForwardHost: "192.168.8.3", ForwardPort: 3000, CertificateID: 3, SSLForced: true, Websockets: true})
	if err != nil || ph.ID != 2 {
		t.Fatalf("create: %+v %v", ph, err)
	}
	got := *created
	if got["domain_names"].([]any)[0] != "grafana.home.example.com" || got["certificate_id"].(float64) != 3 || got["ssl_forced"] != true || got["allow_websocket_upgrade"] != true {
		t.Fatalf("create body: %v", got)
	}

	if _, err := New().ProxyHosts(ctx, cfg, Secret{Email: "admin@example.com", Password: "nope"}); err == nil || err.Error() != "NPM login failed: Invalid email or password" {
		t.Fatalf("bad login error: %v", err)
	}
	if _, err := New().ProxyHosts(ctx, Config{URL: "ftp://x"}, sec); err == nil {
		t.Fatal("bad URL should fail")
	}
}

func TestCompareAndCerts(t *testing.T) {
	hosts := []ProxyHost{{ID: 1, DomainNames: []string{"Jellyfin.home.example.com"}, ForwardScheme: "http", ForwardHost: "192.168.8.22", ForwardPort: 8096}}
	if s := Compare(hosts, "jellyfin.home.example.com", "192.168.8.22", 8096); s.State != "ok" || s.ProxyHostID != 1 {
		t.Errorf("ok: %+v", s)
	}
	if s := Compare(hosts, "jellyfin.home.example.com", "192.168.8.30", 8096); s.State != "mismatch" {
		t.Errorf("mismatch: %+v", s)
	}
	if s := Compare(hosts, "radarr.home.example.com", "", 0); s.State != "missing" {
		t.Errorf("missing: %+v", s)
	}
	if s := Compare(hosts, "", "", 0); s.State != "none" {
		t.Errorf("none: %+v", s)
	}
	wild := Certificate{DomainNames: []string{"*.home.example.com"}}
	if !CertCovers(wild, "grafana.home.example.com") || CertCovers(wild, "a.b.home.example.com") || CertCovers(wild, "home.example.com") {
		t.Error("wildcard coverage wrong")
	}
}

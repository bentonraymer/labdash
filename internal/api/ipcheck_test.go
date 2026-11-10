package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

// A port found by a scan (or noted by hand) isn't a conflict: it's usually
// the very service being added. Only another service's port is.
func TestIPCheckPortKinds(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &Server{DB: d}
	for _, q := range []string{
		`INSERT INTO services(id, name, direct_ip, direct_port) VALUES (1, 'Jellyfin', '192.168.8.22', 8096)`,
		`INSERT INTO port_claims(ip, port, proto, label) VALUES ('192.168.8.22', 9000, 'tcp', 'portainer')`,
		`INSERT INTO devices(id, key, ip, is_up, first_seen, last_seen) VALUES (1, 'ip:192.168.8.22', '192.168.8.22', 1, 'x', 'x')`,
		`INSERT INTO device_ports(device_id, port, state, service_name, product, http_title, last_seen) VALUES (1, 3000, 'open', 'http', 'Grafana', 'Grafana', 'x')`,
		`INSERT INTO device_ports(device_id, port, state, service_name, last_seen) VALUES (1, 4000, 'closed', 'http', 'x')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	check := func(port string) map[string]any {
		rec := httptest.NewRecorder()
		s.handleIPCheck(rec, httptest.NewRequest("GET", "/api/ips/check?ip=192.168.8.22&port="+port, nil))
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if o := check("8096"); o["portOwner"] != "Jellyfin" {
		t.Errorf("8096 should conflict with Jellyfin: %v", o)
	}
	if o := check("9000"); o["portOwner"] != nil || o["portClaim"] != "portainer" {
		t.Errorf("9000 is a note, not a conflict: %v", o)
	}
	if o := check("3000"); o["portOwner"] != nil || o["portSeen"] == nil {
		t.Errorf("3000 was seen by a scan, not a conflict: %v", o)
	}
	if o := check("4000"); o["portOwner"] != nil || o["portSeen"] != nil || o["portClaim"] != nil {
		t.Errorf("4000 is closed, so free: %v", o)
	}
	// Editing Jellyfin itself doesn't conflict with its own port.
	rec := httptest.NewRecorder()
	s.handleIPCheck(rec, httptest.NewRequest("GET", "/api/ips/check?ip=192.168.8.22&port=8096&excludeService=1", nil))
	var o map[string]any
	json.Unmarshal(rec.Body.Bytes(), &o)
	if o["portOwner"] != nil {
		t.Errorf("own port reported as conflict: %v", o)
	}
}

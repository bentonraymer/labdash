package vuln

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestScanTarget(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	cve := newTestDB(t)
	if err := cve.Import(ctx, strings.NewReader(feed)); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(q, err)
		}
	}
	mustExec(`INSERT INTO scan_targets(id, cidr) VALUES (1, '192.168.8.0/24')`)
	mustExec(`INSERT INTO devices(id, key, ip, hostname, is_up, first_seen, last_seen) VALUES (1, 'ip:192.168.8.50', '192.168.8.50', 'nas', 1, 'x', 'x')`)
	mustExec(`INSERT INTO devices(id, key, ip, is_up, first_seen, last_seen) VALUES (2, 'ip:10.0.0.5', '10.0.0.5', 1, 'x', 'x')`)
	mustExec(`INSERT INTO device_ports(device_id, port, service_name, product, version, cpe, last_seen) VALUES (1, 22, 'ssh', 'OpenSSH', '9.2p1', 'cpe:/a:openbsd:openssh:9.2p1', 'x')`)
	mustExec(`INSERT INTO device_ports(device_id, port, service_name, last_seen) VALUES (2, 22, 'ssh', 'x')`)

	s := NewService(d, cve, Nuclei{}, nil)
	var gotTargets map[string][]int
	s.RunConfig = func(ctx context.Context, targets map[string][]int, now time.Time) ([]Finding, []ServiceInfo, error) {
		gotTargets = targets
		return []Finding{{IP: "192.168.8.50", Port: 21, Source: "config", Key: "ftp-anonymous", Title: "Anonymous FTP", Severity: "high", Confidence: "high"}}, nil, nil
	}
	var alerts []string
	s.Alert = func(ctx context.Context, title, body string) { alerts = append(alerts, title+"|"+body) }
	if err := s.ScanTarget(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(gotTargets) != 1 || len(gotTargets["192.168.8.50"]) != 1 {
		t.Fatalf("targets outside the CIDR were scanned: %v", gotTargets)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM vuln_findings WHERE status = 'open'`).Scan(&n)
	if n != 3 { // 2 OpenSSH CVEs + anon FTP
		t.Fatalf("want 3 open findings, got %d", n)
	}
	var st, notes string
	var newCount int
	d.QueryRow(`SELECT status, new_count, notes FROM vuln_scans`).Scan(&st, &newCount, &notes)
	if st != "done" || newCount != 3 || !strings.Contains(notes, "") {
		t.Fatalf("scan row: %s %d %q", st, newCount, notes)
	}
	// Critical CVE + high FTP alert (medium Terrapin doesn't).
	if len(alerts) != 1 || !strings.HasPrefix(alerts[0], "🛡️ 2 new vulnerabilities (1 critical)") || !strings.Contains(alerts[0], "nas (192.168.8.50)") {
		t.Fatalf("alerts: %q", alerts)
	}
	// Re-running finds nothing new, so no alert.
	s.ScanTarget(ctx, 1)
	if len(alerts) != 1 {
		t.Fatalf("re-scan alerted again: %q", alerts)
	}
}

package vuln

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestPersistLifecycle(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	d.Exec(`INSERT INTO devices(id, key, ip, first_seen, last_seen) VALUES (1, 'ip:192.168.8.50', '192.168.8.50', 'x', 'x')`)
	devs := map[string]int64{"192.168.8.50": 1}
	ssh := Finding{IP: "192.168.8.50", Port: 22, Source: "cve", Key: "CVE-2023-38408", Title: "OpenSSH RCE", Severity: "critical", Confidence: "high"}
	ftp := Finding{IP: "192.168.8.50", Port: 21, Source: "config", Key: "ftp-anonymous", Title: "Anonymous FTP", Severity: "high", Confidence: "high"}
	web := Finding{IP: "192.168.8.50", Port: 80, Source: "nuclei", Key: "exposed-git", Title: "Exposed .git", Severity: "medium", Confidence: "high"}
	status := func(key string) string {
		var s string
		d.QueryRow(`SELECT status FROM vuln_findings WHERE key = ?`, key).Scan(&s)
		return s
	}

	fresh, err := Persist(ctx, d, devs, []int64{1}, []string{"cve", "config", "nuclei"}, []Finding{ssh, ftp, web})
	if err != nil || len(fresh) != 3 {
		t.Fatalf("first scan: %d new, %v", len(fresh), err)
	}
	d.Exec(`UPDATE vuln_findings SET status = 'accepted' WHERE key = 'ftp-anonymous'`)

	// Second scan: SSH fixed, FTP still there (accepted), nuclei didn't run.
	fresh, _ = Persist(ctx, d, devs, []int64{1}, []string{"cve", "config"}, []Finding{ftp})
	if len(fresh) != 0 || status("CVE-2023-38408") != "fixed" || status("ftp-anonymous") != "accepted" || status("exposed-git") != "open" {
		t.Fatalf("second scan: new=%d ssh=%s ftp=%s web=%s", len(fresh), status("CVE-2023-38408"), status("ftp-anonymous"), status("exposed-git"))
	}

	// Third scan: SSH regresses → reopened and reported as new.
	fresh, _ = Persist(ctx, d, devs, []int64{1}, []string{"cve", "config", "nuclei"}, []Finding{ssh, ftp})
	if len(fresh) != 1 || fresh[0].Key != "CVE-2023-38408" || status("CVE-2023-38408") != "open" || status("exposed-git") != "fixed" {
		t.Fatalf("third scan: %+v ssh=%s web=%s", fresh, status("CVE-2023-38408"), status("exposed-git"))
	}
}

func TestAlertMessage(t *testing.T) {
	fs := []Finding{
		{IP: "192.168.8.50", Port: 22, Title: "OpenSSH RCE", Severity: "critical", KEV: true, Confidence: "high"},
		{IP: "192.168.8.50", Port: 22, Title: "Backported maybe", Severity: "critical", Confidence: "low"},
		{IP: "192.168.8.51", Port: 443, Title: "TLS 1.0", Severity: "medium", Confidence: "high"},
	}
	title, body := AlertMessage(fs, "high", map[string]string{"192.168.8.50": "nas"})
	if title != "🛡️ 1 new vulnerability (1 critical)" || !strings.Contains(body, "nas (192.168.8.50):22") || !strings.Contains(body, "exploited in the wild") {
		t.Fatalf("alert: %q %q", title, body)
	}
	if title, _ := AlertMessage(fs[2:], "high", nil); title != "" {
		t.Fatal("medium shouldn't alert at a high threshold")
	}
}

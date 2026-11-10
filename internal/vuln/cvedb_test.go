package vuln

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

// A tiny feed in the fkie-cad / NVD 2.0 shape.
const feed = `{"timestamp":"x","cve_count":4,"cve_items":[
 {"id":"CVE-2023-38408","published":"2023-07-20","lastModified":"2024-01-01","vulnStatus":"Analyzed",
  "descriptions":[{"lang":"en","value":"OpenSSH ssh-agent remote code execution"}],
  "metrics":{"cvssMetricV31":[{"type":"Primary","cvssData":{"baseScore":9.8,"baseSeverity":"CRITICAL","vectorString":"CVSS:3.1/AV:N"}}]},
  "configurations":[{"nodes":[{"cpeMatch":[{"vulnerable":true,"criteria":"cpe:2.3:a:openbsd:openssh:*:*:*:*:*:*:*:*","versionEndExcluding":"9.3p2"}]}]}],
  "references":[{"url":"https://www.openssh.com/txt/release-9.3p2"}]},
 {"id":"CVE-2023-48795","published":"2023-12-18","lastModified":"2024-01-01","vulnStatus":"Analyzed",
  "descriptions":[{"lang":"en","value":"Terrapin attack"}],
  "metrics":{"cvssMetricV31":[{"type":"Primary","cvssData":{"baseScore":5.9,"baseSeverity":"MEDIUM"}}]},
  "configurations":[{"nodes":[{"cpeMatch":[{"vulnerable":true,"criteria":"cpe:2.3:a:openbsd:openssh:*:*:*:*:*:*:*:*","versionEndExcluding":"9.6"}]}]}]},
 {"id":"CVE-2021-23017","published":"2021-06-01","lastModified":"2024-01-01","vulnStatus":"Analyzed",
  "descriptions":[{"lang":"en","value":"nginx resolver off-by-one"}],
  "metrics":{"cvssMetricV31":[{"type":"Primary","cvssData":{"baseScore":7.7,"baseSeverity":"HIGH"}}]},
  "configurations":[{"nodes":[{"cpeMatch":[{"vulnerable":true,"criteria":"cpe:2.3:a:f5:nginx:*:*:*:*:*:*:*:*","versionStartIncluding":"0.6.18","versionEndExcluding":"1.20.1"}]}]}]},
 {"id":"CVE-2099-0001","published":"2099-01-01","lastModified":"2099-01-01","vulnStatus":"Rejected","descriptions":[],"metrics":{},"configurations":[]}
]}`

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := OpenDB(filepath.Join(t.TempDir(), "cve.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestImportAndMatch(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	if err := d.Import(ctx, strings.NewReader(feed)); err != nil {
		t.Fatal(err)
	}
	if st := d.Status(); st.CVEs != 3 {
		t.Fatalf("want 3 CVEs (rejected skipped), got %d", st.CVEs)
	}
	ms, err := d.Match(ctx, CPE{Part: "a", Vendor: "openbsd", Product: "openssh", Version: "9.2p1"})
	if err != nil || len(ms) != 2 || ms[0].CVE != "CVE-2023-38408" || ms[0].FixedIn != "9.3p2" {
		t.Fatalf("openssh 9.2p1: %+v %v", ms, err)
	}
	if ms, _ := d.Match(ctx, CPE{Vendor: "openbsd", Product: "openssh", Version: "9.6p1"}); len(ms) != 0 {
		t.Fatalf("openssh 9.6p1 should be clean: %+v", ms)
	}
	// nmap calls nginx igor_sysoev:nginx; NVD uses f5:nginx.
	if ms, _ := d.Match(ctx, CPE{Vendor: "igor_sysoev", Product: "nginx", Version: "1.18.0"}); len(ms) != 1 {
		t.Fatalf("nginx alias: %+v", ms)
	}
	if ms, _ := d.Match(ctx, CPE{Vendor: "igor_sysoev", Product: "nginx", Version: "1.25.3"}); len(ms) != 0 {
		t.Fatalf("nginx 1.25.3 should be clean: %+v", ms)
	}
	if ms, _ := d.Match(ctx, CPE{Vendor: "openbsd", Product: "openssh"}); ms != nil {
		t.Fatal("no version → no matches")
	}
}

func TestSyncDownloadsFeedAndKEV(t *testing.T) {
	var xzBuf bytes.Buffer
	w, _ := xz.NewWriter(&xzBuf)
	w.Write([]byte(feed))
	w.Close()
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		switch r.URL.Path {
		case "/CVE-all.json.xz", "/CVE-modified.json.xz":
			rw.Write(xzBuf.Bytes())
		case "/kev.json":
			rw.Write([]byte(`{"vulnerabilities":[{"cveID":"CVE-2023-38408"}]}`))
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	d := newTestDB(t)
	d.Base, d.KEV = srv.URL+"/", srv.URL+"/kev.json"
	ctx := context.Background()
	if err := d.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	if hits[0] != "/CVE-all.json.xz" {
		t.Fatalf("first sync must be a full import: %v", hits)
	}
	st := d.Status()
	if st.CVEs != 3 || st.KEV != 1 || st.LastFull == "" {
		t.Fatalf("status: %+v", st)
	}
	ms, _ := d.Match(ctx, CPE{Vendor: "openbsd", Product: "openssh", Version: "8.9p1"})
	if !ms[0].KEV {
		t.Fatal("KEV flag missing")
	}
	hits = nil
	if err := d.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	if hits[0] != "/CVE-modified.json.xz" {
		t.Fatalf("later syncs use the delta: %v", hits)
	}
}

package vuln

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
	_ "modernc.org/sqlite"
)

// Feed locations. The NVD mirror is fkie-cad/nvd-json-data-feeds, rebuilt
// daily from the official NVD API: a full export plus a "modified" delta.
const (
	FeedBase = "https://github.com/fkie-cad/nvd-json-data-feeds/releases/latest/download/"
	KEVURL   = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
)

const cveSchema = `
CREATE TABLE IF NOT EXISTS cves (
  id TEXT PRIMARY KEY, published TEXT, modified TEXT,
  severity TEXT, cvss REAL, vector TEXT, summary TEXT,
  refs TEXT NOT NULL DEFAULT '[]', kev INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cve_cpe (
  cve_id TEXT NOT NULL, part TEXT, vendor TEXT NOT NULL, product TEXT NOT NULL, version TEXT NOT NULL DEFAULT '',
  start_incl TEXT NOT NULL DEFAULT '', start_excl TEXT NOT NULL DEFAULT '',
  end_incl TEXT NOT NULL DEFAULT '', end_excl TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS cve_cpe_product ON cve_cpe(product, vendor);
CREATE INDEX IF NOT EXISTS cve_cpe_cve ON cve_cpe(cve_id);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// DB is the local CVE database (its own SQLite file, not part of backups).
type DB struct {
	sql  *sql.DB
	HTTP *http.Client
	Base string // feed base URL (overridable in tests)
	KEV  string

	mu       sync.Mutex
	syncing  bool
	progress string
}

func OpenDB(path string) (*DB, error) {
	d, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(2)
	if _, err := d.Exec(cveSchema); err != nil {
		return nil, err
	}
	return &DB{sql: d, HTTP: &http.Client{Timeout: 30 * time.Minute}, Base: FeedBase, KEV: KEVURL}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) meta(key string) string {
	var v string
	_ = d.sql.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v
}

func (d *DB) setMeta(key, value string) {
	_, _ = d.sql.Exec(`INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
}

// Status describes the database for the UI.
type Status struct {
	CVEs     int    `json:"cves"`
	KEV      int    `json:"kev"`
	LastSync string `json:"lastSync"`
	LastFull string `json:"lastFull"`
	Syncing  bool   `json:"syncing"`
	Progress string `json:"progress"`
	Error    string `json:"error"`
}

func (d *DB) Status() Status {
	var s Status
	_ = d.sql.QueryRow(`SELECT COUNT(*), COALESCE(SUM(kev), 0) FROM cves`).Scan(&s.CVEs, &s.KEV)
	s.LastSync, s.LastFull, s.Error = d.meta("last_sync"), d.meta("last_full"), d.meta("last_error")
	d.mu.Lock()
	s.Syncing, s.Progress = d.syncing, d.progress
	d.mu.Unlock()
	return s
}

func (d *DB) setProgress(p string) {
	d.mu.Lock()
	d.progress = p
	d.mu.Unlock()
}

// NeedsSync reports whether the last sync is older than maxAge.
func (d *DB) NeedsSync(maxAge time.Duration) bool {
	t, err := time.Parse(time.RFC3339, d.meta("last_sync"))
	return err != nil || time.Since(t) > maxAge
}

// Sync downloads the full export the first time (or when forced / stale
// beyond a week, since the delta only covers recent changes), otherwise
// the daily delta, then refreshes the CISA known-exploited flags.
func (d *DB) Sync(ctx context.Context, forceFull bool) error {
	d.mu.Lock()
	if d.syncing {
		d.mu.Unlock()
		return errors.New("a sync is already running")
	}
	d.syncing = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.syncing, d.progress = false, ""
		d.mu.Unlock()
	}()

	full := forceFull
	var n int
	_ = d.sql.QueryRow(`SELECT COUNT(*) FROM cves`).Scan(&n)
	if last, err := time.Parse(time.RFC3339, d.meta("last_sync")); n == 0 || err != nil || time.Since(last) > 7*24*time.Hour {
		full = true
	}
	if d.meta("format") != dbFormat { // stored layout changed: rebuild from scratch
		full = true
		_, _ = d.sql.ExecContext(ctx, `DELETE FROM cve_cpe`)
		_, _ = d.sql.ExecContext(ctx, `DELETE FROM cves`)
	}
	feed := "CVE-modified.json.xz"
	if full {
		feed = "CVE-all.json.xz"
	}
	d.setProgress("Downloading " + feed)
	err := d.importURL(ctx, d.Base+feed)
	if err == nil {
		d.setProgress("Updating known-exploited list (CISA KEV)")
		err = d.syncKEV(ctx)
	}
	if err != nil {
		d.setMeta("last_error", err.Error())
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	d.setMeta("last_sync", now)
	d.setMeta("last_error", "")
	if full {
		d.setMeta("last_full", now)
		d.setMeta("format", dbFormat)
		// A full import rewrites most rows; reclaim the space.
		d.setProgress("Compacting database")
		_, _ = d.sql.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
		_, _ = d.sql.ExecContext(ctx, `VACUUM`)
		_, _ = d.sql.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	return nil
}

// dbFormat versions the stored layout; bump it to force a rebuild.
const dbFormat = "2"

func (d *DB) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LabDash vulnerability database sync")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

func (d *DB) importURL(ctx context.Context, url string) error {
	body, err := d.get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	var r io.Reader = body
	if strings.HasSuffix(url, ".xz") {
		xr, err := xz.NewReader(body)
		if err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
		r = xr
	}
	return d.Import(ctx, r)
}

// ── NVD JSON (fkie-cad layout: {"cve_items": [ {NVD 2.0 cve object} ]}) ──

type nvdCPEMatch struct {
	Vulnerable bool   `json:"vulnerable"`
	Criteria   string `json:"criteria"`
	StartIncl  string `json:"versionStartIncluding"`
	StartExcl  string `json:"versionStartExcluding"`
	EndIncl    string `json:"versionEndIncluding"`
	EndExcl    string `json:"versionEndExcluding"`
}

type nvdCVE struct {
	ID           string `json:"id"`
	Published    string `json:"published"`
	LastModified string `json:"lastModified"`
	VulnStatus   string `json:"vulnStatus"`
	Descriptions []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"descriptions"`
	Metrics map[string][]struct {
		Type     string `json:"type"`
		CVSSData struct {
			BaseScore    float64 `json:"baseScore"`
			BaseSeverity string  `json:"baseSeverity"`
			Vector       string  `json:"vectorString"`
		} `json:"cvssData"`
		BaseSeverity string `json:"baseSeverity"` // v2 keeps it here
	} `json:"metrics"`
	Configurations []struct {
		Nodes []struct {
			CPEMatch []nvdCPEMatch `json:"cpeMatch"`
		} `json:"nodes"`
	} `json:"configurations"`
	References []struct {
		URL string `json:"url"`
	} `json:"references"`
}

func (c nvdCVE) score() (float64, string, string) {
	for _, k := range []string{"cvssMetricV31", "cvssMetricV30", "cvssMetricV40", "cvssMetricV2"} {
		ms := c.Metrics[k]
		if len(ms) == 0 {
			continue
		}
		m := ms[0]
		for _, x := range ms { // prefer the NVD ("Primary") score
			if x.Type == "Primary" {
				m = x
				break
			}
		}
		sev := m.CVSSData.BaseSeverity
		if sev == "" {
			sev = m.BaseSeverity
		}
		return m.CVSSData.BaseScore, strings.ToLower(sev), m.CVSSData.Vector
	}
	return 0, "", ""
}

// Import streams a feed into the database, replacing CVEs it contains.
func (d *DB) Import(ctx context.Context, r io.Reader) error {
	dec := json.NewDecoder(r)
	// Walk to the "cve_items" (or NVD API "vulnerabilities") array.
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("feed: %w", err)
		}
		if k, ok := tok.(string); ok && (k == "cve_items" || k == "vulnerabilities") {
			if _, err := dec.Token(); err != nil { // [
				return err
			}
			break
		}
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	insCVE, _ := tx.PrepareContext(ctx, `INSERT INTO cves(id, published, modified, severity, cvss, vector, summary, refs) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET published = excluded.published, modified = excluded.modified, severity = excluded.severity,
		cvss = excluded.cvss, vector = excluded.vector, summary = excluded.summary, refs = excluded.refs`)
	delCPE, _ := tx.PrepareContext(ctx, `DELETE FROM cve_cpe WHERE cve_id = ?`)
	insCPE, _ := tx.PrepareContext(ctx, `INSERT INTO cve_cpe(cve_id, part, vendor, product, version, start_incl, start_excl, end_incl, end_excl) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	count := 0
	for dec.More() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var wrap struct {
			nvdCVE
			CVE *nvdCVE `json:"cve"` // NVD API 2.0 wraps items in {"cve": {...}}
		}
		if err := dec.Decode(&wrap); err != nil {
			return fmt.Errorf("feed item %d: %w", count, err)
		}
		c := wrap.nvdCVE
		if wrap.CVE != nil {
			c = *wrap.CVE
		}
		if c.ID == "" {
			continue
		}
		if c.VulnStatus == "Rejected" {
			_, _ = tx.ExecContext(ctx, `DELETE FROM cves WHERE id = ?`, c.ID)
			_, _ = delCPE.ExecContext(ctx, c.ID)
			continue
		}
		// Only CVEs naming an application CPE can ever match a detected
		// service, so skip the rest (and OS / hardware entries) to keep the
		// database small.
		type cpeRow struct {
			cp CPE
			m  nvdCPEMatch
		}
		var cpes []cpeRow
		for _, cfg := range c.Configurations {
			for _, node := range cfg.Nodes {
				for _, m := range node.CPEMatch {
					if !m.Vulnerable {
						continue
					}
					if cp, ok := ParseCPE(m.Criteria); ok && cp.Part == "a" {
						cpes = append(cpes, cpeRow{cp, m})
					}
				}
			}
		}
		if len(cpes) == 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM cves WHERE id = ?`, c.ID); err != nil {
				return err
			}
			if _, err := delCPE.ExecContext(ctx, c.ID); err != nil {
				return err
			}
			continue
		}
		summary := ""
		for _, ds := range c.Descriptions {
			if ds.Lang == "en" {
				summary = ds.Value
				break
			}
		}
		if r := []rune(summary); len(r) > 700 {
			summary = string(r[:700]) + "…"
		}
		refs := make([]string, 0, 4)
		for i, ref := range c.References {
			if i == 4 {
				break
			}
			refs = append(refs, ref.URL)
		}
		rj, _ := json.Marshal(refs)
		score, sev, vector := c.score()
		if _, err := insCVE.ExecContext(ctx, c.ID, c.Published, c.LastModified, sev, score, vector, summary, string(rj)); err != nil {
			return err
		}
		if _, err := delCPE.ExecContext(ctx, c.ID); err != nil {
			return err
		}
		for _, r := range cpes {
			if _, err := insCPE.ExecContext(ctx, c.ID, r.cp.Part, r.cp.Vendor, r.cp.Product, r.cp.Version, r.m.StartIncl, r.m.StartExcl, r.m.EndIncl, r.m.EndExcl); err != nil {
				return err
			}
		}
		count++
		if count%5000 == 0 {
			d.setProgress(fmt.Sprintf("Imported %d CVEs…", count))
			// Commit in batches so a huge import doesn't hold one giant transaction.
			if err := tx.Commit(); err != nil {
				return err
			}
			if tx, err = d.sql.BeginTx(ctx, nil); err != nil {
				return err
			}
			insCVE, _ = tx.PrepareContext(ctx, `INSERT INTO cves(id, published, modified, severity, cvss, vector, summary, refs) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET published = excluded.published, modified = excluded.modified, severity = excluded.severity,
				cvss = excluded.cvss, vector = excluded.vector, summary = excluded.summary, refs = excluded.refs`)
			delCPE, _ = tx.PrepareContext(ctx, `DELETE FROM cve_cpe WHERE cve_id = ?`)
			insCPE, _ = tx.PrepareContext(ctx, `INSERT INTO cve_cpe(cve_id, part, vendor, product, version, start_incl, start_excl, end_incl, end_excl) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		}
	}
	return tx.Commit()
}

func (d *DB) syncKEV(ctx context.Context) error {
	body, err := d.get(ctx, d.KEV)
	if err != nil {
		return err
	}
	defer body.Close()
	var kev struct {
		Vulnerabilities []struct {
			CVEID string `json:"cveID"`
		} `json:"vulnerabilities"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 64<<20)).Decode(&kev); err != nil {
		return fmt.Errorf("KEV feed: %w", err)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE cves SET kev = 0 WHERE kev = 1`); err != nil {
		return err
	}
	for _, v := range kev.Vulnerabilities {
		if _, err := tx.ExecContext(ctx, `UPDATE cves SET kev = 1 WHERE id = ?`, v.CVEID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── matching ──────────────────────────────────────────────────────────────

// aliases maps the vendor/product nmap reports to the names NVD uses.
var aliases = map[string][][2]string{
	"igor_sysoev:nginx":                     {{"f5", "nginx"}, {"nginx", "nginx"}},
	"nginx:nginx":                           {{"f5", "nginx"}, {"nginx", "nginx"}},
	"microsoft:iis":                         {{"microsoft", "internet_information_services"}},
	"microsoft:internet_information_server": {{"microsoft", "internet_information_services"}},
	"beasts:vsftpd":                         {{"vsftpd_project", "vsftpd"}, {"beasts", "vsftpd"}},
	"matt_johnston:dropbear_ssh_server":     {{"dropbear_ssh_project", "dropbear_ssh"}},
	"mysql:mysql":                           {{"oracle", "mysql"}, {"mysql", "mysql"}},
	"redislabs:redis":                       {{"redis", "redis"}, {"redislabs", "redis"}},
	"jellyfin:jellyfin":                     {{"jellyfin", "jellyfin"}},
	"lighttpd:lighttpd":                     {{"lighttpd", "lighttpd"}},
	"samba:samba":                           {{"samba", "samba"}},
	"proftpd:proftpd":                       {{"proftpd", "proftpd"}, {"proftpd_project", "proftpd"}},
}

// Match is one CVE that applies to a product version.
type Match struct {
	CVE      string   `json:"cve"`
	Severity string   `json:"severity"`
	CVSS     float64  `json:"cvss"`
	KEV      bool     `json:"kev"`
	Summary  string   `json:"summary"`
	Refs     []string `json:"refs"`
	FixedIn  string   `json:"fixedIn"` // first version outside the vulnerable range, when NVD says
}

func inRange(v string, version, si, se, ei, ee string) bool {
	if version != "" {
		return CompareVersions(v, version) == 0
	}
	if si == "" && se == "" && ei == "" && ee == "" {
		return false // "all versions" entries are too broad to trust for a version match
	}
	if si != "" && CompareVersions(v, si) < 0 {
		return false
	}
	if se != "" && CompareVersions(v, se) <= 0 {
		return false
	}
	if ei != "" && CompareVersions(v, ei) > 0 {
		return false
	}
	if ee != "" && CompareVersions(v, ee) >= 0 {
		return false
	}
	return true
}

// Match returns the CVEs affecting a product version (deduplicated, worst first).
func (d *DB) Match(ctx context.Context, c CPE) ([]Match, error) {
	if c.Version == "" || c.Product == "" {
		return nil, nil
	}
	pairs := aliases[c.Vendor+":"+c.Product]
	if len(pairs) == 0 {
		pairs = [][2]string{{c.Vendor, c.Product}}
	}
	seen := map[string]*Match{}
	var order []string
	for _, p := range pairs {
		rows, err := d.sql.QueryContext(ctx, `SELECT m.cve_id, m.version, m.start_incl, m.start_excl, m.end_incl, m.end_excl,
			COALESCE(c.severity, ''), COALESCE(c.cvss, 0), COALESCE(c.kev, 0), COALESCE(c.summary, ''), COALESCE(c.refs, '[]')
			FROM cve_cpe m JOIN cves c ON c.id = m.cve_id WHERE m.product = ? AND m.vendor = ?`, p[1], p[0])
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, ver, si, se, ei, ee, sev, summary, refs string
			var cvss float64
			var kev bool
			if err := rows.Scan(&id, &ver, &si, &se, &ei, &ee, &sev, &cvss, &kev, &summary, &refs); err != nil {
				rows.Close()
				return nil, err
			}
			if !inRange(c.Version, ver, si, se, ei, ee) {
				continue
			}
			if m, ok := seen[id]; ok {
				if m.FixedIn == "" && ee != "" {
					m.FixedIn = ee
				}
				continue
			}
			m := &Match{CVE: id, Severity: sev, CVSS: cvss, KEV: kev, Summary: summary, FixedIn: ee}
			_ = json.Unmarshal([]byte(refs), &m.Refs)
			seen[id] = m
			order = append(order, id)
		}
		rows.Close()
	}
	out := make([]Match, 0, len(order))
	for _, id := range order {
		out = append(out, *seen[id])
	}
	sortMatches(out)
	return out, nil
}

func sortMatches(ms []Match) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && (ms[j].CVSS > ms[j-1].CVSS || (ms[j].KEV && !ms[j-1].KEV)); j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

package vuln

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/scanner"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ScanStatus is the live state of the vulnerability scanner.
type ScanStatus struct {
	Running  bool    `json:"running"`
	TargetID int64   `json:"targetId"`
	CIDR     string  `json:"cidr"`
	Phase    string  `json:"phase"`
	Started  string  `json:"started"`
	Queued   []int64 `json:"queued"`
	// Progress: Step of Steps (1 config & TLS, 2 CVEs, 3 web checks),
	// Percent within the step (-1 unknown), when the step started, a
	// detail line, devices / web URLs in scope and findings so far.
	Step        int     `json:"step"`
	Steps       int     `json:"steps"`
	Percent     float64 `json:"percent"`
	StepStarted string  `json:"stepStarted"`
	Detail      string  `json:"detail"`
	Devices     int     `json:"devices"`
	URLs        int     `json:"urls"`
	Found       int     `json:"found"`
}

// Steps of a vulnerability scan, in order.
var ScanSteps = []string{"Config & TLS checks", "CVE matching", "Web checks"}

// Service schedules vulnerability scans (one at a time) and keeps the CVE
// database and Nuclei templates fresh.
type Service struct {
	DB     *sql.DB
	CVE    *DB
	Nuclei Nuclei
	Hub    *events.Hub
	// Settings
	Enabled  func(ctx context.Context) bool
	AlertMin func(ctx context.Context) string
	// Alert sends a notification (title, HTML body).
	Alert func(ctx context.Context, title, body string)

	// Swappable for tests.
	RunConfig func(ctx context.Context, targets map[string][]int, now time.Time, onProgress func(Progress)) ([]Finding, []ServiceInfo, error)

	started time.Time
	mu      sync.Mutex
	status  ScanStatus
	queue   []int64
	wake    chan struct{}
	tmplMu  sync.Mutex
	tmplErr string
	lastPub time.Time
}

func NewService(db *sql.DB, cve *DB, nuclei Nuclei, hub *events.Hub) *Service {
	return &Service{DB: db, CVE: cve, Nuclei: nuclei, Hub: hub, RunConfig: RunConfigScan,
		started: time.Now(), wake: make(chan struct{}, 1)}
}

func (s *Service) Status() ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Queued = append([]int64{}, s.queue...)
	return st
}

func (s *Service) publish() {
	if s.Hub != nil {
		s.Hub.Publish("vuln", s.Status())
	}
}

func (s *Service) setPhase(p string) {
	s.mu.Lock()
	s.status.Phase = p
	s.mu.Unlock()
	s.publish()
}

// startStep moves to step n (1-based) and publishes right away.
func (s *Service) startStep(n int, detail string) {
	s.mu.Lock()
	s.status.Step, s.status.Steps = n, len(ScanSteps)
	s.status.Phase = ScanSteps[n-1]
	s.status.Percent, s.status.Detail = 0, detail
	s.status.StepStarted = time.Now().UTC().Format(tsFormat)
	s.lastPub = time.Now()
	s.mu.Unlock()
	s.publish()
}

// progress updates the current step (published at most every 2s).
func (s *Service) progress(p Progress, foundBase int) {
	s.mu.Lock()
	if p.Percent >= 0 && p.Percent >= s.status.Percent {
		s.status.Percent = p.Percent
	}
	if p.Detail != "" {
		s.status.Detail = p.Detail
	}
	if p.Matched > 0 {
		s.status.Found = foundBase + p.Matched
	}
	pub := time.Since(s.lastPub) > 2*time.Second
	if pub {
		s.lastPub = time.Now()
	}
	s.mu.Unlock()
	if pub {
		s.publish()
	}
}

func (s *Service) setFound(n int) {
	s.mu.Lock()
	s.status.Found = n
	s.mu.Unlock()
}

// Enqueue requests a vulnerability scan of a target.
func (s *Service) Enqueue(targetID int64) {
	s.mu.Lock()
	if s.status.Running && s.status.TargetID == targetID {
		s.mu.Unlock()
		return
	}
	for _, q := range s.queue {
		if q == targetID {
			s.mu.Unlock()
			return
		}
	}
	s.queue = append(s.queue, targetID)
	s.mu.Unlock()
	s.publish()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run drains the queue, enqueues due targets, and runs daily maintenance.
func (s *Service) Run(ctx context.Context) {
	// Scans that were running when LabDash stopped never finished.
	_, _ = s.DB.ExecContext(ctx, `UPDATE vuln_scans SET status = 'failed', finished_at = COALESCE(finished_at, started_at),
		error = 'Interrupted (LabDash restarted)' WHERE status = 'running'`)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	go s.maintain(ctx)
	for {
		s.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
			if s.Enabled == nil || s.Enabled(ctx) {
				s.enqueueDue(ctx)
			}
		}
	}
}

// maintain syncs the CVE database and Nuclei templates about once a day.
func (s *Service) maintain(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	select { // let startup settle first
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	for {
		if s.Enabled == nil || s.Enabled(ctx) {
			if s.CVE != nil && s.CVE.NeedsSync(23*time.Hour) {
				if err := s.CVE.Sync(ctx, false); err != nil && ctx.Err() == nil {
					slog.Warn("CVE database sync failed", "err", err)
				}
			}
			if s.templatesAge() > 23*time.Hour {
				s.UpdateTemplates(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) stampPath() string { return filepath.Join(s.Nuclei.TemplatesDir, ".labdash-updated") }

func (s *Service) templatesAge() time.Duration {
	if !s.Nuclei.Available() {
		return 0
	}
	fi, err := os.Stat(s.stampPath())
	if err != nil {
		return time.Duration(1<<62 - 1)
	}
	return time.Since(fi.ModTime())
}

// TemplatesUpdated is when Nuclei templates were last refreshed ("" = never).
func (s *Service) TemplatesUpdated() string {
	if !s.Nuclei.HasTemplates() {
		return ""
	}
	fi, err := os.Stat(s.stampPath())
	if err != nil {
		return ""
	}
	return fi.ModTime().UTC().Format(tsFormat)
}

// TemplatesError is the last template update failure, if any.
func (s *Service) TemplatesError() string {
	s.tmplMu.Lock()
	defer s.tmplMu.Unlock()
	return s.tmplErr
}

// UpdateTemplates refreshes Nuclei templates (serialised).
func (s *Service) UpdateTemplates(ctx context.Context) error {
	if !s.Nuclei.Available() {
		return errors.New("nuclei is not installed")
	}
	s.tmplMu.Lock()
	defer s.tmplMu.Unlock()
	err := s.Nuclei.UpdateTemplates(ctx)
	if err != nil {
		s.tmplErr = err.Error()
		slog.Warn("nuclei template update failed", "err", err)
		return err
	}
	s.tmplErr = ""
	_ = os.WriteFile(s.stampPath(), []byte(time.Now().UTC().Format(tsFormat)), 0o640)
	return nil
}

func (s *Service) enqueueDue(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, vuln_schedule, vuln_last_run FROM scan_targets WHERE enabled = 1 AND vuln_enabled = 1 AND vuln_schedule != ''`)
	if err != nil {
		return
	}
	var due []int64
	now := time.Now()
	for rows.Next() {
		var id int64
		var sched string
		var last sql.NullString
		if rows.Scan(&id, &sched, &last) != nil {
			continue
		}
		sc, err := cronParser.Parse(sched)
		if err != nil {
			continue
		}
		from := s.started
		if last.Valid {
			if t, err := time.Parse(tsFormat, last.String); err == nil {
				from = t.Local()
			}
		}
		if !sc.Next(from).After(now) {
			due = append(due, id)
		}
	}
	rows.Close()
	for _, id := range due {
		s.Enqueue(id)
	}
}

func (s *Service) drain(ctx context.Context) {
	for ctx.Err() == nil {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		id := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		if err := s.ScanTarget(ctx, id); err != nil && ctx.Err() == nil {
			slog.Warn("vulnerability scan failed", "target", id, "err", err)
		}
	}
}

type portRow struct {
	port                         int
	name, product, version, cpes string
}

// ScanTarget runs CVE, config and web checks against the devices a
// discovery scan found in the target's range (using their known open ports).
func (s *Service) ScanTarget(ctx context.Context, targetID int64) error {
	var cidr string
	err := s.DB.QueryRowContext(ctx, `SELECT cidr FROM scan_targets WHERE id = ?`, targetID).Scan(&cidr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC().Format(tsFormat)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO vuln_scans(target_id, cidr, started_at) VALUES (?, ?, ?)`, targetID, cidr, startedAt)
	if err != nil {
		return err
	}
	scanID, _ := res.LastInsertId()
	s.mu.Lock()
	s.status = ScanStatus{Running: true, TargetID: targetID, CIDR: cidr, Phase: "Loading devices", Started: startedAt}
	s.mu.Unlock()
	s.publish()
	defer func() {
		s.mu.Lock()
		s.status = ScanStatus{}
		s.mu.Unlock()
		s.publish()
		if s.Hub != nil {
			s.Hub.Publish("vulns", map[string]any{"targetId": targetID})
		}
	}()

	fail := func(err error) error {
		_, _ = s.DB.ExecContext(context.Background(), `UPDATE vuln_scans SET status = 'failed', finished_at = ?, error = ? WHERE id = ?`,
			time.Now().UTC().Format(tsFormat), err.Error(), scanID)
		return err
	}

	// Devices in range that are up, with their known open TCP ports.
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		if a, aerr := netip.ParseAddr(cidr); aerr == nil {
			prefix = netip.PrefixFrom(a, a.BitLen())
		} else {
			return fail(fmt.Errorf("bad CIDR %q", cidr))
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id, d.ip, COALESCE(NULLIF(d.custom_name, ''), NULLIF(d.router_name, ''), d.hostname),
		p.port, p.service_name, p.product, p.version, p.cpe
		FROM devices d JOIN device_ports p ON p.device_id = d.id
		WHERE d.is_up = 1 AND d.ignored = 0 AND d.ip != '' AND p.state = 'open' AND p.proto = 'tcp'`)
	if err != nil {
		return fail(err)
	}
	deviceByIP := map[string]int64{}
	names := map[string]string{}
	ports := map[string]map[int]portRow{}
	for rows.Next() {
		var id int64
		var ip, name string
		var pr portRow
		if err := rows.Scan(&id, &ip, &name, &pr.port, &pr.name, &pr.product, &pr.version, &pr.cpes); err != nil {
			continue
		}
		a, err := netip.ParseAddr(ip)
		if err != nil || !prefix.Contains(a) {
			continue
		}
		deviceByIP[ip], names[ip] = id, name
		if ports[ip] == nil {
			ports[ip] = map[int]portRow{}
		}
		ports[ip][pr.port] = pr
	}
	rows.Close()
	if len(deviceByIP) == 0 {
		_, _ = s.DB.ExecContext(ctx, `UPDATE vuln_scans SET status = 'done', finished_at = ?, notes = ? WHERE id = ?`,
			time.Now().UTC().Format(tsFormat), "No online devices with open ports in this range yet. Run a network scan first.", scanID)
		_, _ = s.DB.ExecContext(ctx, `UPDATE scan_targets SET vuln_last_run = ? WHERE id = ?`, time.Now().UTC().Format(tsFormat), targetID)
		return nil
	}

	var findings []Finding
	var ran, notes []string
	s.mu.Lock()
	s.status.Devices = len(deviceByIP)
	s.mu.Unlock()

	// 1. Config / TLS checks (+ fresh -sV versions for CVE matching).
	s.startStep(1, fmt.Sprintf("Starting nmap on %d device%s", len(deviceByIP), map[bool]string{true: "", false: "s"}[len(deviceByIP) == 1]))
	targets := map[string][]int{}
	for ip, ps := range ports {
		for p := range ps {
			targets[ip] = append(targets[ip], p)
		}
	}
	cfgFindings, svcs, cfgErr := s.RunConfig(ctx, targets, time.Now(), func(p Progress) { s.progress(p, 0) })
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if cfgErr != nil {
		notes = append(notes, "Config checks failed: "+cfgErr.Error())
	} else {
		ran = append(ran, "config")
		findings = append(findings, cfgFindings...)
	}
	s.setFound(len(findings))
	// Merge freshly detected versions over what discovery stored.
	for _, sv := range svcs {
		if ports[sv.IP] == nil {
			continue
		}
		pr := ports[sv.IP][sv.Port]
		pr.port = sv.Port
		if sv.Name != "" {
			pr.name = sv.Name
		}
		if sv.Product != "" {
			pr.product, pr.version = sv.Product, sv.Version
		}
		if len(sv.CPEs) > 0 {
			pr.cpes = strings.Join(sv.CPEs, " ")
		}
		ports[sv.IP][sv.Port] = pr
	}

	// 2. CVEs by detected version.
	if s.CVE != nil && s.CVE.Status().CVEs > 0 {
		s.startStep(2, "Matching detected service versions against the CVE database")
		cves, err := s.matchCVEs(ctx, ports)
		if err != nil {
			notes = append(notes, "CVE matching failed: "+err.Error())
		} else {
			ran = append(ran, "cve")
			findings = append(findings, cves...)
		}
		s.setFound(len(findings))
	} else {
		notes = append(notes, "CVE database isn't downloaded yet, so version-based CVE checks were skipped.")
	}

	// 3. Web checks with Nuclei.
	var urls []string
	for ip, ps := range ports {
		for _, pr := range ps {
			if scheme, ok := scanner.IsWebPort(scanner.Port{Port: pr.port, Service: pr.name, TLS: strings.Contains(pr.name, "ssl") || strings.Contains(pr.name, "https")}); ok {
				urls = append(urls, fmt.Sprintf("%s://%s:%d", scheme, ip, pr.port))
			}
		}
	}
	sort.Strings(urls)
	s.mu.Lock()
	s.status.URLs = len(urls)
	s.mu.Unlock()
	switch {
	case len(urls) == 0:
		ran = append(ran, "nuclei") // nothing to check means nothing remains open
	case !s.Nuclei.Available():
		notes = append(notes, "Nuclei isn't installed, so web checks were skipped.")
	case !s.Nuclei.HasTemplates():
		notes = append(notes, "Nuclei templates haven't been downloaded yet, so web checks were skipped.")
	default:
		s.startStep(3, fmt.Sprintf("Starting Nuclei on %d web URL%s", len(urls), map[bool]string{true: "", false: "s"}[len(urls) == 1]))
		base := len(findings)
		web, err := s.Nuclei.Run(ctx, urls, func(p Progress) { s.progress(p, base) })
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if err != nil {
			notes = append(notes, "Web checks failed: "+err.Error())
		} else {
			ran = append(ran, "nuclei")
			findings = append(findings, web...)
		}
	}

	s.mu.Lock()
	s.status.Step, s.status.Steps, s.status.Percent = len(ScanSteps), len(ScanSteps), 100
	s.status.Found = len(findings)
	s.mu.Unlock()
	s.setPhase("Saving findings")
	scanned := make([]int64, 0, len(deviceByIP))
	for _, id := range deviceByIP {
		scanned = append(scanned, id)
	}
	fresh, err := Persist(ctx, s.DB, deviceByIP, scanned, ran, findings)
	if err != nil {
		return fail(err)
	}
	_ = RecordHistory(ctx, s.DB, time.Now())
	var open int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM vuln_findings WHERE status = 'open' AND device_id IN (`+placeholders(len(scanned))+`)`, int64sAny(scanned)...).Scan(&open)
	finished := time.Now().UTC().Format(tsFormat)
	_, _ = s.DB.ExecContext(ctx, `UPDATE vuln_scans SET status = 'done', finished_at = ?, devices = ?, new_count = ?, open_count = ?, notes = ? WHERE id = ?`,
		finished, len(deviceByIP), len(fresh), open, strings.Join(notes, "\n"), scanID)
	_, _ = s.DB.ExecContext(ctx, `UPDATE scan_targets SET vuln_last_run = ? WHERE id = ?`, finished, targetID)
	slog.Info("vulnerability scan done", "cidr", cidr, "devices", len(deviceByIP), "findings", len(findings), "new", len(fresh), "open", open)

	if s.Alert != nil && len(fresh) > 0 {
		min := "high"
		if s.AlertMin != nil {
			min = s.AlertMin(ctx)
		}
		if min != "off" {
			if title, body := AlertMessage(fresh, min, names); title != "" {
				s.Alert(ctx, title, body)
			}
		}
	}
	return nil
}

func int64sAny(xs []int64) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func (s *Service) matchCVEs(ctx context.Context, ports map[string]map[int]portRow) ([]Finding, error) {
	var out []Finding
	for ip, ps := range ports {
		for _, pr := range ps {
			seen := map[string]bool{}
			for _, raw := range strings.Fields(pr.cpes) {
				c, ok := ParseCPE(raw)
				if !ok || c.Part != "a" {
					continue
				}
				if c.Version == "" { // nmap sometimes leaves the version out of the CPE
					if f := strings.Fields(pr.version); len(f) > 0 {
						c.Version = f[0]
					}
				}
				ms, err := s.CVE.Match(ctx, c)
				if err != nil {
					return nil, err
				}
				conf := "high"
				if distroHint(pr.product + " " + pr.version) {
					conf = "low"
				}
				label := strings.TrimSpace(pr.product + " " + c.Version)
				if pr.product == "" {
					label = c.Product + " " + c.Version
				}
				for _, m := range ms {
					if seen[m.CVE] {
						continue
					}
					seen[m.CVE] = true
					refs := m.Refs
					if len(refs) > 6 {
						refs = refs[:6]
					}
					refs = append([]string{"https://nvd.nist.gov/vuln/detail/" + m.CVE}, refs...)
					ev := fmt.Sprintf("Detected %s (%s)", strings.TrimSpace(pr.product+" "+pr.version), raw)
					if conf == "low" {
						ev += ". This looks like a distro package: distros often backport fixes without changing the version, so check your distro's security tracker."
					}
					out = append(out, Finding{IP: ip, Port: pr.port, Source: "cve", Key: m.CVE,
						Title: m.CVE + " in " + label, Severity: orInfo(m.Severity), CVSS: m.CVSS, KEV: m.KEV,
						Description: m.Summary, Evidence: ev, References: refs, FixedIn: m.FixedIn, Confidence: conf})
				}
			}
		}
	}
	return out, nil
}

func orInfo(s string) string {
	if s == "" || s == "none" {
		return "info"
	}
	return s
}

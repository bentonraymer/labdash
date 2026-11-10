package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type VulnFinding struct {
	ID          int64    `json:"id"`
	DeviceID    int64    `json:"deviceId"`
	DeviceName  string   `json:"deviceName"`
	IP          string   `json:"ip"`
	Port        int      `json:"port"`
	Source      string   `json:"source"`
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Severity    string   `json:"severity"`
	CVSS        float64  `json:"cvss"`
	KEV         bool     `json:"kev"`
	Description string   `json:"description"`
	Evidence    string   `json:"evidence"`
	References  []string `json:"references"`
	FixedIn     string   `json:"fixedIn"`
	Confidence  string   `json:"confidence"`
	Status      string   `json:"status"`
	Note        string   `json:"note"`
	FirstSeen   string   `json:"firstSeen"`
	LastSeen    string   `json:"lastSeen"`
	ResolvedAt  *string  `json:"resolvedAt"`
}

// GET /api/vulns?status=open|fixed|accepted|ignored|all&device=ID&ip=x
func (s *Server) handleListVulns(w http.ResponseWriter, r *http.Request) {
	q := `SELECT f.id, f.device_id, COALESCE(NULLIF(d.custom_name, ''), NULLIF(d.router_name, ''), NULLIF(d.hostname, ''), ''),
		f.ip, f.port, f.source, f.key, f.title, f.severity, f.cvss, f.kev, f.description, f.evidence, f.refs, f.fixed_in,
		f.confidence, f.status, f.note, f.first_seen, f.last_seen, f.resolved_at
		FROM vuln_findings f JOIN devices d ON d.id = f.device_id WHERE 1=1`
	var args []any
	switch st := r.URL.Query().Get("status"); st {
	case "", "open":
		q += ` AND f.status = 'open'`
	case "all":
	case "fixed", "accepted", "ignored":
		q += ` AND f.status = ?`
		args = append(args, st)
	default:
		writeErr(w, http.StatusBadRequest, "bad status")
		return
	}
	if dev := r.URL.Query().Get("device"); dev != "" {
		q += ` AND f.device_id = ?`
		args = append(args, dev)
	}
	if ip := r.URL.Query().Get("ip"); ip != "" {
		q += ` AND f.ip = ?`
		args = append(args, ip)
	}
	q += ` ORDER BY CASE f.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END,
		f.kev DESC, f.cvss DESC, f.ip, f.port LIMIT 5000`
	rows, err := s.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	out := []VulnFinding{}
	for rows.Next() {
		var f VulnFinding
		var refs string
		var resolved sql.NullString
		if err := rows.Scan(&f.ID, &f.DeviceID, &f.DeviceName, &f.IP, &f.Port, &f.Source, &f.Key, &f.Title, &f.Severity, &f.CVSS, &f.KEV,
			&f.Description, &f.Evidence, &refs, &f.FixedIn, &f.Confidence, &f.Status, &f.Note, &f.FirstSeen, &f.LastSeen, &resolved); err != nil {
			serverErr(w, err)
			return
		}
		_ = json.Unmarshal([]byte(refs), &f.References)
		if f.References == nil {
			f.References = []string{}
		}
		if resolved.Valid {
			f.ResolvedAt = &resolved.String
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, out)
}

// PATCH /api/vulns/{id} {status?: open|accepted|ignored, note?}
func (s *Server) handleUpdateVuln(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in struct {
		Status *string `json:"status"`
		Note   *string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Status != nil && oneOf("open", "accepted", "ignored")(*in.Status) != nil {
		writeErr(w, http.StatusBadRequest, "status must be open, accepted or ignored")
		return
	}
	if in.Note != nil && len(*in.Note) > 2000 {
		writeErr(w, http.StatusBadRequest, "note is too long")
		return
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := s.DB.ExecContext(r.Context(), `UPDATE vuln_findings SET
		status = COALESCE(?, status),
		resolved_at = CASE WHEN ? IS NULL THEN resolved_at WHEN ? = 'open' THEN NULL ELSE ? END,
		note = COALESCE(?, note) WHERE id = ?`, in.Status, in.Status, in.Status, now, in.Note, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.Events.Publish("vulns", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type VulnScanRow struct {
	ID         int64   `json:"id"`
	TargetID   *int64  `json:"targetId"`
	CIDR       string  `json:"cidr"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
	Status     string  `json:"status"`
	Devices    int     `json:"devices"`
	NewCount   int     `json:"newCount"`
	OpenCount  int     `json:"openCount"`
	Notes      string  `json:"notes"`
	Error      string  `json:"error"`
}

// GET /api/vulns/status: engine state, data freshness, recent scans, counts.
func (s *Server) handleVulnStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"available": s.Vuln != nil}
	if s.Vuln == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["scan"] = s.Vuln.Status()
	if s.Vuln.CVE != nil {
		out["cveDb"] = s.Vuln.CVE.Status()
	}
	nuc := map[string]any{"installed": s.Vuln.Nuclei.Available(), "templatesUpdated": s.Vuln.TemplatesUpdated(), "error": s.Vuln.TemplatesError()}
	if s.Vuln.Nuclei.Available() {
		nuc["version"] = s.nucleiVersion(ctx)
	}
	out["nuclei"] = nuc

	counts := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT severity, COUNT(*) FROM vuln_findings WHERE status = 'open' GROUP BY severity`)
	if err == nil {
		for rows.Next() {
			var sev string
			var n int
			if rows.Scan(&sev, &n) == nil {
				counts[sev] = n
			}
		}
		rows.Close()
	}
	out["open"] = counts
	var kev int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM vuln_findings WHERE status = 'open' AND kev = 1`).Scan(&kev)
	out["kev"] = kev

	scans := []VulnScanRow{}
	rows, err = s.DB.QueryContext(ctx, `SELECT id, target_id, cidr, started_at, finished_at, status, devices, new_count, open_count, notes, error
		FROM vuln_scans ORDER BY id DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var v VulnScanRow
			var tid sql.NullInt64
			var fin sql.NullString
			if rows.Scan(&v.ID, &tid, &v.CIDR, &v.StartedAt, &fin, &v.Status, &v.Devices, &v.NewCount, &v.OpenCount, &v.Notes, &v.Error) == nil {
				if tid.Valid {
					v.TargetID = &tid.Int64
				}
				if fin.Valid {
					v.FinishedAt = &fin.String
				}
				scans = append(scans, v)
			}
		}
		rows.Close()
	}
	out["recent"] = scans
	writeJSON(w, http.StatusOK, out)
}

// nucleiVersion caches `nuclei -version` (it takes a moment to run).
func (s *Server) nucleiVersion(ctx context.Context) string {
	s.nucleiVer.Do(func() { s.nucleiVerStr = s.Vuln.Nuclei.Version(ctx) })
	return s.nucleiVerStr
}

// POST /api/scan-targets/{id}/vuln-scan
func (s *Server) handleRunVulnScan(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Vuln == nil {
		writeErr(w, http.StatusServiceUnavailable, "vulnerability scanning is unavailable")
		return
	}
	s.Vuln.Enqueue(id)
	writeJSON(w, http.StatusAccepted, s.Vuln.Status())
}

// POST /api/vulns/sync {full?: bool}: refresh the CVE DB and Nuclei templates now.
func (s *Server) handleVulnSync(w http.ResponseWriter, r *http.Request) {
	if s.Vuln == nil || s.Vuln.CVE == nil {
		writeErr(w, http.StatusServiceUnavailable, "vulnerability scanning is unavailable")
		return
	}
	var in struct {
		Full bool   `json:"full"`
		What string `json:"what"` // "", "cve", "templates"
	}
	_ = readJSON(r, &in)
	if s.Vuln.CVE.Status().Syncing && in.What != "templates" {
		writeErr(w, http.StatusConflict, "a CVE sync is already running")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if in.What != "templates" {
			_ = s.Vuln.CVE.Sync(ctx, in.Full)
		}
		if in.What != "cve" && s.Vuln.Nuclei.Available() {
			_ = s.Vuln.UpdateTemplates(ctx)
		}
		s.Events.Publish("vulns", map[string]any{"synced": true})
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

package vuln

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const tsFormat = "2006-01-02T15:04:05.000Z"

// Persist merges one scan's findings into vuln_findings.
//
//   - new findings are opened;
//   - previously fixed ones that reappear are reopened (and count as new);
//   - accepted/ignored ones stay that way, but their details refresh;
//   - open findings on scanned devices that weren't seen again are marked
//     fixed, but only for sources that actually ran this time (if Nuclei was
//     unavailable, its old findings are left alone).
//
// It returns the findings that are newly open (for alerting).
func Persist(ctx context.Context, db *sql.DB, deviceByIP map[string]int64, scanned []int64, sourcesRan []string, findings []Finding) ([]Finding, error) {
	now := time.Now().UTC().Format(tsFormat)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	seen := map[int64]bool{}
	var fresh []Finding
	for _, f := range findings {
		dev, ok := deviceByIP[f.IP]
		if !ok {
			continue
		}
		refs, _ := json.Marshal(nonNil(f.References))
		var id int64
		var status string
		err := tx.QueryRowContext(ctx, `SELECT id, status FROM vuln_findings WHERE device_id = ? AND port = ? AND source = ? AND key = ?`,
			dev, f.Port, f.Source, f.Key).Scan(&id, &status)
		switch {
		case err == sql.ErrNoRows:
			res, err := tx.ExecContext(ctx, `INSERT INTO vuln_findings(device_id, ip, port, source, key, title, severity, cvss, kev, description,
				evidence, refs, fixed_in, confidence, status, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
				dev, f.IP, f.Port, f.Source, f.Key, f.Title, f.Severity, f.CVSS, f.KEV, f.Description, f.Evidence, string(refs), f.FixedIn, f.Confidence, now, now)
			if err != nil {
				return nil, err
			}
			id, _ = res.LastInsertId()
			fresh = append(fresh, f)
		case err != nil:
			return nil, err
		default:
			newStatus := status
			if status == "fixed" {
				newStatus = "open"
				fresh = append(fresh, f)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE vuln_findings SET ip = ?, title = ?, severity = ?, cvss = ?, kev = ?, description = ?,
				evidence = ?, refs = ?, fixed_in = ?, confidence = ?, status = ?, last_seen = ?,
				resolved_at = CASE WHEN ? = 'open' AND status = 'fixed' THEN NULL ELSE resolved_at END WHERE id = ?`,
				f.IP, f.Title, f.Severity, f.CVSS, f.KEV, f.Description, f.Evidence, string(refs), f.FixedIn, f.Confidence, newStatus, now, newStatus, id); err != nil {
				return nil, err
			}
		}
		seen[id] = true
	}

	// Mark disappeared findings fixed (scanned devices, sources that ran).
	if len(scanned) > 0 && len(sourcesRan) > 0 {
		q := `SELECT id FROM vuln_findings WHERE status = 'open' AND device_id IN (` + placeholders(len(scanned)) + `) AND source IN (` + placeholders(len(sourcesRan)) + `)`
		args := make([]any, 0, len(scanned)+len(sourcesRan))
		for _, d := range scanned {
			args = append(args, d)
		}
		for _, s := range sourcesRan {
			args = append(args, s)
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		var gone []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil && !seen[id] {
				gone = append(gone, id)
			}
		}
		rows.Close()
		for _, id := range gone {
			if _, err := tx.ExecContext(ctx, `UPDATE vuln_findings SET status = 'fixed', resolved_at = ? WHERE id = ?`, now, id); err != nil {
				return nil, err
			}
		}
	}
	return fresh, tx.Commit()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// AlertMessage summarizes new findings at or above minSeverity
// ("" when there's nothing to say).
func AlertMessage(fresh []Finding, minSeverity string, names map[string]string) (title, body string) {
	min := SeverityRank(minSeverity)
	var hits []Finding
	for _, f := range fresh {
		if SeverityRank(f.Severity) >= min && f.Confidence != "low" {
			hits = append(hits, f)
		}
	}
	if len(hits) == 0 {
		return "", ""
	}
	crit := 0
	for _, f := range hits {
		if f.Severity == "critical" {
			crit++
		}
	}
	title = fmt.Sprintf("🛡️ %d new vulnerabilit%s", len(hits), map[bool]string{true: "y", false: "ies"}[len(hits) == 1])
	if crit > 0 {
		title += fmt.Sprintf(" (%d critical)", crit)
	}
	var b strings.Builder
	for i, f := range hits {
		if i == 10 {
			fmt.Fprintf(&b, "…and %d more", len(hits)-10)
			break
		}
		where := f.IP
		if n := names[f.IP]; n != "" {
			where = n + " (" + f.IP + ")"
		}
		kev := ""
		if f.KEV {
			kev = " ⚠️ exploited in the wild"
		}
		fmt.Fprintf(&b, "<b>%s</b> %s — %s:%d%s\n", strings.ToUpper(f.Severity), htmlEsc(f.Title), htmlEsc(where), f.Port, kev)
	}
	return title, strings.TrimSpace(b.String())
}

func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

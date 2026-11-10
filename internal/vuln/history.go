package vuln

import (
	"context"
	"database/sql"
	"time"
)

// HistoryPoint is the number of open findings by severity on one day.
type HistoryPoint struct {
	Day      string `json:"day"` // YYYY-MM-DD (server-local)
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Medium   int    `json:"medium"`
	Low      int    `json:"low"`
	Info     int    `json:"info"`
	KEV      int    `json:"kev"`
	// Estimated: no snapshot existed yet, so the counts were rebuilt from
	// each finding's first-seen and resolved dates.
	Estimated bool `json:"estimated"`
}

const dayFormat = "2006-01-02"

// RecordHistory stores today's open counts (call after anything changes them).
func RecordHistory(ctx context.Context, db *sql.DB, now time.Time) error {
	var p HistoryPoint
	err := db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(severity = 'critical'), 0), COALESCE(SUM(severity = 'high'), 0), COALESCE(SUM(severity = 'medium'), 0),
		COALESCE(SUM(severity = 'low'), 0), COALESCE(SUM(severity = 'info'), 0), COALESCE(SUM(kev), 0)
		FROM vuln_findings WHERE status = 'open'`).Scan(&p.Critical, &p.High, &p.Medium, &p.Low, &p.Info, &p.KEV)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO vuln_history(day, critical, high, medium, low, info, kev, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET critical = excluded.critical, high = excluded.high, medium = excluded.medium,
		low = excluded.low, info = excluded.info, kev = excluded.kev, updated_at = excluded.updated_at`,
		now.Local().Format(dayFormat), p.Critical, p.High, p.Medium, p.Low, p.Info, p.KEV, now.UTC().Format(tsFormat))
	return err
}

// History returns one point per day for the last `days` days (oldest first).
// Days after the first snapshot carry the last known counts forward (counts
// only change when a snapshot is written); days before it are estimated.
func History(ctx context.Context, db *sql.DB, days int, now time.Time) ([]HistoryPoint, error) {
	if days < 1 {
		days = 1
	}
	now = now.Local()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))

	snaps := map[string]HistoryPoint{}
	rows, err := db.QueryContext(ctx, `SELECT day, critical, high, medium, low, info, kev FROM vuln_history ORDER BY day`)
	if err != nil {
		return nil, err
	}
	var first string
	var before *HistoryPoint // latest snapshot before the window
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.Day, &p.Critical, &p.High, &p.Medium, &p.Low, &p.Info, &p.KEV); err != nil {
			rows.Close()
			return nil, err
		}
		if first == "" {
			first = p.Day
		}
		if p.Day < start.Format(dayFormat) {
			cp := p
			before = &cp
		}
		snaps[p.Day] = p
	}
	rows.Close()

	// Findings, for estimating the days before the first snapshot.
	type span struct {
		sev        string
		kev        bool
		from, till time.Time // till zero = still open
	}
	var spans []span
	if first == "" || first > start.Format(dayFormat) {
		frows, err := db.QueryContext(ctx, `SELECT severity, kev, first_seen, status, COALESCE(resolved_at, '') FROM vuln_findings`)
		if err != nil {
			return nil, err
		}
		for frows.Next() {
			var sp span
			var fs, status, res string
			if frows.Scan(&sp.sev, &sp.kev, &fs, &status, &res) != nil {
				continue
			}
			t, err := time.Parse(tsFormat, fs)
			if err != nil {
				continue
			}
			sp.from = t
			if status != "open" {
				if r, err := time.Parse(tsFormat, res); err == nil {
					sp.till = r
				} else {
					continue // closed with no date: can't place it
				}
			}
			spans = append(spans, sp)
		}
		frows.Close()
	}

	out := make([]HistoryPoint, 0, days)
	var last *HistoryPoint = before
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		key := d.Format(dayFormat)
		if p, ok := snaps[key]; ok {
			out = append(out, p)
			cp := p
			last = &cp
			continue
		}
		if last != nil {
			p := *last
			p.Day = key
			out = append(out, p)
			continue
		}
		// Estimate: open at the end of that day.
		end := d.AddDate(0, 0, 1)
		p := HistoryPoint{Day: key, Estimated: true}
		for _, sp := range spans {
			if !sp.from.Before(end) || (!sp.till.IsZero() && sp.till.Before(end)) {
				continue
			}
			switch sp.sev {
			case "critical":
				p.Critical++
			case "high":
				p.High++
			case "medium":
				p.Medium++
			case "low":
				p.Low++
			default:
				p.Info++
			}
			if sp.kev {
				p.KEV++
			}
		}
		out = append(out, p)
	}
	return out, nil
}

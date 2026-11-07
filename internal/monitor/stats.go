package monitor

import (
	"context"
	"database/sql"
	"time"
)

// Bucket aggregates heartbeats over a time slice for uptime bars.
type Bucket struct {
	Start   time.Time `json:"t"`
	Up      int       `json:"up"`
	Total   int       `json:"total"` // up+down checks (maintenance/pending excluded)
	Maint   int       `json:"maint"`
	Latency *int      `json:"latency"`
}

// Buckets returns n consecutive buckets of `size` ending with the current period, merging raw
// heartbeats and hourly rollups.
func Buckets(ctx context.Context, db *sql.DB, monitorID int64, size time.Duration, n int) ([]Bucket, error) {
	// Align to bucket boundaries; the last bucket is the current (partial) period.
	end := time.Now().UTC().Truncate(size).Add(size)
	from := end.Add(-time.Duration(n) * size)
	fromUnix := from.Unix()
	sz := int64(size.Seconds())
	rows, err := db.QueryContext(ctx, `
		SELECT b, SUM(upc), SUM(tot), SUM(mnt), CAST(AVG(lat) AS INTEGER) FROM (
		  SELECT (CAST(strftime('%s', ts) AS INTEGER) - ?1) / ?2 AS b,
		         (up = 1) AS upc, (up IN (0, 1)) AS tot, (up = 2) AS mnt,
		         CASE WHEN up = 1 THEN latency_ms END AS lat
		  FROM heartbeats WHERE monitor_id = ?3 AND ts >= ?4
		  UNION ALL
		  SELECT (CAST(strftime('%s', hour) AS INTEGER) - ?1) / ?2, up_count, total_count, 0, avg_latency_ms
		  FROM heartbeat_hourly WHERE monitor_id = ?3 AND hour >= ?4
		) WHERE b >= 0 GROUP BY b`, fromUnix, sz, monitorID, from.Format(tsFormat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Bucket, n)
	for i := range out {
		out[i].Start = from.Add(time.Duration(i) * size)
	}
	for rows.Next() {
		var b, up, tot, mnt int
		var lat sql.NullInt64
		if err := rows.Scan(&b, &up, &tot, &mnt, &lat); err != nil {
			return nil, err
		}
		if b < 0 || b >= n {
			continue
		}
		out[b].Up, out[b].Total, out[b].Maint = up, tot, mnt
		if lat.Valid {
			v := int(lat.Int64)
			out[b].Latency = &v
		}
	}
	return out, rows.Err()
}

// Uptime returns the fraction of successful checks since `since`, or nil if
// there's no data.
func Uptime(ctx context.Context, db *sql.DB, monitorID int64, since time.Time) (*float64, error) {
	var up, tot sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT SUM(u), SUM(t) FROM (
		  SELECT (up = 1) AS u, (up IN (0, 1)) AS t FROM heartbeats WHERE monitor_id = ?1 AND ts >= ?2
		  UNION ALL
		  SELECT up_count, total_count FROM heartbeat_hourly WHERE monitor_id = ?1 AND hour >= ?2
		)`, monitorID, since.UTC().Format(tsFormat)).Scan(&up, &tot)
	if err != nil || !tot.Valid || tot.Int64 == 0 {
		return nil, err
	}
	v := float64(up.Int64) / float64(tot.Int64)
	return &v, nil
}

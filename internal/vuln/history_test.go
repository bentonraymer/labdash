package vuln

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestHistory(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	loc := time.Local
	day := func(n int) time.Time { return time.Date(2026, 11, n, 12, 0, 0, 0, loc) }
	ts := func(t time.Time) string { return t.UTC().Format(tsFormat) }
	d.Exec(`INSERT INTO devices(id, key, ip, first_seen, last_seen) VALUES (1, 'k', '10.0.0.1', 'x', 'x')`)
	ins := func(key, sev string, kev bool, seen time.Time, status string, resolved *time.Time) {
		var res any
		if resolved != nil {
			res = ts(*resolved)
		}
		if _, err := d.Exec(`INSERT INTO vuln_findings(device_id, ip, port, source, key, title, severity, kev, status, first_seen, last_seen, resolved_at)
			VALUES (1, '10.0.0.1', 80, 'cve', ?, ?, ?, ?, ?, ?, ?, ?)`, key, key, sev, kev, status, ts(seen), ts(seen), res); err != nil {
			t.Fatal(err)
		}
	}
	fixed := day(4)
	ins("a", "critical", true, day(2), "open", nil)
	ins("b", "high", false, day(2), "fixed", &fixed)
	ins("c", "medium", false, day(3), "open", nil)

	// No snapshots yet: everything estimated from the findings.
	pts, err := History(ctx, d, 5, day(5))
	if err != nil || len(pts) != 5 {
		t.Fatalf("%v %d", err, len(pts))
	}
	want := []struct{ c, h, m, kev int }{{0, 0, 0, 0}, {1, 1, 0, 1}, {1, 1, 1, 1}, {1, 0, 1, 1}, {1, 0, 1, 1}}
	for i, w := range want {
		p := pts[i]
		if !p.Estimated || p.Critical != w.c || p.High != w.h || p.Medium != w.m || p.KEV != w.kev {
			t.Errorf("day %s: %+v want %+v", p.Day, p, w)
		}
	}

	// A real snapshot on day 5, then carried forward to day 7.
	if err := RecordHistory(ctx, d, day(5)); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE vuln_findings SET status = 'accepted' WHERE key = 'c'`)
	pts, _ = History(ctx, d, 7, day(7))
	if p := pts[4]; p.Estimated || p.Day != "2026-11-05" || p.Critical != 1 || p.Medium != 1 {
		t.Errorf("snapshot day: %+v", p)
	}
	if p := pts[6]; p.Estimated || p.Medium != 1 {
		t.Errorf("carried forward: %+v", p)
	}
	if p := pts[3]; !p.Estimated {
		t.Errorf("before the first snapshot should be estimated: %+v", p)
	}
	// Accepting it and recording today drops it from today's count.
	RecordHistory(ctx, d, day(7))
	pts, _ = History(ctx, d, 7, day(7))
	if p := pts[6]; p.Medium != 0 || pts[5].Medium != 1 {
		t.Errorf("after accept: today %+v yesterday %+v", p, pts[5])
	}
}

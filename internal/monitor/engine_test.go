package monitor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bentonraymer/labdash/internal/db"
	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/monitor"
)

type recorder struct {
	mu  sync.Mutex
	got []monitor.Notification
}

func (r *recorder) Notify(_ context.Context, n monitor.Notification) error {
	r.mu.Lock()
	r.got = append(r.got, n)
	r.mu.Unlock()
	return nil
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var k []string
	for _, n := range r.got {
		k = append(k, n.Kind)
	}
	return k
}

// End-to-end: a service with a derived HTTP monitor goes down after retries,
// opens an incident, alerts once, recovers, closes the incident, and stats
// reflect the checks.
func TestEngineLifecycle(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()

	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO services(id, name, dns_url) VALUES (1, 'Jellyfin', ?)`, srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO monitors(id, service_id, kind, retries) VALUES (1, 1, 'http', 1)`); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	eng := monitor.NewEngine(d, events.NewHub(), rec)
	check := func() string {
		t.Helper()
		_, _, err := eng.CheckNow(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		var st string
		d.QueryRow(`SELECT status FROM monitors WHERE id = 1`).Scan(&st)
		return st
	}

	if st := check(); st != "up" {
		t.Fatalf("want up, got %s", st)
	}
	healthy.Store(false)
	if st := check(); st != "up" { // 1 failure, within retries
		t.Fatalf("want still up during retry, got %s", st)
	}
	if st := check(); st != "down" {
		t.Fatalf("want down, got %s", st)
	}
	check() // stays down, no second alert
	var open int
	d.QueryRow(`SELECT COUNT(*) FROM incidents WHERE ended_at IS NULL`).Scan(&open)
	if open != 1 {
		t.Fatalf("want 1 open incident, got %d", open)
	}
	healthy.Store(true)
	if st := check(); st != "up" {
		t.Fatalf("want recovered, got %s", st)
	}
	d.QueryRow(`SELECT COUNT(*) FROM incidents WHERE ended_at IS NULL`).Scan(&open)
	if open != 0 {
		t.Fatal("incident should be closed")
	}

	time.Sleep(50 * time.Millisecond) // notifications are sent asynchronously
	if k := rec.kinds(); len(k) != 2 || k[0] != "down" || k[1] != "up" {
		t.Fatalf("notifications = %v", k)
	}

	// 5 checks: up, pending, down, down, up → 2 up / 4 counted (pending excluded)
	u, err := monitor.Uptime(ctx, d, 1, time.Now().Add(-time.Hour))
	if err != nil || u == nil || *u != 0.5 {
		t.Fatalf("uptime = %v, %v", u, err)
	}
	b, err := monitor.Buckets(ctx, d, 1, 30*time.Minute, 48)
	if err != nil {
		t.Fatal(err)
	}
	last := b[len(b)-1]
	if last.Up != 2 || last.Total != 4 {
		t.Fatalf("last bucket = %+v", last)
	}

	// Muted monitors and paused alerts don't notify.
	d.Exec(`UPDATE monitors SET muted = 1 WHERE id = 1`)
	healthy.Store(false)
	check()
	check()
	time.Sleep(50 * time.Millisecond)
	if n := len(rec.kinds()); n != 2 {
		t.Fatalf("muted monitor notified: %v", rec.kinds())
	}
}

func TestRollup(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO monitors(id, kind) VALUES (1, 'tcp')`)
	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 4; i++ {
		up := 1
		if i == 3 {
			up = 0
		}
		d.Exec(`INSERT INTO heartbeats(monitor_id, ts, up, latency_ms) VALUES (1, ?, ?, 10)`, old.Add(time.Duration(i)*time.Minute).Format("2006-01-02T15:04:05.000Z"), up)
	}
	d.Exec(`INSERT INTO heartbeats(monitor_id, ts, up) VALUES (1, ?, 1)`, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))

	monitor.NewEngine(d, events.NewHub(), nil).Rollup(context.Background())

	var raw, upc, tot int
	d.QueryRow(`SELECT COUNT(*) FROM heartbeats`).Scan(&raw)
	d.QueryRow(`SELECT up_count, total_count FROM heartbeat_hourly WHERE monitor_id = 1`).Scan(&upc, &tot)
	if raw != 1 || upc != 3 || tot != 4 {
		t.Fatalf("raw=%d up=%d total=%d", raw, upc, tot)
	}
	u, _ := monitor.Uptime(context.Background(), d, 1, time.Now().Add(-30*24*time.Hour))
	if u == nil || *u != 0.8 {
		t.Fatalf("uptime across raw+hourly = %v", u)
	}
}

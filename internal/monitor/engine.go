package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/urls"
)

const tsFormat = "2006-01-02T15:04:05.000Z"

// Config is one enabled monitor with its target resolved.
type Config struct {
	ID          int64
	ServiceID   *int64
	HostID      *int64
	Name        string
	ServiceName string
	HostName    string
	Kind        string
	Target      string
	Derived     bool // target came from the service, not an explicit override
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	Expected    string
	Keyword     string
	IgnoreTLS   bool
	Muted       bool
	Priority    int
	Sound       string
}

func (c Config) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.ServiceName != "" {
		return c.ServiceName
	}
	return c.Target
}

// Notification describes an alert-worthy transition.
type Notification struct {
	Kind      string // down | up
	Monitor   Config
	Message   string
	DownSince time.Time // for "up": when the outage started
	At        time.Time
}

type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}

type entry struct {
	cfg     Config
	next    time.Time
	running bool
}

type Engine struct {
	DB       *sql.DB
	Hub      *events.Hub
	Notifier Notifier

	mu       sync.Mutex
	mons     map[int64]*entry
	windows  []Window
	parents  map[int64]int64 // host → parent host
	reloadCh chan struct{}
	sem      chan struct{}
}

func NewEngine(db *sql.DB, hub *events.Hub, n Notifier) *Engine {
	return &Engine{
		DB: db, Hub: hub, Notifier: n,
		mons:     map[int64]*entry{},
		reloadCh: make(chan struct{}, 1),
		sem:      make(chan struct{}, 24),
	}
}

// Reload asks the engine to re-read monitors (call after CRUD/settings changes).
func (e *Engine) Reload() {
	select {
	case e.reloadCh <- struct{}{}:
	default:
	}
}

func (e *Engine) Run(ctx context.Context) {
	if err := e.load(ctx); err != nil {
		slog.Error("monitor: initial load", "err", err)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	reload := time.NewTicker(30 * time.Second)
	defer reload.Stop()
	rollup := time.NewTicker(time.Hour)
	defer rollup.Stop()
	go e.Rollup(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.reloadCh:
			if err := e.load(ctx); err != nil {
				slog.Error("monitor: reload", "err", err)
			}
		case <-reload.C:
			if err := e.load(ctx); err != nil {
				slog.Error("monitor: reload", "err", err)
			}
		case <-rollup.C:
			go e.Rollup(ctx)
		case now := <-tick.C:
			e.dispatchDue(ctx, now)
		}
	}
}

func (e *Engine) dispatchDue(ctx context.Context, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, en := range e.mons {
		if en.running || now.Before(en.next) {
			continue
		}
		en.running = true
		en.next = now.Add(en.cfg.Interval)
		cfg := en.cfg
		go func() {
			e.sem <- struct{}{}
			defer func() { <-e.sem }()
			if _, err := e.execute(ctx, cfg); err != nil && ctx.Err() == nil {
				slog.Error("monitor: check", "monitor", cfg.ID, "err", err)
			}
			e.mu.Lock()
			if cur, ok := e.mons[cfg.ID]; ok {
				cur.running = false
			}
			e.mu.Unlock()
		}()
	}
}

// load reads enabled monitors, resolves targets, and refreshes maintenance
// windows. Existing schedule positions are preserved.
func (e *Engine) load(ctx context.Context) error {
	mode := "dns"
	var raw string
	if err := e.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'access_mode'`).Scan(&raw); err == nil {
		var m string
		if json.Unmarshal([]byte(raw), &m) == nil && m == "direct" {
			mode = "direct"
		}
	}
	cfgs, err := LoadConfigs(ctx, e.DB, mode, "m.enabled = 1")
	if err != nil {
		return err
	}
	windows, err := LoadWindows(ctx, e.DB)
	if err != nil {
		return err
	}
	parents := map[int64]int64{}
	if rows, err := e.DB.QueryContext(ctx, `SELECT id, parent_id FROM hosts WHERE parent_id IS NOT NULL`); err == nil {
		for rows.Next() {
			var id, p int64
			if rows.Scan(&id, &p) == nil {
				parents[id] = p
			}
		}
		rows.Close()
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	next := make(map[int64]*entry, len(cfgs))
	now := time.Now()
	for _, c := range cfgs {
		if old, ok := e.mons[c.ID]; ok {
			old.cfg = c
			if old.next.Sub(now) > c.Interval { // interval shortened
				old.next = now.Add(time.Duration(rand.Int64N(int64(2 * time.Second))))
			}
			next[c.ID] = old
			continue
		}
		// Spread first checks over a few seconds to avoid a thundering herd.
		next[c.ID] = &entry{cfg: c, next: now.Add(time.Duration(rand.Int64N(int64(5 * time.Second))))}
	}
	e.mons = next
	e.windows = windows
	e.parents = parents
	return nil
}

// LoadConfigs reads monitors (filtered by where) with targets resolved for mode.
func LoadConfigs(ctx context.Context, db *sql.DB, mode, where string, args ...any) ([]Config, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.service_id, d.host_id, m.name, COALESCE(s.name, ''), COALESCE(h.name, ''), m.kind, m.target, m.interval_s, m.timeout_s,
		       m.retries, m.expected_status, m.keyword, m.ignore_tls, m.muted, m.pushover_priority, m.pushover_sound,
		       COALESCE(s.dns_url, ''), COALESCE(s.direct_scheme, 'http'), COALESCE(s.direct_ip, ''), s.direct_port, COALESCE(s.direct_path, '')
		FROM monitors m
		LEFT JOIN services s ON s.id = m.service_id
		LEFT JOIN deployments d ON d.service_id = m.service_id
		LEFT JOIN hosts h ON h.id = d.host_id
		WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Config
	for rows.Next() {
		var c Config
		var svcID, hostID, port sql.NullInt64
		var interval, timeout int
		var t urls.Target
		if err := rows.Scan(&c.ID, &svcID, &hostID, &c.Name, &c.ServiceName, &c.HostName, &c.Kind, &c.Target, &interval, &timeout,
			&c.Retries, &c.Expected, &c.Keyword, &c.IgnoreTLS, &c.Muted, &c.Priority, &c.Sound,
			&t.DNSURL, &t.DirectScheme, &t.DirectIP, &port, &t.DirectPath); err != nil {
			return nil, err
		}
		if svcID.Valid {
			c.ServiceID = &svcID.Int64
		}
		if hostID.Valid {
			c.HostID = &hostID.Int64
		}
		if port.Valid {
			p := int(port.Int64)
			t.DirectPort = &p
		}
		if c.Target == "" {
			c.Target = DeriveTarget(c.Kind, t, mode)
			c.Derived = true
		}
		c.Interval = time.Duration(max(interval, 10)) * time.Second
		c.Timeout = time.Duration(max(timeout, 1)) * time.Second
		out = append(out, c)
	}
	return out, rows.Err()
}

func LoadWindows(ctx context.Context, db *sql.DB) ([]Window, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, scope, scope_id, starts_at, ends_at, recurrence, duration_min, enabled FROM maintenance_windows`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Window
	for rows.Next() {
		var w Window
		var scopeID sql.NullInt64
		var start, end string
		var dur int
		if err := rows.Scan(&w.ID, &w.Name, &w.Scope, &scopeID, &start, &end, &w.Recurrence, &dur, &w.Enabled); err != nil {
			return nil, err
		}
		if scopeID.Valid {
			w.ScopeID = &scopeID.Int64
		}
		w.Start, _ = time.Parse(time.RFC3339, start)
		w.End, _ = time.Parse(time.RFC3339, end)
		w.Duration = time.Duration(dur) * time.Minute
		out = append(out, w)
	}
	return out, rows.Err()
}

// InMaintenance reports whether any active window covers the monitor,
// including windows on any ancestor host (a VM inside a node under maintenance).
func (e *Engine) InMaintenance(c Config, t time.Time) bool {
	e.mu.Lock()
	windows, parents := e.windows, e.parents
	e.mu.Unlock()
	for _, w := range windows {
		if !w.ActiveAt(t) {
			continue
		}
		if w.Scope == "host" && c.HostID != nil {
			for h, depth := *c.HostID, 0; depth < 8; depth++ {
				hid := h
				if w.Covers(c.ServiceID, &hid) {
					return true
				}
				p, ok := parents[h]
				if !ok {
					break
				}
				h = p
			}
			continue
		}
		if w.Covers(c.ServiceID, c.HostID) {
			return true
		}
	}
	return false
}

// CheckNow runs a monitor immediately (used by the "check now" button).
func (e *Engine) CheckNow(ctx context.Context, id int64) (Outcome, Result, error) {
	mode := "dns"
	var raw string
	if err := e.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'access_mode'`).Scan(&raw); err == nil && raw == `"direct"` {
		mode = "direct"
	}
	cfgs, err := LoadConfigs(ctx, e.DB, mode, "m.id = ?", id)
	if err != nil {
		return Outcome{}, Result{}, err
	}
	if len(cfgs) == 0 {
		return Outcome{}, Result{}, sql.ErrNoRows
	}
	r, err := e.execute(ctx, cfgs[0])
	return r.outcome, r.result, err
}

type execResult struct {
	outcome Outcome
	result  Result
}

func (e *Engine) execute(ctx context.Context, c Config) (execResult, error) {
	res := Check(ctx, CheckSpec{Kind: c.Kind, Target: c.Target, Timeout: c.Timeout, ExpectedStatus: c.Expected, Keyword: c.Keyword, IgnoreTLS: c.IgnoreTLS})
	now := time.Now()
	nowS := now.UTC().Format(tsFormat)
	maint := e.InMaintenance(c, now)

	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return execResult{}, err
	}
	defer tx.Rollback()

	var cur State
	if err := tx.QueryRowContext(ctx, `SELECT status, consecutive_failures FROM monitors WHERE id = ?`, c.ID).Scan(&cur.Status, &cur.Failures); err != nil {
		return execResult{}, err // monitor deleted mid-check
	}
	out := Transition(cur, res.Up, c.Retries, maint)
	latMs := res.Latency.Milliseconds()
	var latPtr *int64
	if res.Latency > 0 {
		latPtr = &latMs
	}

	if _, err := tx.ExecContext(ctx, `UPDATE monitors SET status = ?, consecutive_failures = ?, last_check_at = ?,
		last_latency_ms = ?, last_message = ?, last_change_at = CASE WHEN ? THEN ? ELSE last_change_at END WHERE id = ?`,
		out.Status, out.Failures, nowS, latPtr, res.Message, out.Changed, nowS, c.ID); err != nil {
		return execResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO heartbeats(monitor_id, ts, up, latency_ms, message) VALUES (?, ?, ?, ?, ?)`,
		c.ID, nowS, out.Beat, latPtr, res.Message); err != nil {
		return execResult{}, err
	}

	var downSince time.Time
	if out.Changed && out.Status == "down" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO incidents(monitor_id, started_at, cause) VALUES (?, ?, ?)`, c.ID, nowS, res.Message); err != nil {
			return execResult{}, err
		}
	}
	if cur.Status == "down" && out.Status != "down" {
		var started string
		if err := tx.QueryRowContext(ctx, `SELECT started_at FROM incidents WHERE monitor_id = ? AND ended_at IS NULL ORDER BY id DESC LIMIT 1`, c.ID).Scan(&started); err == nil {
			downSince, _ = time.Parse(tsFormat, started)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET ended_at = ? WHERE monitor_id = ? AND ended_at IS NULL`, nowS, c.ID); err != nil {
			return execResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return execResult{}, err
	}

	e.Hub.Publish("beat", map[string]any{"id": c.ID, "status": out.Status, "latency": latPtr, "up": out.Beat})
	if out.Changed {
		e.Hub.Publish("status", map[string]any{"id": c.ID, "serviceId": c.ServiceID, "status": out.Status})
	}

	if out.Notify != "" && !c.Muted && !maint && !alertsPaused(ctx, e.DB) && e.Notifier != nil {
		n := Notification{Kind: out.Notify, Monitor: c, Message: res.Message, DownSince: downSince, At: now}
		go func() {
			nctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := e.Notifier.Notify(nctx, n); err != nil {
				slog.Warn("monitor: notify failed", "monitor", c.ID, "err", err)
			}
		}()
	}
	return execResult{outcome: out, result: res}, nil
}

func alertsPaused(ctx context.Context, db *sql.DB) bool {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'alerts_paused'`).Scan(&raw); err != nil {
		return false
	}
	return raw == "true"
}

// Rollup compacts raw heartbeats older than 7 days into hourly rows and drops
// hourly data older than ~13 months.
func (e *Engine) Rollup(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Truncate(time.Hour).Format(tsFormat)
	oldest := time.Now().UTC().Add(-400 * 24 * time.Hour).Format(tsFormat)
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO heartbeat_hourly(monitor_id, hour, up_count, total_count, avg_latency_ms)
		SELECT monitor_id, strftime('%Y-%m-%dT%H:00:00.000Z', ts), SUM(up = 1), SUM(up IN (0, 1)),
		       CAST(AVG(CASE WHEN up = 1 THEN latency_ms END) AS INTEGER)
		FROM heartbeats WHERE ts < ? GROUP BY 1, 2
		ON CONFLICT(monitor_id, hour) DO UPDATE SET
		  up_count = up_count + excluded.up_count,
		  total_count = total_count + excluded.total_count,
		  avg_latency_ms = COALESCE(excluded.avg_latency_ms, avg_latency_ms)`, cutoff); err != nil {
		slog.Error("monitor: rollup", "err", err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM heartbeats WHERE ts < ?`, cutoff); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM heartbeat_hourly WHERE hour < ?`, oldest); err != nil {
		return
	}
	_ = tx.Commit()
}

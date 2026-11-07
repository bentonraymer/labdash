package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/monitor"
)

type MonitorView struct {
	ID             int64            `json:"id"`
	ServiceID      *int64           `json:"serviceId"`
	ServiceName    string           `json:"serviceName"`
	ServiceIcon    string           `json:"serviceIcon"`
	SectionID      *int64           `json:"sectionId"`
	Name           string           `json:"name"`
	Kind           string           `json:"kind"`
	Target         string           `json:"target"`         // explicit override ('' = derived)
	ResolvedTarget string           `json:"resolvedTarget"` // what is actually checked
	IntervalS      int              `json:"intervalS"`
	TimeoutS       int              `json:"timeoutS"`
	Retries        int              `json:"retries"`
	ExpectedStatus string           `json:"expectedStatus"`
	Keyword        string           `json:"keyword"`
	IgnoreTLS      bool             `json:"ignoreTls"`
	Enabled        bool             `json:"enabled"`
	Muted          bool             `json:"muted"`
	Priority       int              `json:"priority"`
	Sound          string           `json:"sound"`
	Status         string           `json:"status"`
	LastCheckAt    *string          `json:"lastCheckAt"`
	LastChangeAt   *string          `json:"lastChangeAt"`
	LastLatencyMs  *int             `json:"lastLatencyMs"`
	LastMessage    string           `json:"lastMessage"`
	Uptime24h      *float64         `json:"uptime24h"`
	Uptime7d       *float64         `json:"uptime7d"`
	Uptime30d      *float64         `json:"uptime30d"`
	Bars           []monitor.Bucket `json:"bars"`
}

func (s *Server) reloadMonitors() {
	if s.Monitor != nil {
		s.Monitor.Reload()
	}
}

func (s *Server) listMonitors(ctx context.Context, where string, args ...any) ([]*MonitorView, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT m.id, m.service_id, COALESCE(sv.name, ''), COALESCE(sv.icon, ''), sv.section_id, m.name, m.kind, m.target,
		       m.interval_s, m.timeout_s, m.retries, m.expected_status, m.keyword, m.ignore_tls, m.enabled, m.muted,
		       m.pushover_priority, m.pushover_sound, m.status, m.last_check_at, m.last_change_at, m.last_latency_ms, m.last_message
		FROM monitors m LEFT JOIN services sv ON sv.id = m.service_id
		WHERE `+where+` ORDER BY COALESCE(sv.name, m.name) COLLATE NOCASE, m.id`, args...)
	if err != nil {
		return nil, err
	}
	var out []*MonitorView
	byID := map[int64]*MonitorView{}
	for rows.Next() {
		var m MonitorView
		var svc, sec, lat sql.NullInt64
		var lastCheck, lastChange sql.NullString
		if err := rows.Scan(&m.ID, &svc, &m.ServiceName, &m.ServiceIcon, &sec, &m.Name, &m.Kind, &m.Target,
			&m.IntervalS, &m.TimeoutS, &m.Retries, &m.ExpectedStatus, &m.Keyword, &m.IgnoreTLS, &m.Enabled, &m.Muted,
			&m.Priority, &m.Sound, &m.Status, &lastCheck, &lastChange, &lat, &m.LastMessage); err != nil {
			rows.Close()
			return nil, err
		}
		if svc.Valid {
			m.ServiceID = &svc.Int64
		}
		if sec.Valid {
			m.SectionID = &sec.Int64
		}
		if lastCheck.Valid {
			m.LastCheckAt = &lastCheck.String
		}
		if lastChange.Valid {
			m.LastChangeAt = &lastChange.String
		}
		if lat.Valid {
			v := int(lat.Int64)
			m.LastLatencyMs = &v
		}
		if !m.Enabled {
			m.Status = "paused"
		}
		out = append(out, &m)
		byID[m.ID] = &m
	}
	rows.Close()

	// Resolved targets follow the current access mode.
	cfgs, err := monitor.LoadConfigs(ctx, s.DB, s.accessMode(ctx), "1 = 1")
	if err != nil {
		return nil, err
	}
	for _, c := range cfgs {
		if m, ok := byID[c.ID]; ok {
			m.ResolvedTarget = c.Target
		}
	}

	now := time.Now()
	for _, m := range out {
		m.Uptime24h, _ = monitor.Uptime(ctx, s.DB, m.ID, now.Add(-24*time.Hour))
		m.Uptime7d, _ = monitor.Uptime(ctx, s.DB, m.ID, now.Add(-7*24*time.Hour))
		m.Uptime30d, _ = monitor.Uptime(ctx, s.DB, m.ID, now.Add(-30*24*time.Hour))
		m.Bars, _ = monitor.Buckets(ctx, s.DB, m.ID, 30*time.Minute, 48)
	}
	if out == nil {
		out = []*MonitorView{}
	}
	return out, nil
}

func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	ms, err := s.listMonitors(r.Context(), "1 = 1")
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

type monitorInput struct {
	ServiceID      *int64  `json:"serviceId"`
	ClearService   bool    `json:"clearService"`
	Name           *string `json:"name"`
	Kind           *string `json:"kind"`
	Target         *string `json:"target"`
	IntervalS      *int    `json:"intervalS"`
	TimeoutS       *int    `json:"timeoutS"`
	Retries        *int    `json:"retries"`
	ExpectedStatus *string `json:"expectedStatus"`
	Keyword        *string `json:"keyword"`
	IgnoreTLS      *bool   `json:"ignoreTls"`
	Enabled        *bool   `json:"enabled"`
	Muted          *bool   `json:"muted"`
	Priority       *int    `json:"priority"`
	Sound          *string `json:"sound"`
}

func (in monitorInput) validate() error {
	if in.Kind != nil && oneOf("http", "tcp", "ping", "dns")(*in.Kind) != nil {
		return errors.New("kind must be http, tcp, ping or dns")
	}
	if in.IntervalS != nil && (*in.IntervalS < 10 || *in.IntervalS > 86400) {
		return errors.New("interval must be 10s–24h")
	}
	if in.TimeoutS != nil && (*in.TimeoutS < 1 || *in.TimeoutS > 120) {
		return errors.New("timeout must be 1–120s")
	}
	if in.Retries != nil && (*in.Retries < 0 || *in.Retries > 20) {
		return errors.New("retries must be 0–20")
	}
	if in.Priority != nil && (*in.Priority < -2 || *in.Priority > 2) {
		return errors.New("priority must be -2…2")
	}
	return nil
}

func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var in monitorInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.ServiceID == nil && strings.TrimSpace(deref(in.Target, "")) == "" {
		writeErr(w, http.StatusBadRequest, "a monitor needs a service or an explicit target")
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO monitors
		(service_id, name, kind, target, interval_s, timeout_s, retries, expected_status, keyword, ignore_tls, enabled, muted, pushover_priority, pushover_sound)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ServiceID, strings.TrimSpace(deref(in.Name, "")), deref(in.Kind, "http"), strings.TrimSpace(deref(in.Target, "")),
		deref(in.IntervalS, 60), deref(in.TimeoutS, 10), deref(in.Retries, 2), deref(in.ExpectedStatus, "200-399"),
		deref(in.Keyword, ""), deref(in.IgnoreTLS, true), deref(in.Enabled, true), deref(in.Muted, false),
		deref(in.Priority, 0), deref(in.Sound, ""))
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	s.reloadMonitors()
	// Run the first check right away so the UI isn't "pending" for a minute.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _, _ = s.Monitor.CheckNow(ctx, id)
	}()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in monitorInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE monitors SET
		service_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, service_id) END,
		name = COALESCE(?, name), kind = COALESCE(?, kind), target = COALESCE(?, target),
		interval_s = COALESCE(?, interval_s), timeout_s = COALESCE(?, timeout_s), retries = COALESCE(?, retries),
		expected_status = COALESCE(?, expected_status), keyword = COALESCE(?, keyword), ignore_tls = COALESCE(?, ignore_tls),
		enabled = COALESCE(?, enabled), muted = COALESCE(?, muted),
		pushover_priority = COALESCE(?, pushover_priority), pushover_sound = COALESCE(?, pushover_sound),
		status = CASE WHEN ? = 0 THEN 'pending' ELSE status END
		WHERE id = ?`,
		in.ClearService, in.ServiceID, trimPtr(in.Name), in.Kind, trimPtr(in.Target),
		in.IntervalS, in.TimeoutS, in.Retries, in.ExpectedStatus, in.Keyword, in.IgnoreTLS,
		in.Enabled, in.Muted, in.Priority, in.Sound, deref(in.Enabled, true), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "monitor not found")
		return
	}
	s.reloadMonitors()
	s.Events.Publish("status", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM monitors WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("status", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCheckMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, res, err := s.Monitor.CheckNow(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "monitor not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": out.Status, "up": res.Up, "latencyMs": res.Latency.Milliseconds(), "message": res.Message,
	})
}

// handleMonitorHistory returns uptime bars for a range plus that monitor's incidents.
func (s *Server) handleMonitorHistory(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	size, n := 15*time.Minute, 96
	switch r.URL.Query().Get("range") {
	case "7d":
		size, n = 4*time.Hour, 42
	case "30d":
		size, n = 24*time.Hour, 30
	}
	b, err := monitor.Buckets(r.Context(), s.DB, id, size, n)
	if err != nil {
		serverErr(w, err)
		return
	}
	inc, err := s.listIncidents(r.Context(), "i.monitor_id = ?", 20, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": b, "bucketSeconds": int(size.Seconds()), "incidents": inc})
}

// ── incidents ─────────────────────────────────────────────────────────────

type Incident struct {
	ID          int64   `json:"id"`
	MonitorID   int64   `json:"monitorId"`
	MonitorName string  `json:"monitorName"`
	ServiceID   *int64  `json:"serviceId"`
	ServiceIcon string  `json:"serviceIcon"`
	StartedAt   string  `json:"startedAt"`
	EndedAt     *string `json:"endedAt"`
	Cause       string  `json:"cause"`
}

func (s *Server) listIncidents(ctx context.Context, where string, limit int, args ...any) ([]Incident, error) {
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT i.id, i.monitor_id, COALESCE(NULLIF(m.name, ''), sv.name, m.target), m.service_id, COALESCE(sv.icon, ''),
		       i.started_at, i.ended_at, i.cause
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id LEFT JOIN services sv ON sv.id = m.service_id
		WHERE `+where+` ORDER BY i.started_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var in Incident
		var svc sql.NullInt64
		var ended sql.NullString
		if err := rows.Scan(&in.ID, &in.MonitorID, &in.MonitorName, &svc, &in.ServiceIcon, &in.StartedAt, &ended, &in.Cause); err != nil {
			return nil, err
		}
		if svc.Valid {
			in.ServiceID = &svc.Int64
		}
		if ended.Valid {
			in.EndedAt = &ended.String
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	inc, err := s.listIncidents(r.Context(), "1 = 1", 50)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

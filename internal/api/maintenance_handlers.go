package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/monitor"
)

type MaintenanceView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	ScopeID     *int64 `json:"scopeId"`
	ScopeName   string `json:"scopeName"`
	StartsAt    string `json:"startsAt"`
	EndsAt      string `json:"endsAt"`
	Recurrence  string `json:"recurrence"`
	DurationMin int    `json:"durationMin"`
	Enabled     bool   `json:"enabled"`
	Active      bool   `json:"active"`
}

func (s *Server) handleListMaintenance(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT w.id, w.name, w.scope, w.scope_id,
		       COALESCE(CASE w.scope WHEN 'service' THEN sv.name WHEN 'host' THEN h.name END, ''),
		       w.starts_at, w.ends_at, w.recurrence, w.duration_min, w.enabled
		FROM maintenance_windows w
		LEFT JOIN services sv ON w.scope = 'service' AND sv.id = w.scope_id
		LEFT JOIN hosts h ON w.scope = 'host' AND h.id = w.scope_id
		ORDER BY w.enabled DESC, w.starts_at DESC`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	out := []MaintenanceView{}
	now := time.Now()
	for rows.Next() {
		var v MaintenanceView
		var sid sql.NullInt64
		if err := rows.Scan(&v.ID, &v.Name, &v.Scope, &sid, &v.ScopeName, &v.StartsAt, &v.EndsAt, &v.Recurrence, &v.DurationMin, &v.Enabled); err != nil {
			serverErr(w, err)
			return
		}
		if sid.Valid {
			v.ScopeID = &sid.Int64
		}
		v.Active = toWindow(v).ActiveAt(now)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func toWindow(v MaintenanceView) monitor.Window {
	w := monitor.Window{ID: v.ID, Name: v.Name, Scope: v.Scope, ScopeID: v.ScopeID, Recurrence: v.Recurrence,
		Duration: time.Duration(v.DurationMin) * time.Minute, Enabled: v.Enabled}
	w.Start, _ = time.Parse(time.RFC3339, v.StartsAt)
	w.End, _ = time.Parse(time.RFC3339, v.EndsAt)
	return w
}

type maintenanceInput struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	ScopeID     *int64 `json:"scopeId"`
	StartsAt    string `json:"startsAt"` // RFC3339
	EndsAt      string `json:"endsAt"`   // RFC3339; '' for open-ended recurring
	Recurrence  string `json:"recurrence"`
	DurationMin int    `json:"durationMin"`
	Enabled     bool   `json:"enabled"`
}

func (in *maintenanceInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Recurrence = strings.TrimSpace(in.Recurrence)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if oneOf("global", "service", "host")(in.Scope) != nil {
		return errors.New("scope must be global, service or host")
	}
	if in.Scope != "global" && in.ScopeID == nil {
		return errors.New("choose what this window applies to")
	}
	if in.Scope == "global" {
		in.ScopeID = nil
	}
	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339, s)
	}
	start, err := parse(in.StartsAt)
	if err != nil {
		return errors.New("invalid start time")
	}
	end, err := parse(in.EndsAt)
	if err != nil {
		return errors.New("invalid end time")
	}
	if in.Recurrence == "" {
		if start.IsZero() || end.IsZero() || !end.After(start) {
			return errors.New("a one-off window needs a start before its end")
		}
		in.DurationMin = int(end.Sub(start).Minutes())
	} else {
		if err := monitor.ValidateCron(in.Recurrence); err != nil {
			return errors.New("invalid cron expression: " + err.Error())
		}
		if in.DurationMin < 1 || in.DurationMin > 7*24*60 {
			return errors.New("duration must be 1 minute – 7 days")
		}
	}
	return nil
}

func (s *Server) handleSaveMaintenance(w http.ResponseWriter, r *http.Request) {
	var in maintenanceInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.normalize(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var err error
	status := http.StatusOK
	if r.Method == http.MethodPost {
		_, err = s.DB.ExecContext(r.Context(), `INSERT INTO maintenance_windows(name, scope, scope_id, starts_at, ends_at, recurrence, duration_min, enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, in.Name, in.Scope, in.ScopeID, in.StartsAt, in.EndsAt, in.Recurrence, in.DurationMin, in.Enabled)
		status = http.StatusCreated
	} else {
		id, perr := idParam(r)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr.Error())
			return
		}
		_, err = s.DB.ExecContext(r.Context(), `UPDATE maintenance_windows SET name = ?, scope = ?, scope_id = ?, starts_at = ?, ends_at = ?,
			recurrence = ?, duration_min = ?, enabled = ? WHERE id = ?`,
			in.Name, in.Scope, in.ScopeID, in.StartsAt, in.EndsAt, in.Recurrence, in.DurationMin, in.Enabled, id)
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("status", nil)
	writeJSON(w, status, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteMaintenance(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM maintenance_windows WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("status", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

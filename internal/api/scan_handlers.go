package api

import (
	"database/sql"
	"net/http"
	"net/netip"
	"strings"

	"github.com/bentonraymer/labdash/internal/scanner"
)

// ── scan targets ──────────────────────────────────────────────────────────

type ScanTarget struct {
	ID          int64   `json:"id"`
	CIDR        string  `json:"cidr"`
	Name        string  `json:"name"`
	Schedule    string  `json:"schedule"`
	PortProfile string  `json:"portProfile"`
	CustomPorts string  `json:"customPorts"`
	Enabled     bool    `json:"enabled"`
	LastRunAt   *string `json:"lastRunAt"`
}

func (s *Server) handleListScanTargets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, cidr, name, schedule, port_profile, custom_ports, enabled, last_run_at FROM scan_targets ORDER BY cidr`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	out := []ScanTarget{}
	for rows.Next() {
		var t ScanTarget
		var last sql.NullString
		if err := rows.Scan(&t.ID, &t.CIDR, &t.Name, &t.Schedule, &t.PortProfile, &t.CustomPorts, &t.Enabled, &last); err != nil {
			serverErr(w, err)
			return
		}
		if last.Valid {
			t.LastRunAt = &last.String
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveScanTarget(w http.ResponseWriter, r *http.Request) {
	var in ScanTarget
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.CIDR = strings.TrimSpace(in.CIDR)
	p, err := netip.ParsePrefix(in.CIDR)
	if err != nil {
		if a, aerr := netip.ParseAddr(in.CIDR); aerr == nil && a.Is4() {
			p = netip.PrefixFrom(a, 32)
		} else {
			writeErr(w, http.StatusBadRequest, "enter a CIDR like 192.168.8.0/24 or a single IP")
			return
		}
	}
	if !p.Addr().Is4() || p.Bits() < 16 {
		writeErr(w, http.StatusBadRequest, "IPv4 only, /16 or smaller")
		return
	}
	in.CIDR = p.Masked().String()
	in.Schedule = strings.TrimSpace(in.Schedule)
	if err := scanner.ValidateSchedule(in.Schedule); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid schedule: "+err.Error())
		return
	}
	if in.PortProfile == "" {
		in.PortProfile = "top1000"
	}
	if oneOf("top100", "top1000", "full", "custom")(in.PortProfile) != nil {
		writeErr(w, http.StatusBadRequest, "invalid port profile")
		return
	}
	if err := (scanner.Profile{Name: in.PortProfile, Custom: in.CustomPorts}).Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{in.CIDR, strings.TrimSpace(in.Name), in.Schedule, in.PortProfile, strings.TrimSpace(in.CustomPorts), in.Enabled}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		res, err := s.DB.ExecContext(r.Context(), `INSERT INTO scan_targets(cidr, name, schedule, port_profile, custom_ports, enabled) VALUES (?, ?, ?, ?, ?, ?)`, args...)
		if err != nil {
			serverErr(w, err)
			return
		}
		in.ID, _ = res.LastInsertId()
		status = http.StatusCreated
	} else {
		id, perr := idParam(r)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr.Error())
			return
		}
		if _, err := s.DB.ExecContext(r.Context(), `UPDATE scan_targets SET cidr = ?, name = ?, schedule = ?, port_profile = ?, custom_ports = ?, enabled = ? WHERE id = ?`, append(args, id)...); err != nil {
			serverErr(w, err)
			return
		}
		in.ID = id
	}
	writeJSON(w, status, map[string]int64{"id": in.ID})
}

func (s *Server) handleDeleteScanTarget(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM scan_targets WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRunScanTarget(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Scanner.Enqueue(id)
	writeJSON(w, http.StatusAccepted, s.Scanner.Status())
}

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	type scanRow struct {
		ID         int64   `json:"id"`
		CIDR       string  `json:"cidr"`
		StartedAt  string  `json:"startedAt"`
		FinishedAt *string `json:"finishedAt"`
		Status     string  `json:"status"`
		HostsUp    int     `json:"hostsUp"`
		Error      string  `json:"error"`
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, cidr, started_at, finished_at, status, hosts_up, error FROM scans ORDER BY id DESC LIMIT 15`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	recent := []scanRow{}
	for rows.Next() {
		var sr scanRow
		var fin sql.NullString
		if err := rows.Scan(&sr.ID, &sr.CIDR, &sr.StartedAt, &fin, &sr.Status, &sr.HostsUp, &sr.Error); err != nil {
			serverErr(w, err)
			return
		}
		if fin.Valid {
			sr.FinishedAt = &fin.String
		}
		recent = append(recent, sr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"current": s.Scanner.Status(), "recent": recent})
}

// ── devices ───────────────────────────────────────────────────────────────

type DevicePort struct {
	Port     int    `json:"port"`
	Proto    string `json:"proto"`
	State    string `json:"state"`
	Service  string `json:"service"`
	Product  string `json:"product"`
	Version  string `json:"version"`
	Title    string `json:"title"`
	Favicon  string `json:"favicon"`
	LastSeen string `json:"lastSeen"`
}

type DeviceRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

type Device struct {
	ID           int64        `json:"id"`
	MAC          string       `json:"mac"`
	IP           string       `json:"ip"`
	Hostname     string       `json:"hostname"`
	CustomName   string       `json:"customName"`
	RouterName   string       `json:"routerName"`
	Vendor       string       `json:"vendor"`
	OSName       string       `json:"osName"`
	OSFamily     string       `json:"osFamily"`
	DeviceType   string       `json:"deviceType"`
	IsUp         bool         `json:"isUp"`
	Acknowledged bool         `json:"acknowledged"`
	Ignored      bool         `json:"ignored"`
	Notes        string       `json:"notes"`
	FirstSeen    string       `json:"firstSeen"`
	LastSeen     string       `json:"lastSeen"`
	Host         *DeviceRef   `json:"host"`
	Services     []DeviceRef  `json:"services"`
	Ports        []DevicePort `json:"ports"`
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.DB.QueryContext(ctx, `
		SELECT d.id, d.mac, d.ip, d.hostname, d.custom_name, d.router_name, d.vendor, d.os_name, d.os_family, d.device_type, d.is_up, d.acknowledged,
		       d.ignored, d.notes, d.first_seen, d.last_seen, d.host_id, COALESCE(h.name, '')
		FROM devices d LEFT JOIN hosts h ON h.id = d.host_id`)
	if err != nil {
		serverErr(w, err)
		return
	}
	var out []*Device
	byID := map[int64]*Device{}
	for rows.Next() {
		var d Device
		var hid sql.NullInt64
		var hname string
		if err := rows.Scan(&d.ID, &d.MAC, &d.IP, &d.Hostname, &d.CustomName, &d.RouterName, &d.Vendor, &d.OSName, &d.OSFamily, &d.DeviceType, &d.IsUp, &d.Acknowledged,
			&d.Ignored, &d.Notes, &d.FirstSeen, &d.LastSeen, &hid, &hname); err != nil {
			rows.Close()
			serverErr(w, err)
			return
		}
		if hid.Valid {
			d.Host = &DeviceRef{ID: hid.Int64, Name: hname}
		}
		d.Services, d.Ports = []DeviceRef{}, []DevicePort{}
		out = append(out, &d)
		byID[d.ID] = &d
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT device_id, port, proto, state, service_name, product, version, http_title, favicon, last_seen FROM device_ports ORDER BY port`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var id int64
		var p DevicePort
		if err := rows.Scan(&id, &p.Port, &p.Proto, &p.State, &p.Service, &p.Product, &p.Version, &p.Title, &p.Favicon, &p.LastSeen); err == nil {
			if d, ok := byID[id]; ok {
				d.Ports = append(d.Ports, p)
			}
		}
	}
	rows.Close()

	// Services whose direct IP matches the device.
	byIP := map[string][]*Device{}
	for _, d := range out {
		byIP[d.IP] = append(byIP[d.IP], d)
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT id, name, icon, direct_ip FROM services WHERE direct_ip != '' AND archived_at IS NULL`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var ref DeviceRef
		var ip string
		if rows.Scan(&ref.ID, &ref.Name, &ref.Icon, &ip) == nil {
			for _, d := range byIP[ip] {
				d.Services = append(d.Services, ref)
			}
		}
	}
	rows.Close()
	if out == nil {
		out = []*Device{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in struct {
		Acknowledged *bool   `json:"acknowledged"`
		Ignored      *bool   `json:"ignored"`
		Notes        *string `json:"notes"`
		CustomName   *string `json:"customName"`
		HostID       *int64  `json:"hostId"`
		ClearHost    bool    `json:"clearHost"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.CustomName != nil && len(*in.CustomName) > 80 {
		writeErr(w, http.StatusBadRequest, "name is too long (max 80)")
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `UPDATE devices SET acknowledged = COALESCE(?, acknowledged), ignored = COALESCE(?, ignored),
		notes = COALESCE(?, notes), custom_name = COALESCE(?, custom_name),
		host_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, host_id) END WHERE id = ?`,
		in.Acknowledged, in.Ignored, in.Notes, trimPtr(in.CustomName), in.ClearHost, in.HostID, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("devices", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAcknowledgeAll(w http.ResponseWriter, r *http.Request) {
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE devices SET acknowledged = 1 WHERE acknowledged = 0`); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("devices", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM devices WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("devices", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeviceEvents(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT ts, kind, detail FROM device_events WHERE device_id = ? ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	type ev struct {
		TS     string `json:"ts"`
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	}
	out := []ev{}
	for rows.Next() {
		var e ev
		if rows.Scan(&e.TS, &e.Kind, &e.Detail) == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

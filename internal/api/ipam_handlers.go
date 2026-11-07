package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bentonraymer/labdash/internal/ipam"
)

// ── IP usage: everything that claims an address ───────────────────────────

type IPOwner struct {
	Kind  string `json:"kind"` // assignment | host | service | device | router
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub"` // assignment kind / host kind / port / vendor
	Icon  string `json:"icon,omitempty"`
}

// ipUsage collects manual reservations, host IPs, service direct IPs and
// recently-seen scan results, keyed by IP string.
func (s *Server) ipUsage(ctx context.Context) (map[string][]IPOwner, error) {
	out := map[string][]IPOwner{}
	add := func(ip string, o IPOwner) {
		if ip = strings.TrimSpace(ip); ip != "" {
			out[ip] = append(out[ip], o)
		}
	}
	type q struct {
		sql  string
		kind string
	}
	queries := []q{
		{`SELECT id, ip, COALESCE(NULLIF(label, ''), (SELECT name FROM services WHERE id = service_id), (SELECT name FROM hosts WHERE id = host_id), ''), kind, '' FROM ip_assignments`, "assignment"},
		{`SELECT id, ip, name, kind, '' FROM hosts WHERE ip != ''`, "host"},
		{`SELECT id, direct_ip, name, COALESCE(CAST(direct_port AS TEXT), ''), icon FROM services WHERE direct_ip != ''`, "service"},
		{`SELECT id, ip, COALESCE(NULLIF(custom_name, ''), NULLIF(router_name, ''), NULLIF(hostname, ''), NULLIF(vendor, ''), mac), vendor, '' FROM devices
		  WHERE ip != '' AND ignored = 0 AND last_seen >= ?`, "device"},
	}
	for _, b := range s.routerSnapshot().Reservations {
		label := b.Name
		if label == "" {
			label = b.MAC
		}
		add(b.IP, IPOwner{Kind: "router", Label: label, Sub: b.MAC})
	}
	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	for _, qq := range queries {
		var rows *sql.Rows
		var err error
		if qq.kind == "device" {
			rows, err = s.DB.QueryContext(ctx, qq.sql, since)
		} else {
			rows, err = s.DB.QueryContext(ctx, qq.sql)
		}
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var o IPOwner
			var ip string
			if err := rows.Scan(&o.ID, &ip, &o.Label, &o.Sub, &o.Icon); err != nil {
				rows.Close()
				return nil, err
			}
			o.Kind = qq.kind
			add(ip, o)
		}
		rows.Close()
	}
	return out, nil
}

// ── subnets ───────────────────────────────────────────────────────────────

type SubnetView struct {
	ID        int64  `json:"id"`
	CIDR      string `json:"cidr"`
	Name      string `json:"name"`
	VLAN      *int   `json:"vlan"`
	Gateway   string `json:"gateway"`
	DHCPStart string `json:"dhcpStart"`
	DHCPEnd   string `json:"dhcpEnd"`
	Notes     string `json:"notes"`
	Used      int    `json:"used"`
	Total     int    `json:"total"`
	NextFree  string `json:"nextFree"`
}

func (s *Server) listSubnets(ctx context.Context) ([]SubnetView, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, cidr, name, vlan, gateway, dhcp_start, dhcp_end, notes FROM subnets`)
	if err != nil {
		return nil, err
	}
	var out []SubnetView
	for rows.Next() {
		var v SubnetView
		var vlan sql.NullInt64
		if err := rows.Scan(&v.ID, &v.CIDR, &v.Name, &vlan, &v.Gateway, &v.DHCPStart, &v.DHCPEnd, &v.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		if vlan.Valid {
			n := int(vlan.Int64)
			v.VLAN = &n
		}
		out = append(out, v)
	}
	rows.Close()
	usage, err := s.ipUsage(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		sn, err := ipam.Parse(out[i].CIDR, out[i].Gateway, out[i].DHCPStart, out[i].DHCPEnd)
		if err != nil {
			continue
		}
		used := map[netip.Addr]bool{}
		for ip := range usage {
			if a, err := netip.ParseAddr(ip); err == nil && sn.Prefix.Contains(a) {
				used[a] = true
			}
		}
		out[i].Used = len(used)
		out[i].Total = len(sn.All())
		if a, ok := sn.NextFree(used); ok {
			out[i].NextFree = a.String()
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := netip.ParsePrefix(out[i].CIDR)
		b, _ := netip.ParsePrefix(out[j].CIDR)
		return a.Addr().Less(b.Addr())
	})
	if out == nil {
		out = []SubnetView{}
	}
	return out, nil
}

func (s *Server) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	out, err := s.listSubnets(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type subnetInput struct {
	CIDR      string `json:"cidr"`
	Name      string `json:"name"`
	VLAN      *int   `json:"vlan"`
	Gateway   string `json:"gateway"`
	DHCPStart string `json:"dhcpStart"`
	DHCPEnd   string `json:"dhcpEnd"`
	Notes     string `json:"notes"`
}

func (s *Server) handleSaveSubnet(w http.ResponseWriter, r *http.Request) {
	var in subnetInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sn, err := ipam.Parse(strings.TrimSpace(in.CIDR), strings.TrimSpace(in.Gateway), strings.TrimSpace(in.DHCPStart), strings.TrimSpace(in.DHCPEnd))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{sn.Prefix.String(), strings.TrimSpace(in.Name), in.VLAN, strings.TrimSpace(in.Gateway), strings.TrimSpace(in.DHCPStart), strings.TrimSpace(in.DHCPEnd), in.Notes}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		_, err = s.DB.ExecContext(r.Context(), `INSERT INTO subnets(cidr, name, vlan, gateway, dhcp_start, dhcp_end, notes) VALUES (?, ?, ?, ?, ?, ?, ?)`, args...)
		status = http.StatusCreated
	} else {
		id, perr := idParam(r)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr.Error())
			return
		}
		_, err = s.DB.ExecContext(r.Context(), `UPDATE subnets SET cidr = ?, name = ?, vlan = ?, gateway = ?, dhcp_start = ?, dhcp_end = ?, notes = ? WHERE id = ?`, append(args, id)...)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "that subnet already exists")
			return
		}
		serverErr(w, err)
		return
	}
	writeJSON(w, status, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM subnets WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── subnet map ────────────────────────────────────────────────────────────

type IPCell struct {
	IP     string    `json:"ip"`
	State  string    `json:"state"` // network | broadcast | gateway | static | reserved | service | dhcp | discovered | free
	InDHCP bool      `json:"inDhcp"`
	Owners []IPOwner `json:"owners"`
}

func cellState(sn ipam.Subnet, a netip.Addr, owners []IPOwner) string {
	switch {
	case sn.Prefix.Bits() <= 30 && a == sn.Network():
		return "network"
	case sn.Prefix.Bits() <= 30 && a == sn.Broadcast():
		return "broadcast"
	case sn.Gateway.IsValid() && a == sn.Gateway:
		return "gateway"
	}
	has := map[string]bool{}
	for _, o := range owners {
		has[o.Kind] = true
		if o.Kind == "assignment" && o.Sub == "reserved" {
			has["reserved"] = true
		}
		if o.Kind == "assignment" && o.Sub == "dhcp" {
			has["dhcplease"] = true
		}
	}
	switch {
	case has["host"] || (has["assignment"] && !has["reserved"] && !has["dhcplease"]):
		return "static"
	case has["service"]:
		return "service"
	case has["router"]:
		return "lease"
	case has["reserved"]:
		return "reserved"
	case has["dhcplease"]:
		return "dhcp"
	case has["device"]:
		if sn.InDHCP(a) {
			return "dhcp"
		}
		return "discovered" // seen on the network but nobody claimed it
	case sn.InDHCP(a):
		return "dhcp"
	}
	return "free"
}

func (s *Server) handleSubnetMap(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var v subnetInput
	if err := s.DB.QueryRowContext(r.Context(), `SELECT cidr, gateway, dhcp_start, dhcp_end FROM subnets WHERE id = ?`, id).
		Scan(&v.CIDR, &v.Gateway, &v.DHCPStart, &v.DHCPEnd); err != nil {
		writeErr(w, http.StatusNotFound, "subnet not found")
		return
	}
	sn, err := ipam.Parse(v.CIDR, v.Gateway, v.DHCPStart, v.DHCPEnd)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	usage, err := s.ipUsage(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	cells := make([]IPCell, 0, 256)
	for _, a := range sn.All() {
		owners := usage[a.String()]
		if owners == nil {
			owners = []IPOwner{}
		}
		cells = append(cells, IPCell{IP: a.String(), State: cellState(sn, a, owners), InDHCP: sn.InDHCP(a), Owners: owners})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cells": cells})
}

// ── single IP: owners + port view ─────────────────────────────────────────

type PortRow struct {
	Port        int    `json:"port"`
	Proto       string `json:"proto"`
	Source      string `json:"source"` // service | claim
	ServiceID   *int64 `json:"serviceId"`
	ServiceName string `json:"serviceName"`
	ServiceIcon string `json:"serviceIcon"`
	Label       string `json:"label"`
	ClaimID     *int64 `json:"claimId"`
	// From scans (phase 5)
	Open     *bool  `json:"open"`
	Banner   string `json:"banner"`
	Title    string `json:"title"`
	Favicon  string `json:"favicon"`
	LastSeen string `json:"lastSeen"`
}

func (s *Server) portRows(ctx context.Context, ip string) ([]PortRow, error) {
	byKey := map[string]*PortRow{}
	var order []string
	get := func(port int, proto string) *PortRow {
		k := fmt.Sprintf("%d/%s", port, proto)
		if p, ok := byKey[k]; ok {
			return p
		}
		p := &PortRow{Port: port, Proto: proto}
		byKey[k] = p
		order = append(order, k)
		return p
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, icon, direct_port FROM services WHERE direct_ip = ? AND direct_port IS NOT NULL`, ip)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name, icon string
		var port int
		if err := rows.Scan(&id, &name, &icon, &port); err != nil {
			rows.Close()
			return nil, err
		}
		p := get(port, "tcp")
		p.Source, p.ServiceID, p.ServiceName, p.ServiceIcon = "service", &id, name, icon
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT pc.id, pc.port, pc.proto, pc.label, pc.service_id, COALESCE(sv.name, ''), COALESCE(sv.icon, '')
		FROM port_claims pc LEFT JOIN services sv ON sv.id = pc.service_id WHERE pc.ip = ?`, ip)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid int64
		var port int
		var proto, label, name, icon string
		var sid sql.NullInt64
		if err := rows.Scan(&cid, &port, &proto, &label, &sid, &name, &icon); err != nil {
			rows.Close()
			return nil, err
		}
		p := get(port, proto)
		if p.Source == "" {
			p.Source = "claim"
		}
		p.ClaimID = &cid
		p.Label = label
		if sid.Valid && p.ServiceID == nil {
			p.ServiceID, p.ServiceName, p.ServiceIcon = &sid.Int64, name, icon
		}
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT dp.port, dp.proto, dp.state, TRIM(dp.service_name || ' ' || dp.product || ' ' || dp.version), dp.http_title, dp.favicon, dp.last_seen
		FROM device_ports dp JOIN devices d ON d.id = dp.device_id WHERE d.ip = ?`, ip)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var port int
		var proto, state, banner, title, fav, seen string
		if err := rows.Scan(&port, &proto, &state, &banner, &title, &fav, &seen); err != nil {
			rows.Close()
			return nil, err
		}
		p := get(port, proto)
		open := state == "open"
		p.Open, p.Banner, p.Title, p.Favicon, p.LastSeen = &open, banner, title, fav, seen
		if p.Source == "" {
			p.Source = "scan"
		}
	}
	rows.Close()

	out := make([]PortRow, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Port < out[j].Port || (out[i].Port == out[j].Port && out[i].Proto < out[j].Proto)
	})
	return out, nil
}

func (s *Server) handleIPDetail(w http.ResponseWriter, r *http.Request) {
	ipStr := chi.URLParam(r, "ip")
	a, err := netip.ParseAddr(ipStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid IP")
		return
	}
	ctx := r.Context()
	usage, err := s.ipUsage(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	ports, err := s.portRows(ctx, a.String())
	if err != nil {
		serverErr(w, err)
		return
	}
	subnets, err := s.listSubnets(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	var subnet *SubnetView
	state := "unmanaged"
	for i := range subnets {
		sn, err := ipam.Parse(subnets[i].CIDR, subnets[i].Gateway, subnets[i].DHCPStart, subnets[i].DHCPEnd)
		if err == nil && sn.Prefix.Contains(a) {
			subnet = &subnets[i]
			state = cellState(sn, a, usage[a.String()])
			break
		}
	}
	owners := usage[a.String()]
	if owners == nil {
		owners = []IPOwner{}
	}
	// The host record at this IP (for WoL/quick actions in later phases).
	var mac string
	_ = s.DB.QueryRowContext(ctx, `SELECT mac FROM hosts WHERE ip = ? AND mac != '' LIMIT 1`, a.String()).Scan(&mac)
	if mac == "" {
		_ = s.DB.QueryRowContext(ctx, `SELECT mac FROM devices WHERE ip = ? AND mac != '' ORDER BY last_seen DESC LIMIT 1`, a.String()).Scan(&mac)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ip": a.String(), "state": state, "subnet": subnet, "owners": owners, "ports": ports, "mac": mac})
}

func (s *Server) usedPorts(ctx context.Context, ip string) (map[int]bool, error) {
	rows, err := s.portRows(ctx, ip)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, p := range rows {
		if p.Proto == "tcp" && (p.Source != "scan" || (p.Open != nil && *p.Open)) {
			used[p.Port] = true
		}
	}
	return used, nil
}

func (s *Server) handleFreePort(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	used, err := s.usedPorts(r.Context(), ip)
	if err != nil {
		serverErr(w, err)
		return
	}
	p, ok := ipam.NextFreePort(from, used)
	if !ok {
		writeErr(w, http.StatusConflict, "no free ports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"port": p})
}

// ── manual reservations & port claims ─────────────────────────────────────

type assignmentInput struct {
	IP        string `json:"ip"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Notes     string `json:"notes"`
	ServiceID *int64 `json:"serviceId"`
	HostID    *int64 `json:"hostId"`
}

func (s *Server) handleSaveAssignment(w http.ResponseWriter, r *http.Request) {
	var in assignmentInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid IP")
		return
	}
	if oneOf("static", "reserved", "dhcp")(in.Kind) != nil {
		writeErr(w, http.StatusBadRequest, "kind must be static, reserved or dhcp")
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `INSERT INTO ip_assignments(ip, kind, label, notes, service_id, host_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET kind = excluded.kind, label = excluded.label, notes = excluded.notes,
		service_id = excluded.service_id, host_id = excluded.host_id`,
		a.String(), in.Kind, strings.TrimSpace(in.Label), in.Notes, in.ServiceID, in.HostID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteAssignment(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM ip_assignments WHERE ip = ?`, ip); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type portClaimInput struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Proto     string `json:"proto"`
	ServiceID *int64 `json:"serviceId"`
	Label     string `json:"label"`
}

func (in *portClaimInput) validate() error {
	a, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		return errors.New("invalid IP")
	}
	in.IP = a.String()
	if in.Port < 1 || in.Port > 65535 {
		return errors.New("port must be 1–65535")
	}
	if in.Proto == "" {
		in.Proto = "tcp"
	}
	if oneOf("tcp", "udp")(in.Proto) != nil {
		return errors.New("proto must be tcp or udp")
	}
	return nil
}

func (s *Server) handleCreatePortClaim(w http.ResponseWriter, r *http.Request) {
	var in portClaimInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err := s.DB.ExecContext(r.Context(), `INSERT INTO port_claims(ip, port, proto, service_id, label) VALUES (?, ?, ?, ?, ?)`,
		in.IP, in.Port, in.Proto, in.ServiceID, strings.TrimSpace(in.Label))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, fmt.Sprintf("%s:%d/%s is already claimed", in.IP, in.Port, in.Proto))
			return
		}
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleDeletePortClaim(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM port_claims WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── conflict check (used live by the Add Service wizard) ──────────────────

func (s *Server) handleIPCheck(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ipStr := strings.TrimSpace(q.Get("ip"))
	port, _ := strconv.Atoi(q.Get("port"))
	exclude, _ := strconv.ParseInt(q.Get("excludeService"), 10, 64)
	out := map[string]any{"valid": false}
	a, err := netip.ParseAddr(ipStr)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["valid"] = true
	ctx := r.Context()
	usage, err := s.ipUsage(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	owners := []IPOwner{}
	seen := false
	for _, o := range usage[a.String()] {
		if o.Kind == "service" && o.ID == exclude {
			continue
		}
		if o.Kind == "device" {
			seen = true
		}
		owners = append(owners, o)
	}
	out["owners"] = owners
	out["seenOnNetwork"] = seen
	subnets, _ := s.listSubnets(ctx)
	for _, sv := range subnets {
		sn, err := ipam.Parse(sv.CIDR, sv.Gateway, sv.DHCPStart, sv.DHCPEnd)
		if err == nil && sn.Prefix.Contains(a) {
			out["subnet"] = sv.Name + " (" + sv.CIDR + ")"
			out["inDhcp"] = sn.InDHCP(a)
			out["reserved"] = sn.Reserved(a)
			break
		}
	}
	if port > 0 {
		rows, err := s.portRows(ctx, a.String())
		if err != nil {
			serverErr(w, err)
			return
		}
		for _, p := range rows {
			if p.Port != port || p.Proto != "tcp" {
				continue
			}
			if p.ServiceID != nil && *p.ServiceID == exclude {
				continue
			}
			if p.Source == "scan" && (p.Open == nil || !*p.Open) {
				continue
			}
			who := p.ServiceName
			if who == "" {
				who = p.Label
			}
			if who == "" && p.Source == "scan" {
				who = "an open port found by a scan"
			}
			out["portOwner"] = who
		}
	}
	writeJSON(w, http.StatusOK, out)
}

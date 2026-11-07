package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strings"
)

type Host struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ParentID *int64 `json:"parentId"`
	VMID     *int   `json:"vmid"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	OS       string `json:"os"`
	Notes    string `json:"notes"`
	// Derived
	NodeName     string `json:"nodeName"` // top-most ancestor (usually the Proxmox node)
	ServiceCount int    `json:"serviceCount"`
}

var hostKinds = []string{"proxmox_node", "server", "nas", "router", "vm", "lxc", "device"}

func (s *Server) listHosts(ctx context.Context) ([]Host, map[int64]*Host, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT h.id, h.name, h.kind, h.parent_id, h.vmid, h.ip, h.mac, h.os, h.notes,
		       (SELECT COUNT(*) FROM deployments d WHERE d.host_id = h.id)
		FROM hosts h ORDER BY h.name COLLATE NOCASE`)
	if err != nil {
		return nil, nil, err
	}
	var out []Host
	for rows.Next() {
		var h Host
		var parent, vmid sql.NullInt64
		if err := rows.Scan(&h.ID, &h.Name, &h.Kind, &parent, &vmid, &h.IP, &h.MAC, &h.OS, &h.Notes, &h.ServiceCount); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if parent.Valid {
			h.ParentID = &parent.Int64
		}
		if vmid.Valid {
			v := int(vmid.Int64)
			h.VMID = &v
		}
		out = append(out, h)
	}
	rows.Close()
	byID := make(map[int64]*Host, len(out))
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for i := range out {
		out[i].NodeName = rootName(byID, out[i].ID)
	}
	if out == nil {
		out = []Host{}
	}
	return out, byID, nil
}

// rootName walks parent links to the top-most host (cycle-safe).
func rootName(byID map[int64]*Host, id int64) string {
	h := byID[id]
	for depth := 0; h != nil && h.ParentID != nil && depth < 10; depth++ {
		p, ok := byID[*h.ParentID]
		if !ok {
			break
		}
		h = p
	}
	if h == nil {
		return ""
	}
	return h.Name
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, _, err := s.listHosts(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

type hostInput struct {
	Name        *string `json:"name"`
	Kind        *string `json:"kind"`
	ParentID    *int64  `json:"parentId"`
	ClearParent bool    `json:"clearParent"`
	VMID        *int    `json:"vmid"`
	ClearVMID   bool    `json:"clearVmid"`
	IP          *string `json:"ip"`
	MAC         *string `json:"mac"`
	OS          *string `json:"os"`
	Notes       *string `json:"notes"`
}

func (in hostInput) validate() error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errors.New("name is required")
	}
	if in.Kind != nil && oneOf(hostKinds...)(*in.Kind) != nil {
		return errors.New("invalid host kind")
	}
	if in.IP != nil && strings.TrimSpace(*in.IP) != "" {
		if _, err := netip.ParseAddr(strings.TrimSpace(*in.IP)); err != nil {
			return errors.New("invalid IP address")
		}
	}
	return nil
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var in hostInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Name == nil {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO hosts(name, kind, parent_id, vmid, ip, mac, os, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(*in.Name), deref(in.Kind, "server"), in.ParentID, in.VMID, strings.TrimSpace(deref(in.IP, "")),
		strings.ToLower(strings.TrimSpace(deref(in.MAC, ""))), deref(in.OS, ""), deref(in.Notes, ""))
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in hostInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.ParentID != nil && *in.ParentID == id {
		writeErr(w, http.StatusBadRequest, "a host can't be its own parent")
		return
	}
	var mac *string
	if in.MAC != nil {
		v := strings.ToLower(strings.TrimSpace(*in.MAC))
		mac = &v
	}
	_, err = s.DB.ExecContext(r.Context(), `UPDATE hosts SET
		name = COALESCE(?, name), kind = COALESCE(?, kind),
		parent_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, parent_id) END,
		vmid = CASE WHEN ? THEN NULL ELSE COALESCE(?, vmid) END,
		ip = COALESCE(?, ip), mac = COALESCE(?, mac), os = COALESCE(?, os), notes = COALESCE(?, notes)
		WHERE id = ?`,
		trimPtr(in.Name), in.Kind, in.ClearParent, in.ParentID, in.ClearVMID, in.VMID, trimPtr(in.IP), mac, in.OS, in.Notes, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM hosts WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── deployments (where a service runs) ────────────────────────────────────

type Hosting struct {
	HostID        *int64 `json:"hostId"`
	HostName      string `json:"hostName"`
	HostKind      string `json:"hostKind"`
	NodeName      string `json:"nodeName"`
	Runtime       string `json:"runtime"`
	VMID          *int   `json:"vmid"`
	ContainerName string `json:"containerName"`
}

func (s *Server) loadHostings(ctx context.Context) (map[int64]*Hosting, error) {
	_, hosts, err := s.listHosts(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT service_id, host_id, runtime, vmid, container_name FROM deployments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*Hosting{}
	for rows.Next() {
		var sid int64
		var hid, vmid sql.NullInt64
		var h Hosting
		if err := rows.Scan(&sid, &hid, &h.Runtime, &vmid, &h.ContainerName); err != nil {
			return nil, err
		}
		if hid.Valid {
			h.HostID = &hid.Int64
			if host, ok := hosts[hid.Int64]; ok {
				h.HostName, h.HostKind = host.Name, host.Kind
				h.NodeName = rootName(hosts, host.ID)
			}
		}
		if vmid.Valid {
			v := int(vmid.Int64)
			h.VMID = &v
		}
		out[sid] = &h
	}
	return out, rows.Err()
}

type deploymentInput struct {
	HostID        *int64 `json:"hostId"`
	Runtime       string `json:"runtime"`
	VMID          *int   `json:"vmid"`
	ContainerName string `json:"containerName"`
}

func (in deploymentInput) validate() error {
	if oneOf("vm", "lxc", "docker", "bare_metal", "other")(in.Runtime) != nil {
		return errors.New("runtime must be vm, lxc, docker, bare_metal or other")
	}
	if in.VMID != nil && (*in.VMID < 100 || *in.VMID > 999999999) {
		return errors.New("Proxmox VMIDs are 100 or higher")
	}
	return nil
}

func upsertDeployment(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, serviceID int64, in deploymentInput) error {
	_, err := ex.ExecContext(ctx, `INSERT INTO deployments(service_id, host_id, runtime, vmid, container_name) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(service_id) DO UPDATE SET host_id = excluded.host_id, runtime = excluded.runtime,
		vmid = excluded.vmid, container_name = excluded.container_name`,
		serviceID, in.HostID, in.Runtime, in.VMID, strings.TrimSpace(in.ContainerName))
	return err
}

// handlePutDeployment sets where a service runs; a body of null removes it.
func (s *Server) handlePutDeployment(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in *deploymentInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in == nil {
		_, err = s.DB.ExecContext(r.Context(), `DELETE FROM deployments WHERE service_id = ?`, id)
	} else {
		if verr := in.validate(); verr != nil {
			writeErr(w, http.StatusBadRequest, verr.Error())
			return
		}
		err = upsertDeployment(r.Context(), s.DB, id, *in)
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

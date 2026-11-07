package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bentonraymer/labdash/internal/urls"
)

type Section struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Position  int    `json:"position"`
	Collapsed bool   `json:"collapsed"`
	TileSize  string `json:"tileSize"`
}

type QuickLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Service struct {
	ID           int64       `json:"id"`
	SectionID    *int64      `json:"sectionId"`
	Name         string      `json:"name"`
	Icon         string      `json:"icon"`
	Description  string      `json:"description"`
	Position     int         `json:"position"`
	DNSURL       string      `json:"dnsUrl"`
	DirectScheme string      `json:"directScheme"`
	DirectIP     string      `json:"directIp"`
	DirectPort   *int        `json:"directPort"`
	DirectPath   string      `json:"directPath"`
	QuickLinks   []QuickLink `json:"quickLinks"`
	Tags         []string    `json:"tags"`
	NewTab       bool        `json:"newTab"`
	URL          string      `json:"url"`       // resolved for current access mode
	DirectURL    string      `json:"directUrl"` // always the IP form
	Status       string      `json:"status"`    // from monitor; 'unknown' if none
	LatencyMs    *int        `json:"latencyMs"`
	Hosting      *Hosting    `json:"hosting"` // last check latency of the service's monitor
}

const serviceCols = `id, section_id, name, icon, description, position, dns_url, direct_scheme,
	direct_ip, direct_port, direct_path, quick_links, tags, new_tab`

func scanService(sc interface{ Scan(...any) error }, mode string) (*Service, error) {
	var sv Service
	var port sql.NullInt64
	var sec sql.NullInt64
	var ql, tags string
	if err := sc.Scan(&sv.ID, &sec, &sv.Name, &sv.Icon, &sv.Description, &sv.Position, &sv.DNSURL,
		&sv.DirectScheme, &sv.DirectIP, &port, &sv.DirectPath, &ql, &tags, &sv.NewTab); err != nil {
		return nil, err
	}
	if sec.Valid {
		sv.SectionID = &sec.Int64
	}
	if port.Valid {
		p := int(port.Int64)
		sv.DirectPort = &p
	}
	_ = json.Unmarshal([]byte(ql), &sv.QuickLinks)
	_ = json.Unmarshal([]byte(tags), &sv.Tags)
	if sv.QuickLinks == nil {
		sv.QuickLinks = []QuickLink{}
	}
	if sv.Tags == nil {
		sv.Tags = []string{}
	}
	t := urls.Target{DNSURL: sv.DNSURL, DirectScheme: sv.DirectScheme, DirectIP: sv.DirectIP, DirectPort: sv.DirectPort, DirectPath: sv.DirectPath}
	sv.URL = t.Resolve(mode)
	sv.DirectURL = t.Direct()
	sv.Status = "unknown"
	return &sv, nil
}

func (s *Server) listServices(ctx context.Context) ([]*Service, error) {
	mode := s.accessMode(ctx)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+serviceCols+` FROM services ORDER BY position, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Service{}
	for rows.Next() {
		sv, err := scanService(rows, mode)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // release the connection before the next query
	if hostings, err := s.loadHostings(ctx); err == nil {
		for _, sv := range out {
			sv.Hosting = hostings[sv.ID]
		}
	}
	// Overlay monitor status (worst status of a service's monitors).
	st, err := s.serviceStatuses(ctx)
	if err == nil {
		for _, sv := range out {
			if v, ok := st[sv.ID]; ok {
				sv.Status = v.status
				sv.LatencyMs = v.latency
			}
		}
	}
	return out, nil
}

type svcStatus struct {
	status  string
	latency *int
}

func (s *Server) serviceStatuses(ctx context.Context) (map[int64]svcStatus, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT service_id, status, last_latency_ms FROM monitors WHERE service_id IS NOT NULL AND enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Worst status wins: down > up > maintenance > pending.
	rank := map[string]int{"down": 4, "up": 3, "maintenance": 2, "pending": 1}
	out := map[int64]svcStatus{}
	for rows.Next() {
		var id int64
		var st string
		var lat sql.NullInt64
		if err := rows.Scan(&id, &st, &lat); err != nil {
			return nil, err
		}
		cur, ok := out[id]
		if !ok || rank[st] > rank[cur.status] {
			cur.status = st
		}
		if lat.Valid && st == "up" {
			v := int(lat.Int64)
			cur.latency = &v
		}
		out[id] = cur
	}
	return out, rows.Err()
}

func (s *Server) listSections(ctx context.Context) ([]Section, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, icon, position, collapsed, tile_size FROM sections ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Section{}
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Icon, &sec.Position, &sec.Collapsed, &sec.TileSize); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// ── launchpad ─────────────────────────────────────────────────────────────

func (s *Server) handleLaunchpad(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	secs, err := s.listSections(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	svcs, err := s.listServices(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	st, err := s.loadSettings(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": secs, "services": svcs, "settings": st})
}

// handleLayout persists drag-and-drop order: section order and, per section,
// the ordered service IDs (moving services between sections as needed).
func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sections []struct {
			ID         int64   `json:"id"`
			ServiceIDs []int64 `json:"serviceIds"`
		} `json:"sections"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()
	for i, sec := range body.Sections {
		if _, err := tx.Exec(`UPDATE sections SET position = ? WHERE id = ?`, i, sec.ID); err != nil {
			serverErr(w, err)
			return
		}
		for j, sid := range sec.ServiceIDs {
			if _, err := tx.Exec(`UPDATE services SET section_id = ?, position = ? WHERE id = ?`, sec.ID, j, sid); err != nil {
				serverErr(w, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── sections ──────────────────────────────────────────────────────────────

type sectionInput struct {
	Name      *string `json:"name"`
	Icon      *string `json:"icon"`
	Collapsed *bool   `json:"collapsed"`
	TileSize  *string `json:"tileSize"`
}

func (in sectionInput) validate() error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errors.New("name is required")
	}
	if in.TileSize != nil && oneOf("sm", "md", "lg")(*in.TileSize) != nil {
		return errors.New("tileSize must be sm, md or lg")
	}
	return nil
}

func (s *Server) handleListSections(w http.ResponseWriter, r *http.Request) {
	secs, err := s.listSections(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, secs)
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	var in sectionInput
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
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO sections(name, icon, tile_size, position)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM sections))`,
		strings.TrimSpace(*in.Name), deref(in.Icon, ""), deref(in.TileSize, "md"))
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in sectionInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `UPDATE sections SET
		name = COALESCE(?, name), icon = COALESCE(?, icon), collapsed = COALESCE(?, collapsed), tile_size = COALESCE(?, tile_size)
		WHERE id = ?`, trimPtr(in.Name), in.Icon, in.Collapsed, in.TileSize, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Services in the section become unsectioned rather than deleted.
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM sections WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── services ──────────────────────────────────────────────────────────────

type serviceInput struct {
	SectionID    *int64       `json:"sectionId"`
	Name         *string      `json:"name"`
	Icon         *string      `json:"icon"`
	Description  *string      `json:"description"`
	DNSURL       *string      `json:"dnsUrl"`
	DirectScheme *string      `json:"directScheme"`
	DirectIP     *string      `json:"directIp"`
	DirectPort   *int         `json:"directPort"`
	DirectPath   *string      `json:"directPath"`
	QuickLinks   *[]QuickLink `json:"quickLinks"`
	Tags         *[]string    `json:"tags"`
	NewTab       *bool        `json:"newTab"`
	// ClearPort/ClearSection let PATCH set nullable fields to NULL.
	ClearPort    bool `json:"clearPort"`
	ClearSection bool `json:"clearSection"`
}

func (in serviceInput) validate() error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errors.New("name is required")
	}
	if in.DirectScheme != nil && oneOf("http", "https")(*in.DirectScheme) != nil {
		return errors.New("directScheme must be http or https")
	}
	if in.DirectPort != nil && (*in.DirectPort < 1 || *in.DirectPort > 65535) {
		return errors.New("directPort must be 1–65535")
	}
	if in.DNSURL != nil && *in.DNSURL != "" && !strings.HasPrefix(*in.DNSURL, "http://") && !strings.HasPrefix(*in.DNSURL, "https://") {
		return errors.New("dnsUrl must start with http:// or https://")
	}
	return nil
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	svcs, err := s.listServices(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, svcs)
}

func (s *Server) getService(ctx context.Context, id int64) (*Service, error) {
	mode := s.accessMode(ctx) // before QueryRow: the row holds the connection until Scan
	row := s.DB.QueryRowContext(ctx, `SELECT `+serviceCols+` FROM services WHERE id = ?`, id)
	sv, err := scanService(row, mode)
	if err != nil {
		return nil, err
	}
	if hostings, err := s.loadHostings(ctx); err == nil {
		sv.Hosting = hostings[sv.ID]
	}
	if st, err := s.serviceStatuses(ctx); err == nil {
		if v, ok := st[sv.ID]; ok {
			sv.Status, sv.LatencyMs = v.status, v.latency
		}
	}
	return sv, nil
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sv, err := s.getService(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sv)
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	var in serviceInput
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
	ql, _ := json.Marshal(derefSlice(in.QuickLinks))
	tags, _ := json.Marshal(derefSlice(in.Tags))
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO services
		(section_id, name, icon, description, dns_url, direct_scheme, direct_ip, direct_port, direct_path, quick_links, tags, new_tab, position)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		 (SELECT COALESCE(MAX(position), -1) + 1 FROM services WHERE section_id IS ?))`,
		in.SectionID, strings.TrimSpace(*in.Name), deref(in.Icon, ""), deref(in.Description, ""),
		strings.TrimSpace(deref(in.DNSURL, "")), deref(in.DirectScheme, "http"), strings.TrimSpace(deref(in.DirectIP, "")),
		in.DirectPort, deref(in.DirectPath, ""), string(ql), string(tags), deref(in.NewTab, true), in.SectionID)
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	sv, err := s.getService(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusCreated, sv)
}

func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in serviceInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ql, tags *string
	if in.QuickLinks != nil {
		b, _ := json.Marshal(*in.QuickLinks)
		v := string(b)
		ql = &v
	}
	if in.Tags != nil {
		b, _ := json.Marshal(*in.Tags)
		v := string(b)
		tags = &v
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE services SET
		section_id    = CASE WHEN ? THEN NULL ELSE COALESCE(?, section_id) END,
		name          = COALESCE(?, name),
		icon          = COALESCE(?, icon),
		description   = COALESCE(?, description),
		dns_url       = COALESCE(?, dns_url),
		direct_scheme = COALESCE(?, direct_scheme),
		direct_ip     = COALESCE(?, direct_ip),
		direct_port   = CASE WHEN ? THEN NULL ELSE COALESCE(?, direct_port) END,
		direct_path   = COALESCE(?, direct_path),
		quick_links   = COALESCE(?, quick_links),
		tags          = COALESCE(?, tags),
		new_tab       = COALESCE(?, new_tab),
		updated_at    = ?
		WHERE id = ?`,
		in.ClearSection, in.SectionID, trimPtr(in.Name), in.Icon, in.Description, trimPtr(in.DNSURL), in.DirectScheme,
		trimPtr(in.DirectIP), in.ClearPort, in.DirectPort, in.DirectPath, ql, tags, in.NewTab, now(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	sv, err := s.getService(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, sv)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM services WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── spotlight frecency ────────────────────────────────────────────────────

func (s *Server) handleSpotlightUsage(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT kind, ref, count, last_used FROM spotlight_usage`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	type usage struct {
		Kind     string `json:"kind"`
		Ref      string `json:"ref"`
		Count    int    `json:"count"`
		LastUsed string `json:"lastUsed"`
	}
	out := []usage{}
	for rows.Next() {
		var u usage
		if err := rows.Scan(&u.Kind, &u.Ref, &u.Count, &u.LastUsed); err != nil {
			serverErr(w, err)
			return
		}
		out = append(out, u)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSpotlightRecord(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	}
	if err := readJSON(r, &body); err != nil || body.Kind == "" || body.Ref == "" {
		writeErr(w, http.StatusBadRequest, "kind and ref required")
		return
	}
	_, err := s.DB.ExecContext(r.Context(), `INSERT INTO spotlight_usage(kind, ref, count, last_used) VALUES (?, ?, 1, ?)
		ON CONFLICT(kind, ref) DO UPDATE SET count = count + 1, last_used = excluded.last_used`, body.Kind, body.Ref, now())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── small helpers ─────────────────────────────────────────────────────────

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func derefSlice[T any](p *[]T) []T {
	if p == nil || *p == nil {
		return []T{}
	}
	return *p
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

package api

import (
	"net/http"
	"strings"

	"github.com/bentonraymer/labdash/internal/importer"
)

type importPreviewService struct {
	importer.Service
	Duplicate bool `json:"duplicate"`
}

type importPreviewSection struct {
	Name      string                 `json:"name"`
	Icon      string                 `json:"icon"`
	Collapsed bool                   `json:"collapsed"`
	Exists    bool                   `json:"exists"`
	Services  []importPreviewService `json:"services"`
}

// handleImportDashy previews (commit=false) or applies (commit=true) a Dashy
// conf.yml import. Sections are matched by name; services whose name already
// exists anywhere are skipped so re-running an import is safe.
func (s *Server) handleImportDashy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		YAML   string `json:"yaml"`
		Commit bool   `json:"commit"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed, err := importer.ParseDashy([]byte(body.YAML))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()

	sectionIDs := map[string]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name FROM sections`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			sectionIDs[strings.ToLower(name)] = id
		}
	}
	rows.Close()

	existing := map[string]bool{}
	rows, err = s.DB.QueryContext(ctx, `SELECT name FROM services`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			existing[strings.ToLower(name)] = true
		}
	}
	rows.Close()

	preview := make([]importPreviewSection, 0, len(parsed))
	seen := map[string]bool{}
	newCount := 0
	for _, sec := range parsed {
		ps := importPreviewSection{Name: sec.Name, Icon: sec.Icon, Collapsed: sec.Collapsed}
		_, ps.Exists = sectionIDs[strings.ToLower(sec.Name)]
		for _, svc := range sec.Services {
			key := strings.ToLower(svc.Name)
			dup := existing[key] || seen[key]
			seen[key] = true
			if !dup {
				newCount++
			}
			ps.Services = append(ps.Services, importPreviewService{Service: svc, Duplicate: dup})
		}
		preview = append(preview, ps)
	}

	if !body.Commit {
		writeJSON(w, http.StatusOK, map[string]any{"sections": preview, "newServices": newCount})
		return
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()
	createdSections, createdServices := 0, 0
	for _, sec := range preview {
		key := strings.ToLower(sec.Name)
		secID, ok := sectionIDs[key]
		if !ok {
			res, err := tx.ExecContext(ctx, `INSERT INTO sections(name, icon, collapsed, position)
				VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM sections))`, sec.Name, sec.Icon, sec.Collapsed)
			if err != nil {
				serverErr(w, err)
				return
			}
			secID, _ = res.LastInsertId()
			sectionIDs[key] = secID
			createdSections++
		}
		for _, svc := range sec.Services {
			if svc.Duplicate {
				continue
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO services
				(section_id, name, icon, description, dns_url, direct_scheme, direct_ip, direct_port, direct_path, new_tab, position)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM services WHERE section_id = ?))`,
				secID, svc.Name, svc.Icon, svc.Description, svc.DNSURL, svc.DirectScheme, svc.DirectIP, svc.DirectPort, svc.DirectPath, svc.NewTab, secID)
			if err != nil {
				serverErr(w, err)
				return
			}
			createdServices++
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("launchpad", nil)
	writeJSON(w, http.StatusOK, map[string]int{"sections": createdSections, "services": createdServices})
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/glinet"
	"github.com/bentonraymer/labdash/internal/urls"
)

type wizardInput struct {
	Service    serviceInput     `json:"service"`
	Hosting    *deploymentInput `json:"hosting"`
	ReserveIP  bool             `json:"reserveIp"`
	ExtraPorts []struct {
		Port  int    `json:"port"`
		Proto string `json:"proto"`
		Label string `json:"label"`
	} `json:"extraPorts"`
	Monitor *struct {
		Kind      string `json:"kind"`
		IntervalS int    `json:"intervalS"`
	} `json:"monitor"`
	CreateNote        bool            `json:"createNote"`
	NPM               *npmCreateInput `json:"npm"`
	RouterReservation *struct {
		MAC string `json:"mac"`
	} `json:"routerReservation"`
}

type wizardStep struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// handleServiceWizard creates a service and everything around it — tile,
// hosting, IP reservation, port claims, monitor and docs page — in one
// transaction, so a failure leaves nothing half-created.
func (s *Server) handleServiceWizard(w http.ResponseWriter, r *http.Request) {
	var in wizardInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	svc := in.Service
	if svc.Name == nil || strings.TrimSpace(*svc.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := svc.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Hosting != nil {
		if err := in.Hosting.validate(); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.Monitor != nil && oneOf("http", "tcp", "ping", "dns")(in.Monitor.Kind) != nil {
		writeErr(w, http.StatusBadRequest, "invalid monitor type")
		return
	}
	ctx := r.Context()
	name := strings.TrimSpace(*svc.Name)
	ip := strings.TrimSpace(deref(svc.DirectIP, ""))
	steps := []wizardStep{}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()

	ql, _ := json.Marshal(derefSlice(svc.QuickLinks))
	tags, _ := json.Marshal(derefSlice(svc.Tags))
	res, err := tx.ExecContext(ctx, `INSERT INTO services
		(section_id, name, icon, description, dns_url, direct_scheme, direct_ip, direct_port, direct_path, quick_links, tags, new_tab, position)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM services WHERE section_id IS ?))`,
		svc.SectionID, name, deref(svc.Icon, ""), deref(svc.Description, ""), strings.TrimSpace(deref(svc.DNSURL, "")),
		deref(svc.DirectScheme, "http"), ip, svc.DirectPort, deref(svc.DirectPath, ""), string(ql), string(tags), deref(svc.NewTab, true), svc.SectionID)
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	steps = append(steps, wizardStep{"Service & launchpad tile", true, name})

	var hostName string
	if in.Hosting != nil {
		if err := upsertDeployment(ctx, tx, id, *in.Hosting); err != nil {
			serverErr(w, err)
			return
		}
		if in.Hosting.HostID != nil {
			_ = tx.QueryRowContext(ctx, `SELECT name FROM hosts WHERE id = ?`, *in.Hosting.HostID).Scan(&hostName)
		}
		detail := strings.ToUpper(in.Hosting.Runtime)
		if hostName != "" {
			detail += " on " + hostName
		}
		if in.Hosting.VMID != nil {
			detail += fmt.Sprintf(" (VMID %d)", *in.Hosting.VMID)
		}
		if in.Hosting.ContainerName != "" {
			detail += " · " + in.Hosting.ContainerName
		}
		steps = append(steps, wizardStep{"Hosting", true, detail})
	}

	if in.ReserveIP && ip != "" {
		var existing string
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(label, ''), kind) FROM ip_assignments WHERE ip = ?`, ip).Scan(&existing)
		if existing != "" {
			steps = append(steps, wizardStep{"IP reservation", true, ip + " already reserved (" + existing + ") — left as is"})
		} else {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ip_assignments(ip, kind, service_id, label) VALUES (?, 'static', ?, ?)`, ip, id, name); err != nil {
				serverErr(w, err)
				return
			}
			steps = append(steps, wizardStep{"IP reservation", true, ip + " marked static"})
		}
	}

	for _, p := range in.ExtraPorts {
		pc := portClaimInput{IP: ip, Port: p.Port, Proto: p.Proto, ServiceID: &id, Label: p.Label}
		if err := pc.validate(); err != nil {
			writeErr(w, http.StatusBadRequest, "extra port: "+err.Error())
			return
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO port_claims(ip, port, proto, service_id, label) VALUES (?, ?, ?, ?, ?)`,
			pc.IP, pc.Port, pc.Proto, id, pc.Label); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeErr(w, http.StatusConflict, fmt.Sprintf("port %d/%s on %s is already claimed", pc.Port, pc.Proto, pc.IP))
				return
			}
			serverErr(w, err)
			return
		}
		steps = append(steps, wizardStep{"Port claim", true, fmt.Sprintf("%s:%d/%s %s", pc.IP, pc.Port, pc.Proto, pc.Label)})
	}

	var monitorID int64
	if in.Monitor != nil {
		interval := in.Monitor.IntervalS
		if interval < 10 {
			interval = 60
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO monitors(service_id, kind, interval_s) VALUES (?, ?, ?)`, id, in.Monitor.Kind, interval)
		if err != nil {
			serverErr(w, err)
			return
		}
		monitorID, _ = res.LastInsertId()
		steps = append(steps, wizardStep{"Uptime monitor", true, fmt.Sprintf("%s every %ds", strings.ToUpper(in.Monitor.Kind), interval)})
	}

	if in.CreateNote {
		t := urls.Target{DNSURL: deref(svc.DNSURL, ""), DirectScheme: deref(svc.DirectScheme, "http"), DirectIP: ip, DirectPort: svc.DirectPort, DirectPath: deref(svc.DirectPath, "")}
		body := noteTemplate(name, t, hostName, in.Hosting)
		if _, err := tx.ExecContext(ctx, `INSERT INTO notes(service_id, title, body) VALUES (?, ?, ?)`, id, name, body); err != nil {
			serverErr(w, err)
			return
		}
		steps = append(steps, wizardStep{"Docs page", true, "Created “" + name + "” notes"})
	}

	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	s.reloadMonitors()
	s.Events.Publish("launchpad", nil)
	if monitorID > 0 && s.Monitor != nil {
		go func() {
			cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _, _ = s.Monitor.CheckNow(cctx, monitorID)
		}()
	}
	// Router reservation (GL.iNet), also external: reported, never rolled back.
	if in.RouterReservation != nil && ip != "" {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		detail, err := s.reserveOnRouter(rctx, glinet.StaticBind{Name: name, MAC: in.RouterReservation.MAC, IP: ip})
		cancel()
		steps = append(steps, wizardStep{"Router DHCP reservation", err == nil, firstNonEmpty(detail, errString(err))})
	}
	// NPM is an external system, so it runs after the commit: a failure is
	// reported as a failed step instead of undoing the service.
	if in.NPM != nil {
		nctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		detail, err := s.createProxyHostFor(nctx, id, *in.NPM)
		cancel()
		if err != nil {
			steps = append(steps, wizardStep{"Nginx Proxy Manager host", false, err.Error()})
		} else {
			steps = append(steps, wizardStep{"Nginx Proxy Manager host", true, detail})
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"serviceId": id, "steps": steps})
}

func noteTemplate(name string, t urls.Target, hostName string, h *deploymentInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n## Access\n", name)
	if t.DNSURL != "" {
		fmt.Fprintf(&b, "- **URL:** %s\n", t.DNSURL)
	}
	if d := t.Direct(); d != "" {
		fmt.Fprintf(&b, "- **Direct:** %s\n", d)
	}
	if h != nil {
		where := strings.ToUpper(h.Runtime)
		if hostName != "" {
			where += " on " + hostName
		}
		if h.VMID != nil {
			where += fmt.Sprintf(" · VMID %d", *h.VMID)
		}
		if h.ContainerName != "" {
			where += " · container `" + h.ContainerName + "`"
		}
		fmt.Fprintf(&b, "- **Runs as:** %s\n", where)
	}
	b.WriteString("\n## Configuration\n\n<!-- compose file, env vars, volumes, reverse proxy settings… -->\n\n## Maintenance\n\n- Update: \n- Backups: \n\n## Notes\n\n")
	return b.String()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

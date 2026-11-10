package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const tsFormat = "2006-01-02T15:04:05.000Z"

// NewDevice is an unknown device seen for the first time.
type NewDevice struct {
	ID       int64  `json:"id"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Vendor   string `json:"vendor"`
	Hostname string `json:"hostname"`
	Type     string `json:"type"`
	OS       string `json:"os"`
}

type Summary struct {
	HostsUp  int
	New      []NewDevice
	WentDown int
}

type known struct {
	hostByMAC map[string]int64
	hostByIP  map[string]int64
	serviceIP map[string]bool
}

func loadKnown(ctx context.Context, tx *sql.Tx) (known, error) {
	k := known{hostByMAC: map[string]int64{}, hostByIP: map[string]int64{}, serviceIP: map[string]bool{}}
	rows, err := tx.QueryContext(ctx, `SELECT id, lower(mac), ip FROM hosts`)
	if err != nil {
		return k, err
	}
	for rows.Next() {
		var id int64
		var mac, ip string
		if err := rows.Scan(&id, &mac, &ip); err != nil {
			rows.Close()
			return k, err
		}
		if mac != "" {
			k.hostByMAC[mac] = id
		}
		if ip != "" {
			k.hostByIP[ip] = id
		}
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT direct_ip FROM services WHERE direct_ip != '' AND archived_at IS NULL UNION SELECT ip FROM ip_assignments`)
	if err != nil {
		return k, err
	}
	for rows.Next() {
		var ip string
		if rows.Scan(&ip) == nil {
			k.serviceIP[ip] = true
		}
	}
	rows.Close()
	return k, nil
}

// Persist merges a scan of `cidr` into the device inventory. When baseline
// is true (a target's first scan) nothing is reported as new, so you don't
// get an alert for every device you already own.
func Persist(ctx context.Context, db *sql.DB, cidr string, hosts []Host, baseline bool) (Summary, error) {
	var sum Summary
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return sum, err
	}
	now := time.Now().UTC().Format(tsFormat)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return sum, err
	}
	defer tx.Rollback()
	k, err := loadKnown(ctx, tx)
	if err != nil {
		return sum, err
	}
	event := func(id int64, kind, detail string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO device_events(device_id, ts, kind, detail) VALUES (?, ?, ?, ?)`, id, now, kind, detail)
		return err
	}

	seen := map[int64]bool{}
	for _, h := range hosts {
		key := "ip:" + h.IP
		if h.MAC != "" {
			key = "mac:" + h.MAC
		}
		devType := ClassifyType(h)
		var hostID *int64
		if id, ok := k.hostByMAC[h.MAC]; ok && h.MAC != "" {
			hostID = &id
		} else if id, ok := k.hostByIP[h.IP]; ok {
			hostID = &id
		}
		isKnown := hostID != nil || k.serviceIP[h.IP]

		var id int64
		var oldIP string
		var wasUp bool
		err := tx.QueryRowContext(ctx, `SELECT id, ip, is_up FROM devices WHERE key = ?`, key).Scan(&id, &oldIP, &wasUp)
		isNew := errors.Is(err, sql.ErrNoRows)
		if err != nil && !isNew {
			return sum, err
		}
		if isNew {
			res, err := tx.ExecContext(ctx, `INSERT INTO devices(key, mac, ip, hostname, vendor, os_name, os_family, device_type, is_up,
				acknowledged, host_id, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
				key, h.MAC, h.IP, h.Hostname, h.Vendor, h.OSName, h.OSFamily, devType, isKnown || baseline, hostID, now, now)
			if err != nil {
				return sum, err
			}
			id, _ = res.LastInsertId()
			if err := event(id, "new", h.IP); err != nil {
				return sum, err
			}
			if !isKnown && !baseline {
				sum.New = append(sum.New, NewDevice{ID: id, IP: h.IP, MAC: h.MAC, Vendor: h.Vendor, Hostname: h.Hostname, Type: devType, OS: h.OSName})
			}
		} else {
			if oldIP != h.IP {
				if err := event(id, "ip_change", oldIP+" → "+h.IP); err != nil {
					return sum, err
				}
			}
			if !wasUp {
				if err := event(id, "up", ""); err != nil {
					return sum, err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET ip = ?, is_up = 1, last_seen = ?,
				hostname = CASE WHEN ? != '' THEN ? ELSE hostname END,
				vendor = CASE WHEN ? != '' THEN ? ELSE vendor END,
				os_name = CASE WHEN ? != '' THEN ? ELSE os_name END,
				os_family = CASE WHEN ? != '' THEN ? ELSE os_family END,
				device_type = ?, host_id = COALESCE(host_id, ?),
				acknowledged = CASE WHEN ? THEN 1 ELSE acknowledged END
				WHERE id = ?`,
				h.IP, now, h.Hostname, h.Hostname, h.Vendor, h.Vendor, h.OSName, h.OSName, h.OSFamily, h.OSFamily,
				devType, hostID, isKnown, id); err != nil {
				return sum, err
			}
		}
		seen[id] = true
		sum.HostsUp++

		if err := syncPorts(ctx, tx, id, h.Ports, now, !isNew, event); err != nil {
			return sum, err
		}
	}

	// Devices in this subnet that didn't answer are now down.
	rows, err := tx.QueryContext(ctx, `SELECT id, ip FROM devices WHERE is_up = 1`)
	if err != nil {
		return sum, err
	}
	var down []int64
	for rows.Next() {
		var id int64
		var ip string
		if rows.Scan(&id, &ip) != nil {
			continue
		}
		if a, err := netip.ParseAddr(ip); err == nil && prefix.Contains(a) && !seen[id] {
			down = append(down, id)
		}
	}
	rows.Close()
	for _, id := range down {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET is_up = 0 WHERE id = ?`, id); err != nil {
			return sum, err
		}
		if err := event(id, "down", ""); err != nil {
			return sum, err
		}
	}
	sum.WentDown = len(down)
	return sum, tx.Commit()
}

func syncPorts(ctx context.Context, tx *sql.Tx, deviceID int64, ports []Port, now string, recordEvents bool, event func(int64, string, string) error) error {
	prev := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT port, proto FROM device_ports WHERE device_id = ? AND state = 'open'`, deviceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p int
		var proto string
		if rows.Scan(&p, &proto) == nil {
			prev[fmt.Sprintf("%d/%s", p, proto)] = true
		}
	}
	rows.Close()

	cur := map[string]bool{}
	for _, p := range ports {
		k := fmt.Sprintf("%d/%s", p.Port, p.Proto)
		cur[k] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_ports(device_id, port, proto, state, service_name, product, version, http_title, favicon, last_seen, cpe)
			VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(device_id, port, proto) DO UPDATE SET state = 'open', service_name = excluded.service_name,
			  product = excluded.product, version = excluded.version,
			  cpe = CASE WHEN excluded.cpe != '' THEN excluded.cpe ELSE cpe END,
			  http_title = CASE WHEN excluded.http_title != '' THEN excluded.http_title ELSE http_title END,
			  favicon = CASE WHEN excluded.favicon != '' THEN excluded.favicon ELSE favicon END,
			  last_seen = excluded.last_seen`,
			deviceID, p.Port, p.Proto, p.Service, p.Product, strings.TrimSpace(p.Version), p.Title, p.Favicon, now, p.CPE); err != nil {
			return err
		}
		if recordEvents && !prev[k] {
			if err := event(deviceID, "port_open", k); err != nil {
				return err
			}
		}
	}
	for k := range prev {
		if cur[k] {
			continue
		}
		var port int
		var proto string
		fmt.Sscanf(strings.Replace(k, "/", " ", 1), "%d %s", &port, &proto)
		if _, err := tx.ExecContext(ctx, `UPDATE device_ports SET state = 'closed' WHERE device_id = ? AND port = ? AND proto = ?`, deviceID, port, proto); err != nil {
			return err
		}
		if recordEvents {
			if err := event(deviceID, "port_closed", k); err != nil {
				return err
			}
		}
	}
	return nil
}

package scanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestPersistLifecycle(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	d.Exec(`INSERT INTO hosts(id, name, ip, mac) VALUES (1, 'pve1', '192.168.8.11', 'aa:aa:aa:aa:aa:11')`)
	cidr := "192.168.8.0/24"

	router := Host{IP: "192.168.8.1", MAC: "94:83:c4:00:00:01", Vendor: "GL Technologies", Ports: []Port{{Port: 80, Proto: "tcp"}, {Port: 22, Proto: "tcp"}}}
	pve := Host{IP: "192.168.8.11", MAC: "aa:aa:aa:aa:aa:11", Ports: []Port{{Port: 8006, Proto: "tcp"}}}

	// Baseline scan: nothing reported as new.
	sum, err := Persist(ctx, d, cidr, []Host{router, pve}, true)
	if err != nil || sum.HostsUp != 2 || len(sum.New) != 0 {
		t.Fatalf("baseline: %+v %v", sum, err)
	}
	var hostID int64
	d.QueryRow(`SELECT host_id FROM devices WHERE mac = 'aa:aa:aa:aa:aa:11'`).Scan(&hostID)
	if hostID != 1 {
		t.Fatal("device should link to host by MAC")
	}

	// Second scan: a phone appears, the router drops port 22 and moves IP, pve goes away.
	phone := Host{IP: "192.168.8.150", MAC: "11:22:33:44:55:66", Vendor: "Apple"}
	router.IP = "192.168.8.2"
	router.Ports = []Port{{Port: 80, Proto: "tcp"}}
	sum, err = Persist(ctx, d, cidr, []Host{router, phone}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.New) != 1 || sum.New[0].IP != "192.168.8.150" || sum.WentDown != 1 {
		t.Fatalf("second scan: %+v", sum)
	}
	var kinds []string
	rows, _ := d.Query(`SELECT kind || ':' || detail FROM device_events WHERE device_id = (SELECT id FROM devices WHERE mac = '94:83:c4:00:00:01') ORDER BY id`)
	for rows.Next() {
		var k string
		rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	want := []string{"new:192.168.8.1", "ip_change:192.168.8.1 → 192.168.8.2", "port_closed:22/tcp"}
	if len(kinds) != len(want) {
		t.Fatalf("router events = %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("router events = %v", kinds)
		}
	}
	var state string
	d.QueryRow(`SELECT state FROM device_ports dp JOIN devices dv ON dv.id = dp.device_id WHERE dv.mac = '94:83:c4:00:00:01' AND port = 22`).Scan(&state)
	if state != "closed" {
		t.Fatalf("port 22 state = %q", state)
	}
	var up int
	d.QueryRow(`SELECT is_up FROM devices WHERE mac = 'aa:aa:aa:aa:aa:11'`).Scan(&up)
	if up != 0 {
		t.Fatal("pve should be marked down")
	}
	// A device in another subnet is untouched by this subnet's scan.
	Persist(ctx, d, "192.168.9.0/24", nil, false)
	d.QueryRow(`SELECT is_up FROM devices WHERE mac = '11:22:33:44:55:66'`).Scan(&up)
	if up != 1 {
		t.Fatal("scanning another subnet must not mark this device down")
	}
}

package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestServiceHierarchy(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &Server{DB: d}
	ctx := context.Background()
	// web-vm (host) ← nginx (docker on web-vm) ← site (no hosting) ← subsite
	d.Exec(`INSERT INTO hosts(id, name, kind) VALUES (1, 'web-vm', 'vm')`)
	d.Exec(`INSERT INTO services(id, name) VALUES (1, 'nginx'), (2, 'site'), (3, 'subsite'), (4, 'standalone')`)
	d.Exec(`UPDATE services SET parent_id = 1 WHERE id = 2`)
	d.Exec(`UPDATE services SET parent_id = 2 WHERE id = 3`)
	d.Exec(`INSERT INTO deployments(service_id, host_id, runtime, container_name) VALUES (1, 1, 'docker', 'nginx')`)

	h, err := s.effectiveHostings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h[1] == nil || h[1].Inherited {
		t.Fatalf("nginx should have its own hosting: %+v", h[1])
	}
	for _, id := range []int64{2, 3} {
		if h[id] == nil || !h[id].Inherited || h[id].ViaService != "nginx" || h[id].HostName != "web-vm" {
			t.Fatalf("service %d should inherit web-vm via nginx: %+v", id, h[id])
		}
	}
	if h[4] != nil {
		t.Fatal("unrelated service should have no hosting")
	}

	if err := s.checkParent(ctx, 1, 1); err == nil {
		t.Error("self-parent must be rejected")
	}
	if err := s.checkParent(ctx, 1, 3); err == nil {
		t.Error("parenting nginx under its grandchild must be rejected")
	}
	if err := s.checkParent(ctx, 4, 3); err != nil {
		t.Errorf("valid parent rejected: %v", err)
	}
	if err := s.checkParent(ctx, 4, 99); err == nil {
		t.Error("missing parent must be rejected")
	}
}

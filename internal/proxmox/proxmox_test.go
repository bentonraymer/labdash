package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "PVEAPIToken=labdash@pve!dash=abc-123" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"data":[
			{"id":"node/pve1","type":"node","node":"pve1","status":"online","cpu":0.12,"maxcpu":16,"mem":8e9,"maxmem":64e9,"uptime":1000},
			{"id":"lxc/120","type":"lxc","node":"pve3","vmid":120,"name":"grafana","status":"running","cpu":0.01,"maxcpu":2,"mem":3e8,"maxmem":2e9},
			{"id":"qemu/105","type":"qemu","node":"pve2","vmid":105,"name":"docker-media","status":"running"},
			{"id":"storage/pve1/local","type":"storage","node":"pve1"}
		]}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	// Self-signed cert: works because VerifyTLS defaults to false.
	res, err := Fetch(ctx, Config{URL: srv.URL + "/"}, Secret{TokenID: "labdash@pve!dash", TokenSecret: "abc-123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[1].VMID != 120 || res[1].Node != "pve3" || res[0].MaxCPU != 16 {
		t.Fatalf("unexpected resources: %+v", res)
	}
	if _, err := Fetch(ctx, Config{URL: srv.URL}, Secret{TokenID: "x", TokenSecret: "y"}); err == nil {
		t.Fatal("bad token should fail")
	}
	if _, err := Fetch(ctx, Config{URL: srv.URL, VerifyTLS: true}, Secret{TokenID: "labdash@pve!dash", TokenSecret: "abc-123"}); err == nil {
		t.Fatal("verifying a self-signed cert should fail")
	}
}

package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
	"github.com/bentonraymer/labdash/internal/proxmox"
)

func TestParseImage(t *testing.T) {
	cases := map[string]string{
		"nginx":                             "docker.io/library/nginx:latest",
		"nginx:1.27-alpine":                 "docker.io/library/nginx:1.27-alpine",
		"louislam/uptime-kuma:1":            "docker.io/louislam/uptime-kuma:1",
		"ghcr.io/gethomepage/homepage:v1.2": "ghcr.io/gethomepage/homepage:v1.2",
		"lscr.io/linuxserver/sonarr:latest": "lscr.io/linuxserver/sonarr:latest",
		"localhost:5000/app":                "localhost:5000/app:latest",
		"redis:7@sha256:abc":                "docker.io/library/redis:7",
	}
	for in, want := range cases {
		r, err := ParseImage(in)
		if err != nil || r.String() != want {
			t.Errorf("%s: got %s %v want %s", in, r.String(), err, want)
		}
	}
}

func TestNewestTag(t *testing.T) {
	tags := []string{"1.25.0-alpine", "1.27.3-alpine", "1.27.10-alpine", "1.28.0-alpine-rc1", "1.27.3", "1.30.0", "latest", "mainline-alpine"}
	if got := NewestTag("1.27.3-alpine", tags); got != "1.27.10-alpine" {
		t.Errorf("alpine flavour: %q", got)
	}
	if got := NewestTag("1.27.3", tags); got != "1.30.0" {
		t.Errorf("plain: %q", got)
	}
	if got := NewestTag("latest", tags); got != "" {
		t.Errorf("latest isn't a version: %q", got)
	}
	if got := NewestTag("1.30.0", tags); got != "" {
		t.Errorf("already newest: %q", got)
	}
}

func TestRepoFromScript(t *testing.T) {
	script := `#!/usr/bin/env bash
# Source: https://gethomepage.dev/ | Github: https://github.com/gethomepage/homepage
  fetch_and_deploy_gh_release "intel" "intel/compute-runtime"
  if check_for_gh_release "homepage" "gethomepage/homepage"; then`
	if got := RepoFromScript(script); got != "gethomepage/homepage" {
		t.Errorf("got %q", got)
	}
	if got := RepoFromScript("# Source: https://jellyfin.org/\napt install jellyfin"); got != "" {
		t.Errorf("apt-based script: %q", got)
	}
}

func TestCompareLoose(t *testing.T) {
	if compareLoose("v2.10.0", "2.9.1") <= 0 || compareLoose("homepage-v1.0.0", "1.0.0") != 0 || compareLoose("1.0", "v1.0.1") >= 0 {
		t.Fatal("loose compare is wrong")
	}
}

func TestDigestMessage(t *testing.T) {
	title, body := DigestMessage([]Outdated{{"Homepage", "v1.0.0", "v1.2.0"}, {"Jellyfin", "", "10.11.2"}},
		[]proxmox.NodeUpdates{{Node: "pve1", Packages: make([]proxmox.AptPackage, 3)}, {Node: "pve2"}})
	if title != "⬆️ Weekly updates: 2 service updates + Proxmox packages" || !strings.Contains(body, "<b>Homepage</b> v1.0.0 → v1.2.0") ||
		!strings.Contains(body, "Jellyfin</b> ? → 10.11.2") || !strings.Contains(body, "3 package updates on pve1 (3)") {
		t.Fatalf("%q\n%s", title, body)
	}
	if title, _ := DigestMessage(nil, nil); title != "" {
		t.Fatal("nothing outdated should send nothing")
	}
}

// End to end against a fake socket proxy, registry (with token auth) and GitHub.
func TestCheckAll(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	var regURL string
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			json.NewEncoder(w).Encode(map[string]string{"token": "t0k"})
		case r.Header.Get("Authorization") != "Bearer t0k":
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="reg",scope="repository:x:pull"`, regURL))
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/manifests/1.27.3-alpine"):
			w.Header().Set("Docker-Content-Digest", "sha256:same")
		case strings.HasSuffix(r.URL.Path, "/manifests/latest"):
			w.Header().Set("Docker-Content-Digest", "sha256:newer")
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.27.3-alpine", "1.27.4-alpine", "latest"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer reg.Close()
	regURL = reg.URL
	host := strings.TrimPrefix(reg.URL, "http://")

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/json":
			json.NewEncoder(w).Encode([]Container{
				{Names: []string{"/web"}, Image: host + "/library/nginx:1.27.3-alpine", ImageID: "sha256:img1"},
				{Names: []string{"/kuma"}, Image: host + "/louislam/uptime-kuma:latest", ImageID: "sha256:img2"},
			})
		case r.URL.Path == "/images/sha256:img1/json":
			json.NewEncoder(w).Encode(map[string]any{"RepoDigests": []string{host + "/library/nginx@sha256:same"}})
		case r.URL.Path == "/images/sha256:img2/json":
			json.NewEncoder(w).Encode(map[string]any{"RepoDigests": []string{host + "/louislam/uptime-kuma@sha256:older"},
				"Config": map[string]any{"Labels": map[string]string{"org.opencontainers.image.version": "1.23.16"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxy.Close()

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/gethomepage/homepage/releases/latest" {
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.4.0", "html_url": "https://github.com/x", "body": "notes"})
			return
		}
		http.NotFound(w, r)
	}))
	defer gh.Close()

	for _, q := range []string{
		`INSERT INTO hosts(id, name, kind) VALUES (1, 'docker-vm', 'vm')`,
		`INSERT INTO docker_endpoints(id, name, url, host_id) VALUES (1, 'docker-vm', '` + proxy.URL + `', 1)`,
		`INSERT INTO services(id, name) VALUES (1, 'Web'), (2, 'Uptime Kuma'), (3, 'Homepage'), (4, 'Notes')`,
		`INSERT INTO deployments(service_id, host_id, runtime, container_name) VALUES (1, 1, 'docker', 'web'), (2, 1, 'docker', 'kuma')`,
		`INSERT INTO update_checks(service_id, source, helper_slug, current_version) VALUES (3, 'helper', 'homepage', '1.2.0')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	c := NewChecker(d, nil)
	c.Registry.Scheme = "http"
	c.GitHub.Base = gh.URL
	c.Helper.cache["homepage"] = cachedRepo{repo: "gethomepage/homepage", at: timeNow()}
	if err := c.CheckAll(ctx); err != nil {
		t.Fatal(err)
	}
	row := func(id int) (status, running, latest, src string) {
		d.QueryRow(`SELECT status, running_version, latest_version, used_source FROM update_checks WHERE service_id = ?`, id).Scan(&status, &running, &latest, &src)
		return
	}
	if s, run, lat, src := row(1); s != "outdated" || run != "1.27.3-alpine" || lat != "1.27.4-alpine" || src != "docker" {
		t.Errorf("web: %s %s %s %s", s, run, lat, src)
	}
	if s, run, lat, _ := row(2); s != "outdated" || run != "1.23.16" || lat != "latest (newer image)" {
		t.Errorf("kuma: %s %s %s", s, run, lat)
	}
	if s, run, lat, src := row(3); s != "outdated" || run != "1.2.0" || lat != "v1.4.0" || src != "helper" {
		t.Errorf("homepage: %s %s %s %s", s, run, lat, src)
	}
	if s, _, _, _ := row(4); s != "untracked" {
		t.Errorf("notes: %s", s)
	}
	// Skipping the version makes it current.
	d.Exec(`UPDATE update_checks SET ignored_version = '1.27.4-alpine' WHERE service_id = 1`)
	c.CheckOne(ctx, 1)
	if s, _, _, _ := row(1); s != "current" {
		t.Errorf("skipped version still outdated: %s", s)
	}
}

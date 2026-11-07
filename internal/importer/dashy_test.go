package importer

import "testing"

const sample = `
pageInfo:
  title: Home Lab
sections:
  - name: Devices
    icon: fas fa-server
    items:
      - title: pve1
        icon: hl-proxmox
        url: https://192.168.8.11:8006/
      - title: Router
        icon: favicon
        url: http://192.168.8.1
        target: sametab
  - name: Media
    icon: "🎬"
    displayData:
      collapsed: true
    items:
      - title: Jellyfin
        description: Movies
        icon: https://example.com/jf.png
        url: https://jellyfin.home.example.com
      - title: Arr stack
        subItems:
          - title: Radarr
            icon: sh-radarr.svg
            url: http://192.168.8.40:7878/movies?x=1
      - title: ""
        url: http://ignored
`

func TestParseDashy(t *testing.T) {
	secs, err := ParseDashy([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 {
		t.Fatalf("sections = %d", len(secs))
	}
	dev, media := secs[0], secs[1]
	if dev.Icon != "" || media.Icon != "emoji:🎬" || !media.Collapsed {
		t.Errorf("section icons/collapsed wrong: %+v %+v", dev, media)
	}

	pve := dev.Services[0]
	if pve.Icon != "di:proxmox" || pve.DirectScheme != "https" || pve.DirectIP != "192.168.8.11" || *pve.DirectPort != 8006 || pve.DirectPath != "" || pve.DNSURL != "" {
		t.Errorf("pve1 wrong: %+v", pve)
	}
	router := dev.Services[1]
	if router.NewTab || router.DirectPort != nil || router.Icon != "" {
		t.Errorf("router wrong: %+v", router)
	}

	if len(media.Services) != 2 {
		t.Fatalf("media services = %d (subItems should flatten, blank titles skipped)", len(media.Services))
	}
	jf := media.Services[0]
	if jf.DNSURL != "https://jellyfin.home.example.com" || jf.Icon != "https://example.com/jf.png" || jf.Description != "Movies" {
		t.Errorf("jellyfin wrong: %+v", jf)
	}
	radarr := media.Services[1]
	if radarr.Icon != "di:radarr" || radarr.DirectPath != "/movies?x=1" || *radarr.DirectPort != 7878 {
		t.Errorf("radarr wrong: %+v", radarr)
	}
}

func TestParseDashyRejectsGarbage(t *testing.T) {
	if _, err := ParseDashy([]byte("hello: world")); err == nil {
		t.Fatal("expected error for config without sections")
	}
}

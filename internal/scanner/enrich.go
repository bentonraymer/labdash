package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ClassifyType guesses a device type from vendor, OS class and open ports.
func ClassifyType(h Host) string {
	v := strings.ToLower(h.Vendor)
	osName := strings.ToLower(h.OSName + " " + h.OSFamily)
	ports := map[int]bool{}
	for _, p := range h.Ports {
		ports[p.Port] = true
	}
	switch {
	case strings.HasPrefix(h.MAC, "bc:24:11") || strings.Contains(v, "proxmox"):
		return "Proxmox VM"
	case ports[8006]:
		return "Hypervisor"
	case h.OSType == "router" || h.OSType == "WAP" || h.OSType == "broadband router" || strings.Contains(osName, "openwrt") || strings.Contains(v, "gl technologies") || strings.Contains(v, "ubiquiti"):
		return "Network"
	case h.OSType == "printer" || ports[631] || ports[9100]:
		return "Printer"
	case h.OSType == "phone" || strings.Contains(osName, "ios") || strings.Contains(osName, "android"):
		return "Mobile"
	case h.OSType == "media device" || strings.Contains(v, "roku") || strings.Contains(v, "sonos") || strings.Contains(v, "hisense"):
		return "Media"
	case strings.Contains(v, "espressif") || strings.Contains(v, "tuya") || strings.Contains(v, "shelly") || strings.Contains(v, "itead") || strings.Contains(v, "amazon") || strings.Contains(v, "google"):
		return "IoT"
	case strings.Contains(v, "synology") || strings.Contains(v, "qnap") || strings.Contains(osName, "truenas") || ports[445] && ports[2049]:
		return "NAS"
	case strings.Contains(osName, "windows"):
		if ports[3389] || ports[445] {
			return "Workstation"
		}
		return "Windows"
	case strings.Contains(v, "apple") || strings.Contains(osName, "mac os"):
		return "Apple"
	case len(h.Ports) > 0 && (strings.Contains(osName, "linux") || h.OSFamily == ""):
		return "Server"
	case len(h.Ports) == 0:
		return "Client"
	}
	return "Server"
}

// IsWebPort reports whether a port likely serves HTTP(S), and which scheme.
func IsWebPort(p Port) (scheme string, ok bool) {
	name := strings.ToLower(p.Service)
	switch {
	case p.TLS || strings.Contains(name, "https") || p.Port == 443 || p.Port == 8443 || p.Port == 8006 || p.Port == 9443:
		if strings.Contains(name, "http") || name == "" || p.Port == 443 || p.Port == 8443 || p.Port == 8006 || p.Port == 9443 {
			return "https", true
		}
	case strings.Contains(name, "http") || p.Port == 80 || p.Port == 8080 || p.Port == 8000 || p.Port == 3000:
		return "http", true
	}
	return "", false
}

// Prober fetches a page's <title> and favicon; implemented by icons.Cache.
type Prober interface {
	Probe(ctx context.Context, url string) (title, favicon string, err error)
}

// EnrichWeb fills Title/Favicon for web ports, a few at a time.
func EnrichWeb(ctx context.Context, hosts []Host, p Prober) {
	if p == nil {
		return
	}
	type job struct{ h, i int }
	var jobs []job
	for hi := range hosts {
		for pi := range hosts[hi].Ports {
			if _, ok := IsWebPort(hosts[hi].Ports[pi]); ok {
				jobs = append(jobs, job{hi, pi})
			}
		}
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			port := &hosts[j.h].Ports[j.i]
			scheme, _ := IsWebPort(*port)
			url := fmt.Sprintf("%s://%s:%d/", scheme, hosts[j.h].IP, port.Port)
			title, fav, err := p.Probe(ctx, url)
			if err == nil {
				port.Title, port.Favicon = title, fav
			}
		}(j)
	}
	wg.Wait()
}

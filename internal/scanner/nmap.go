// Package scanner runs nmap against configured subnets, enriches the results
// (vendor, OS, web titles/favicons) and keeps the device inventory current.
package scanner

import (
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ── nmap XML ──────────────────────────────────────────────────────────────

type nmapRun struct {
	Hosts []nmapHost `xml:"host"`
}

type nmapHost struct {
	Status struct {
		State string `xml:"state,attr"`
	} `xml:"status"`
	Addresses []struct {
		Addr     string `xml:"addr,attr"`
		AddrType string `xml:"addrtype,attr"`
		Vendor   string `xml:"vendor,attr"`
	} `xml:"address"`
	Hostnames []struct {
		Name string `xml:"name,attr"`
		Type string `xml:"type,attr"`
	} `xml:"hostnames>hostname"`
	Ports []struct {
		Protocol string `xml:"protocol,attr"`
		PortID   int    `xml:"portid,attr"`
		State    struct {
			State string `xml:"state,attr"`
		} `xml:"state"`
		Service struct {
			Name      string   `xml:"name,attr"`
			Product   string   `xml:"product,attr"`
			Version   string   `xml:"version,attr"`
			ExtraInfo string   `xml:"extrainfo,attr"`
			OSType    string   `xml:"ostype,attr"`
			Tunnel    string   `xml:"tunnel,attr"`
			CPEs      []string `xml:"cpe"`
		} `xml:"service"`
	} `xml:"ports>port"`
	OSMatches []struct {
		Name     string `xml:"name,attr"`
		Accuracy int    `xml:"accuracy,attr"`
		Classes  []struct {
			Type     string `xml:"type,attr"`
			Vendor   string `xml:"vendor,attr"`
			OSFamily string `xml:"osfamily,attr"`
		} `xml:"osclass"`
	} `xml:"os>osmatch"`
}

// Host is one scanned host after parsing.
type Host struct {
	IP       string
	MAC      string
	Vendor   string
	Hostname string
	OSName   string
	OSFamily string
	OSType   string // nmap osclass type: general purpose, router, phone…
	Ports    []Port
}

type Port struct {
	Port    int
	Proto   string
	State   string
	Service string
	Product string
	Version string
	TLS     bool
	CPE     string // space-separated CPEs from -sV
	// Filled by HTTP enrichment
	Title   string
	Favicon string
}

// ParseXML converts nmap -oX output into up hosts.
func ParseXML(r io.Reader) ([]Host, error) {
	var run nmapRun
	if err := xml.NewDecoder(r).Decode(&run); err != nil {
		return nil, fmt.Errorf("parse nmap xml: %w", err)
	}
	var out []Host
	for _, h := range run.Hosts {
		if h.Status.State != "up" {
			continue
		}
		var host Host
		for _, a := range h.Addresses {
			switch a.AddrType {
			case "ipv4":
				host.IP = a.Addr
			case "mac":
				host.MAC = strings.ToLower(a.Addr)
				host.Vendor = a.Vendor
			}
		}
		if host.IP == "" {
			continue
		}
		for _, hn := range h.Hostnames {
			if hn.Type == "PTR" || host.Hostname == "" {
				host.Hostname = strings.TrimSuffix(hn.Name, ".")
			}
		}
		if len(h.OSMatches) > 0 {
			best := h.OSMatches[0]
			for _, m := range h.OSMatches[1:] {
				if m.Accuracy > best.Accuracy {
					best = m
				}
			}
			host.OSName = best.Name
			if len(best.Classes) > 0 {
				c := best.Classes[0]
				host.OSFamily = c.OSFamily
				host.OSType = c.Type
			}
		}
		for _, p := range h.Ports {
			if p.State.State != "open" {
				continue
			}
			sv := p.Service
			host.Ports = append(host.Ports, Port{
				Port: p.PortID, Proto: p.Protocol, State: "open",
				Service: sv.Name, Product: sv.Product, Version: strings.TrimSpace(sv.Version + " " + sv.ExtraInfo),
				TLS: sv.Tunnel == "ssl" || strings.Contains(sv.Name, "https"),
				CPE: strings.Join(sv.CPEs, " "),
			})
			// Fall back to the OS a service banner reports ("Linux", "Windows").
			if host.OSFamily == "" && sv.OSType != "" {
				host.OSFamily = sv.OSType
			}
		}
		out = append(out, host)
	}
	return out, nil
}

// ── running nmap ──────────────────────────────────────────────────────────

// Profile picks which ports to scan.
type Profile struct {
	Name   string // top100 | top1000 | full | custom
	Custom string // e.g. "22,80,443,8000-9000"
}

var customPortsRe = regexp.MustCompile(`^[0-9,\- ]+$`)

// Validate reports whether the profile (and any custom port list) is usable.
func (p Profile) Validate() error {
	_, err := p.args()
	return err
}

func (p Profile) args() ([]string, error) {
	switch p.Name {
	case "top100":
		return []string{"--top-ports", "100"}, nil
	case "", "top1000":
		return []string{"--top-ports", "1000"}, nil
	case "full":
		return []string{"-p-"}, nil
	case "custom":
		ports := strings.ReplaceAll(strings.TrimSpace(p.Custom), " ", "")
		if ports == "" || !customPortsRe.MatchString(ports) {
			return nil, errors.New("custom port list must look like 22,80,443,8000-9000")
		}
		return []string{"-p", ports}, nil
	}
	return nil, fmt.Errorf("unknown port profile %q", p.Name)
}

// Progress is reported while nmap runs.
type Progress struct {
	Phase   string  `json:"phase"`
	Percent float64 `json:"percent"`
}

var (
	progressRe = regexp.MustCompile(`^(.+?) Timing: About ([\d.]+)% done`)
	phaseRe    = regexp.MustCompile(`^Initiating (.+?) at \d`)
)

// RunNmap scans target (a CIDR or space-separated IPs) and returns up hosts.
// It needs root or CAP_NET_RAW for MAC addresses and OS detection.
func RunNmap(ctx context.Context, target string, prof Profile, onProgress func(Progress)) ([]Host, error) {
	portArgs, err := prof.args()
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("nmap"); err != nil {
		return nil, errors.New("nmap is not installed (it ships in the LabDash Docker image)")
	}
	tmp, err := os.CreateTemp("", "labdash-nmap-*.xml")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	args := []string{
		"-oX", tmp.Name(), "--stats-every", "3s", "-v",
		"-T4", "--max-retries", "2", "--host-timeout", "15m",
		"-sV", "--version-light",
		"-O", "--osscan-limit",
	}
	args = append(args, portArgs...)
	args = append(args, strings.Fields(target)...)

	cmd := exec.CommandContext(ctx, "nmap", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if onProgress == nil {
			continue
		}
		line := strings.TrimSpace(sc.Text())
		if m := progressRe.FindStringSubmatch(line); m != nil {
			pct, _ := strconv.ParseFloat(m[2], 64)
			onProgress(Progress{Phase: m[1], Percent: pct})
		} else if m := phaseRe.FindStringSubmatch(line); m != nil {
			onProgress(Progress{Phase: m[1], Percent: -1}) // -1: phase changed, keep percent
		}
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("nmap failed: %s", firstLine(msg))
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseXML(f)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

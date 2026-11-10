package vuln

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding is one problem on one device port.
type Finding struct {
	IP          string   `json:"ip"`
	Port        int      `json:"port"`
	Source      string   `json:"source"` // cve | nuclei | config
	Key         string   `json:"key"`    // CVE id / template id / check id
	Title       string   `json:"title"`
	Severity    string   `json:"severity"` // critical | high | medium | low | info
	CVSS        float64  `json:"cvss"`
	KEV         bool     `json:"kev"`
	Description string   `json:"description"`
	Evidence    string   `json:"evidence"`
	References  []string `json:"references"`
	FixedIn     string   `json:"fixedIn"`
	Confidence  string   `json:"confidence"` // high | low
}

// SeverityRank orders severities (critical highest).
func SeverityRank(s string) int {
	return map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}[strings.ToLower(s)]
}

// ── Nuclei ────────────────────────────────────────────────────────────────

// Excluded so scans stay safe: no exploitation, DoS, fuzzing, brute force
// or default-credential attempts (those can lock accounts / trip fail2ban).
const NucleiExcludeTags = "dos,fuzz,fuzzing,intrusive,bruteforce,brute-force,default-login,default-logins,backdoor"

// NucleiTags selects the templates that find real problems.
const NucleiTags = "cve,kev,vuln,misconfig,exposure,config"

// Nuclei runs projectdiscovery/nuclei against web URLs.
type Nuclei struct {
	Binary       string
	TemplatesDir string
}

func (n Nuclei) Available() bool {
	if n.Binary == "" {
		return false
	}
	_, err := exec.LookPath(n.Binary)
	return err == nil
}

// HasTemplates reports whether the templates directory holds any templates.
func (n Nuclei) HasTemplates() bool {
	entries, err := os.ReadDir(n.TemplatesDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

// Version reports the installed nuclei version (best effort).
func (n Nuclei) Version(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, n.Binary, "-version", "-duc").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "Nuclei Engine Version:"); i >= 0 {
			return strings.TrimSpace(line[i+len("Nuclei Engine Version:"):])
		}
	}
	return ""
}

// UpdateTemplates downloads/refreshes the community templates.
func (n Nuclei) UpdateTemplates(ctx context.Context) error {
	if err := os.MkdirAll(n.TemplatesDir, 0o750); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	// Note: no -duc here; "disable update check" also blocks template downloads.
	out, err := exec.CommandContext(ctx, n.Binary, "-update-templates", "-ud", n.TemplatesDir, "-silent").CombinedOutput()
	if err != nil {
		return fmt.Errorf("nuclei template update: %s", lastLine(string(out), err))
	}
	if !n.HasTemplates() {
		return fmt.Errorf("nuclei template update finished but no templates were downloaded: %s", lastLine(string(out), nil))
	}
	return nil
}

// Run scans the URLs with safe templates (severity low and up).
func (n Nuclei) Run(ctx context.Context, urls []string) ([]Finding, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	if !n.Available() {
		return nil, errors.New("nuclei is not installed")
	}
	list, err := os.CreateTemp("", "labdash-nuclei-*.txt")
	if err != nil {
		return nil, err
	}
	defer os.Remove(list.Name())
	list.WriteString(strings.Join(urls, "\n"))
	list.Close()
	args := []string{
		"-l", list.Name(), "-jsonl", "-silent", "-nc", "-duc",
		"-ud", n.TemplatesDir,
		// Vulnerability-focused templates only (about 6k of ~14k): known
		// CVEs, misconfigurations and exposed files/configs. Skips the large
		// tech-detection / panel / OSINT sets that only report info.
		"-tags", NucleiTags,
		"-exclude-tags", NucleiExcludeTags,
		"-severity", "low,medium,high,critical",
		"-ni", // no interactsh: never call out to external OAST servers
		// Global limit across all hosts; modest for a LAN.
		"-rate-limit", "150", "-c", "25", "-timeout", "8", "-retries", "1",
	}
	cmd := exec.CommandContext(ctx, n.Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	findings, perr := ParseNucleiJSONL(stdout)
	if err := cmd.Wait(); err != nil && ctx.Err() == nil && len(findings) == 0 {
		return nil, fmt.Errorf("nuclei: %s", lastLine(stderr.String(), err))
	}
	return findings, perr
}

// ParseNucleiJSONL converts nuclei -jsonl output to findings.
func ParseNucleiJSONL(r io.Reader) ([]Finding, error) {
	var out []Finding
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			TemplateID string `json:"template-id"`
			Info       struct {
				Name           string   `json:"name"`
				Severity       string   `json:"severity"`
				Description    string   `json:"description"`
				Reference      []string `json:"reference"`
				Remediation    string   `json:"remediation"`
				Classification struct {
					CVEID     []string `json:"cve-id"`
					CVSSScore float64  `json:"cvss-score"`
				} `json:"classification"`
			} `json:"info"`
			MatchedAt   string   `json:"matched-at"`
			IP          string   `json:"ip"`
			Port        string   `json:"port"`
			MatcherName string   `json:"matcher-name"`
			Extracted   []string `json:"extracted-results"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.TemplateID == "" {
			continue
		}
		port, _ := strconv.Atoi(ev.Port)
		key := ev.TemplateID
		if ev.MatcherName != "" {
			key += ":" + ev.MatcherName
		}
		evidence := ev.MatchedAt
		if len(ev.Extracted) > 0 {
			evidence += " → " + strings.Join(ev.Extracted, ", ")
		}
		desc := strings.TrimSpace(ev.Info.Description)
		if ev.Info.Remediation != "" {
			desc += "\n\nFix: " + strings.TrimSpace(ev.Info.Remediation)
		}
		title := ev.Info.Name
		if len(ev.Info.Classification.CVEID) > 0 {
			title += " (" + strings.ToUpper(strings.Join(ev.Info.Classification.CVEID, ", ")) + ")"
		}
		out = append(out, Finding{
			IP: ev.IP, Port: port, Source: "nuclei", Key: key, Title: title,
			Severity: strings.ToLower(ev.Info.Severity), CVSS: ev.Info.Classification.CVSSScore,
			Description: desc, Evidence: evidence, References: ev.Info.Reference, Confidence: "high",
		})
	}
	return out, sc.Err()
}

// ── nmap configuration checks ─────────────────────────────────────────────

// Safe NSE scripts only (all in nmap's "safe" category).
var ConfigScripts = []string{"ssl-enum-ciphers", "ssl-cert", "sslv2", "ftp-anon", "redis-info", "mongodb-info", "smb2-security-mode", "vnc-info"}

type xRun struct {
	Hosts []struct {
		Addresses []struct {
			Addr string `xml:"addr,attr"`
			Type string `xml:"addrtype,attr"`
		} `xml:"address"`
		Ports []struct {
			PortID int `xml:"portid,attr"`
			State  struct {
				State string `xml:"state,attr"`
			} `xml:"state"`
			Service struct {
				Name      string   `xml:"name,attr"`
				Product   string   `xml:"product,attr"`
				Version   string   `xml:"version,attr"`
				ExtraInfo string   `xml:"extrainfo,attr"`
				CPE       []string `xml:"cpe"`
			} `xml:"service"`
			Scripts []xScript `xml:"script"`
		} `xml:"ports>port"`
		HostScripts []xScript `xml:"hostscript>script"`
	} `xml:"host"`
}

type xScript struct {
	ID     string  `xml:"id,attr"`
	Output string  `xml:"output,attr"`
	Elems  []xElem `xml:"elem"`
	Tables []xTab  `xml:"table"`
}

type xElem struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

type xTab struct {
	Key    string  `xml:"key,attr"`
	Elems  []xElem `xml:"elem"`
	Tables []xTab  `xml:"table"`
}

func (s xScript) elem(key string) string {
	for _, e := range s.Elems {
		if e.Key == key {
			return strings.TrimSpace(e.Value)
		}
	}
	var walk func(ts []xTab) string
	walk = func(ts []xTab) string {
		for _, t := range ts {
			for _, e := range t.Elems {
				if e.Key == key {
					return strings.TrimSpace(e.Value)
				}
			}
			if v := walk(t.Tables); v != "" {
				return v
			}
		}
		return ""
	}
	return walk(s.Tables)
}

func (s xScript) tableKeys() []string {
	var out []string
	for _, t := range s.Tables {
		out = append(out, t.Key)
	}
	return out
}

// ServiceInfo is what -sV identified on a port (used for CVE matching).
type ServiceInfo struct {
	IP      string
	Port    int
	Name    string
	Product string
	Version string // version + extrainfo, for display / distro hints
	CPEs    []string
}

// ParseConfigXML extracts config findings and service versions from nmap XML.
func ParseConfigXML(r io.Reader, now time.Time) ([]Finding, []ServiceInfo, error) {
	var run xRun
	if err := xml.NewDecoder(r).Decode(&run); err != nil {
		return nil, nil, fmt.Errorf("parse nmap xml: %w", err)
	}
	var findings []Finding
	var services []ServiceInfo
	for _, h := range run.Hosts {
		ip := ""
		for _, a := range h.Addresses {
			if a.Type == "ipv4" {
				ip = a.Addr
			}
		}
		for _, p := range h.Ports {
			if p.State.State != "open" {
				continue
			}
			sv := p.Service
			services = append(services, ServiceInfo{IP: ip, Port: p.PortID, Name: sv.Name, Product: sv.Product,
				Version: strings.TrimSpace(sv.Version + " " + sv.ExtraInfo), CPEs: sv.CPE})
			for _, sc := range p.Scripts {
				for _, f := range scriptFindings(sc, now) {
					f.IP, f.Port, f.Source, f.Confidence = ip, p.PortID, "config", "high"
					findings = append(findings, f)
				}
			}
		}
		for _, sc := range h.HostScripts {
			for _, f := range scriptFindings(sc, now) {
				f.IP, f.Port, f.Source, f.Confidence = ip, 445, "config", "high"
				findings = append(findings, f)
			}
		}
	}
	return findings, services, nil
}

func scriptFindings(sc xScript, now time.Time) []Finding {
	out := sc.Output
	low := strings.ToLower(out)
	var fs []Finding
	add := func(key, title, sev, desc string) {
		fs = append(fs, Finding{Key: key, Title: title, Severity: sev, Description: desc, Evidence: trimEvidence(out)})
	}
	switch sc.ID {
	case "ssl-enum-ciphers":
		for _, proto := range sc.tableKeys() {
			switch proto {
			case "SSLv3":
				add("tls-sslv3", "SSLv3 enabled", "high", "SSLv3 is broken (POODLE). Disable it and allow only TLS 1.2+.")
			case "TLSv1.0", "TLSv1.1":
				add("tls-old-"+strings.ToLower(strings.ReplaceAll(proto, ".", "")), proto+" enabled", "medium", proto+" is deprecated. Allow only TLS 1.2 and 1.3.")
			}
		}
		if grade := sc.elem("least strength"); grade != "" && grade >= "C" {
			sev := "medium"
			if grade >= "E" {
				sev = "high"
			}
			add("tls-weak-ciphers", "Weak TLS ciphers (grade "+grade+")", sev, "The server accepts weak cipher suites. Restrict it to modern AEAD ciphers.")
		}
	case "ssl-cert":
		if na := sc.elem("notAfter"); na != "" {
			if t, err := parseCertTime(na); err == nil {
				switch d := t.Sub(now); {
				case d < 0:
					add("tls-cert-expired", "TLS certificate expired", "high", fmt.Sprintf("The certificate expired on %s.", t.Format("2006-01-02")))
				case d < 21*24*time.Hour:
					add("tls-cert-expiring", "TLS certificate expires soon", "low", fmt.Sprintf("The certificate expires on %s (%d days).", t.Format("2006-01-02"), int(d.Hours()/24)))
				}
			}
		}
		if bits := sc.elem("bits"); bits != "" {
			if b, _ := strconv.Atoi(bits); b > 0 && b < 2048 && strings.Contains(strings.ToLower(sc.elem("type")), "rsa") {
				add("tls-weak-key", fmt.Sprintf("Weak RSA key (%d bits)", b), "medium", "Use at least a 2048-bit RSA or an ECDSA key.")
			}
		}
	case "sslv2":
		if strings.Contains(low, "sslv2 supported") {
			add("tls-sslv2", "SSLv2 enabled", "high", "SSLv2 is completely broken. Disable it.")
		}
	case "ftp-anon":
		if strings.Contains(low, "anonymous ftp login allowed") {
			add("ftp-anonymous", "Anonymous FTP login allowed", "high", "Anyone can log in to this FTP server. Disable anonymous access unless it's intended.")
		}
	case "redis-info":
		if out != "" && !strings.Contains(low, "authentication required") && !strings.Contains(low, "noauth") && !strings.Contains(low, "error") {
			add("redis-no-auth", "Redis without authentication", "critical", "Anyone on the network can read/write this Redis instance (and often get code execution). Set requirepass/ACLs and bind it to trusted interfaces.")
		}
	case "mongodb-info":
		if strings.Contains(low, "builddata") || strings.Contains(low, "buildinfo") || strings.Contains(low, "databases") {
			if !strings.Contains(low, "unauthorized") && !strings.Contains(low, "requires authentication") {
				add("mongodb-no-auth", "MongoDB without authentication", "critical", "MongoDB answers queries without credentials. Enable authorization.")
			}
		}
	case "smb2-security-mode":
		if strings.Contains(low, "not required") {
			add("smb-signing-not-required", "SMB signing not required", "medium", "SMB message signing is enabled but not required, allowing relay attacks. Require signing.")
		}
	case "vnc-info":
		if strings.Contains(low, "none (1)") || strings.Contains(low, "security types:\n|     none") || strings.Contains(low, "security types:\n      none") {
			add("vnc-no-auth", "VNC without a password", "critical", "This VNC server allows connections without authentication.")
		}
	}
	return fs
}

func parseCertTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}

func trimEvidence(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}

func lastLine(s string, err error) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	if err == nil {
		return "no output"
	}
	return err.Error()
}

// RunConfigScan runs safe NSE scripts plus -sV on the given ports.
func RunConfigScan(ctx context.Context, targets map[string][]int, now time.Time) ([]Finding, []ServiceInfo, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	if _, err := exec.LookPath("nmap"); err != nil {
		return nil, nil, errors.New("nmap is not installed")
	}
	portSet := map[int]bool{}
	var ips []string
	for ip, ports := range targets {
		ips = append(ips, ip)
		for _, p := range ports {
			portSet[p] = true
		}
	}
	sort.Strings(ips)
	var ports []string
	for p := range portSet {
		ports = append(ports, strconv.Itoa(p))
	}
	sort.Strings(ports)
	tmp, err := os.CreateTemp("", "labdash-vuln-*.xml")
	if err != nil {
		return nil, nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	args := append([]string{"-Pn", "-sV", "--version-light", "-T4", "--host-timeout", "20m",
		"--script", strings.Join(ConfigScripts, ","), "-p", "T:" + strings.Join(ports, ","), "-oX", tmp.Name()}, ips...)
	cmd := exec.CommandContext(ctx, "nmap", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("nmap: %s", lastLine(string(out), err))
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return ParseConfigXML(f, now)
}

package vuln

import (
	"strings"
	"testing"
	"time"
)

func TestParseNucleiJSONL(t *testing.T) {
	in := `[INF] banner line that isn't JSON
{"template-id":"exposed-panel-grafana","info":{"name":"Grafana Login Panel","severity":"info"},"matched-at":"http://192.168.8.3:3000/login","ip":"192.168.8.3","port":"3000"}
{"template-id":"CVE-2021-43798","info":{"name":"Grafana Path Traversal","severity":"high","description":"Directory traversal","reference":["https://grafana.com/blog"],"remediation":"Upgrade to 8.3.1","classification":{"cve-id":["cve-2021-43798"],"cvss-score":7.5}},"matched-at":"http://192.168.8.3:3000/public/plugins/x","ip":"192.168.8.3","port":"3000","matcher-name":"passwd"}
`
	fs, err := ParseNucleiJSONL(strings.NewReader(in))
	if err != nil || len(fs) != 2 {
		t.Fatalf("got %d findings, %v", len(fs), err)
	}
	f := fs[1]
	if f.Key != "CVE-2021-43798:passwd" || f.Severity != "high" || f.Port != 3000 || f.CVSS != 7.5 || !strings.Contains(f.Title, "CVE-2021-43798") || !strings.Contains(f.Description, "Fix: Upgrade") {
		t.Fatalf("bad finding: %+v", f)
	}
}

const configXML = `<?xml version="1.0"?>
<nmaprun><host><address addr="192.168.8.50" addrtype="ipv4"/><ports>
<port protocol="tcp" portid="443"><state state="open"/>
 <service name="http" product="nginx" version="1.18.0" tunnel="ssl"><cpe>cpe:/a:igor_sysoev:nginx:1.18.0</cpe></service>
 <script id="ssl-enum-ciphers" output="...">
  <table key="TLSv1.0"><table key="ciphers"/></table>
  <table key="TLSv1.2"><table key="ciphers"/></table>
  <elem key="least strength">C</elem>
 </script>
 <script id="ssl-cert" output="Subject: commonName=nas.local"><elem key="bits">1024</elem><elem key="type">rsa</elem>
  <table key="validity"><elem key="notBefore">2024-01-01T00:00:00</elem><elem key="notAfter">2026-11-01T00:00:00</elem></table></script>
</port>
<port protocol="tcp" portid="22"><state state="open"/>
 <service name="ssh" product="OpenSSH" version="9.2p1" extrainfo="Debian 2+deb12u3"><cpe>cpe:/a:openbsd:openssh:9.2p1</cpe><cpe>cpe:/o:linux:linux_kernel</cpe></service></port>
<port protocol="tcp" portid="21"><state state="open"/><service name="ftp"/>
 <script id="ftp-anon" output="Anonymous FTP login allowed (FTP code 230)"/></port>
<port protocol="tcp" portid="6379"><state state="open"/><service name="redis"/>
 <script id="redis-info" output="&#xa;  Version: 7.0.11&#xa;  Operating System: Linux"/></port>
</ports>
<hostscript><script id="smb2-security-mode" output="&#xa;  3:1:1: &#xa;    Message signing enabled but not required"/></hostscript>
</host></nmaprun>`

func TestParseConfigXML(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	fs, svcs, err := ParseConfigXML(strings.NewReader(configXML), now)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, f := range fs {
		keys[f.Key] = f.Severity
		if f.IP != "192.168.8.50" || f.Source != "config" {
			t.Errorf("bad finding identity: %+v", f)
		}
	}
	want := map[string]string{
		"tls-old-tlsv10":           "medium",
		"tls-weak-ciphers":         "medium",
		"tls-cert-expired":         "high",
		"tls-weak-key":             "medium",
		"ftp-anonymous":            "high",
		"redis-no-auth":            "critical",
		"smb-signing-not-required": "medium",
	}
	for k, sev := range want {
		if keys[k] != sev {
			t.Errorf("%s: got %q want %q (all: %v)", k, keys[k], sev, keys)
		}
	}
	if len(keys) != len(want) {
		t.Errorf("unexpected extra findings: %v", keys)
	}
	if len(svcs) != 4 || svcs[1].Product != "OpenSSH" || len(svcs[1].CPEs) != 2 || !strings.Contains(svcs[1].Version, "Debian") {
		t.Fatalf("services: %+v", svcs)
	}
}

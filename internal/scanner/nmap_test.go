package scanner

import (
	"strings"
	"testing"
)

const fixture = `<?xml version="1.0"?>
<nmaprun scanner="nmap" args="nmap -sV -O 192.168.8.0/24">
<host><status state="up" reason="arp-response"/>
<address addr="192.168.8.1" addrtype="ipv4"/>
<address addr="94:83:C4:AA:BB:CC" addrtype="mac" vendor="GL Technologies (Hong Kong)"/>
<hostnames><hostname name="console.gl-inet.com." type="PTR"/></hostnames>
<ports>
<port protocol="tcp" portid="22"><state state="open"/><service name="ssh" product="Dropbear sshd" version="2022.83" ostype="Linux"/></port>
<port protocol="tcp" portid="80"><state state="open"/><service name="http" product="nginx"/></port>
<port protocol="tcp" portid="443"><state state="open"/><service name="http" product="nginx" tunnel="ssl"/></port>
<port protocol="tcp" portid="23"><state state="closed"/><service name="telnet"/></port>
</ports>
<os><osmatch name="OpenWrt 21.02 (Linux 5.4)" accuracy="96"><osclass type="WAP" vendor="Linux" osfamily="Linux"/></osmatch>
<osmatch name="Linux 4.15 - 5.8" accuracy="90"><osclass type="general purpose" vendor="Linux" osfamily="Linux"/></osmatch></os>
</host>
<host><status state="up"/>
<address addr="192.168.8.22" addrtype="ipv4"/>
<address addr="BC:24:11:01:02:03" addrtype="mac" vendor="Proxmox Server Solutions GmbH"/>
<ports><port protocol="tcp" portid="8096"><state state="open"/><service name="http" product="Jellyfin"/></port></ports>
</host>
<host><status state="down"/><address addr="192.168.8.99" addrtype="ipv4"/></host>
<host><status state="up"/><address addr="192.168.8.5" addrtype="ipv4"/></host>
</nmaprun>`

func TestParseXML(t *testing.T) {
	hosts, err := ParseXML(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Fatalf("want 3 up hosts, got %d", len(hosts))
	}
	r := hosts[0]
	if r.IP != "192.168.8.1" || r.MAC != "94:83:c4:aa:bb:cc" || r.Vendor != "GL Technologies (Hong Kong)" || r.Hostname != "console.gl-inet.com" {
		t.Errorf("router addr fields: %+v", r)
	}
	if r.OSName != "OpenWrt 21.02 (Linux 5.4)" || r.OSFamily != "Linux" || r.OSType != "WAP" {
		t.Errorf("router os: %+v", r)
	}
	if len(r.Ports) != 3 {
		t.Fatalf("closed ports must be dropped: %+v", r.Ports)
	}
	if !r.Ports[2].TLS {
		t.Error("443 with tunnel=ssl should be TLS")
	}
	if got := ClassifyType(r); got != "Network" {
		t.Errorf("router type = %s", got)
	}
	if got := ClassifyType(hosts[1]); got != "Proxmox VM" {
		t.Errorf("proxmox type = %s", got)
	}
	if got := ClassifyType(hosts[2]); got != "Client" {
		t.Errorf("bare host type = %s", got)
	}
}

func TestIsWebPort(t *testing.T) {
	cases := []struct {
		p      Port
		scheme string
		ok     bool
	}{
		{Port{Port: 80, Service: "http"}, "http", true},
		{Port{Port: 443, Service: "http", TLS: true}, "https", true},
		{Port{Port: 8006, Service: "wpl-analytics"}, "https", true},
		{Port{Port: 8096, Service: "http"}, "http", true},
		{Port{Port: 22, Service: "ssh"}, "", false},
		{Port{Port: 993, Service: "imap", TLS: true}, "", false},
	}
	for _, c := range cases {
		s, ok := IsWebPort(c.p)
		if s != c.scheme || ok != c.ok {
			t.Errorf("%+v → %q %v", c.p, s, ok)
		}
	}
}

func TestProfileArgs(t *testing.T) {
	if a, err := (Profile{Name: "custom", Custom: "22, 80,8000-8100"}).args(); err != nil || a[1] != "22,80,8000-8100" {
		t.Fatalf("custom: %v %v", a, err)
	}
	if _, err := (Profile{Name: "custom", Custom: "22;rm -rf"}).args(); err == nil {
		t.Fatal("custom ports must be validated")
	}
}

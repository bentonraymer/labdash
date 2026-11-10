package vuln

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"9.2", "9.2", 0},
		{"9.2", "9.3", -1},
		{"9.10", "9.9", 1},
		{"9.2p1", "9.2", 1},
		{"9.2p1", "9.3p2", -1},
		{"9.2p1", "9.2p2", -1},
		{"1.18.0", "1.18", 1},
		{"1.0rc1", "1.0", -1},
		{"1.0", "1.0rc1", 1},
		{"2.4.57", "2.4.58", -1},
		{"10.0.19041", "10.0.17763", 1},
		{"8.9p1", "8.9P1", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseCPE(t *testing.T) {
	c, ok := ParseCPE("cpe:/a:openbsd:openssh:9.2p1")
	if !ok || c.Vendor != "openbsd" || c.Product != "openssh" || c.Version != "9.2p1" || c.Part != "a" {
		t.Fatalf("2.2: %+v", c)
	}
	c, ok = ParseCPE("cpe:2.3:a:openbsd:openssh:9.2:p1:*:*:*:*:*:*")
	if !ok || c.Version != "9.2p1" {
		t.Fatalf("2.3 with update: %+v", c)
	}
	c, ok = ParseCPE("cpe:2.3:a:f5:nginx:*:*:*:*:*:*:*:*")
	if !ok || c.Version != "" || c.Product != "nginx" {
		t.Fatalf("2.3 any version: %+v", c)
	}
	if c, ok = ParseCPE("cpe:/a:igor_sysoev:nginx"); !ok || c.Version != "" {
		t.Fatalf("2.2 no version: %+v", c)
	}
	if _, ok := ParseCPE("not a cpe"); ok {
		t.Fatal("garbage parsed")
	}
	if !distroHint("OpenSSH 9.2p1 Debian 2+deb12u3") || distroHint("nginx 1.25.3") {
		t.Fatal("distro hint wrong")
	}
}

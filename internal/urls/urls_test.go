package urls

import "testing"

func ip(p int) *int { return &p }

func TestResolve(t *testing.T) {
	cases := []struct {
		name string
		t    Target
		mode string
		want string
	}{
		{"dns mode uses dns", Target{DNSURL: "https://jf.home.example.com", DirectIP: "192.168.8.30", DirectPort: ip(8096)}, ModeDNS, "https://jf.home.example.com"},
		{"direct mode uses ip:port", Target{DNSURL: "https://jf.home.example.com", DirectIP: "192.168.8.30", DirectPort: ip(8096)}, ModeDirect, "http://192.168.8.30:8096"},
		{"direct with path + https", Target{DirectScheme: "https", DirectIP: "192.168.8.11", DirectPort: ip(8006), DirectPath: "#v1"}, ModeDirect, "https://192.168.8.11:8006/#v1"},
		{"default port omitted", Target{DirectIP: "192.168.8.2", DirectPort: ip(80)}, ModeDirect, "http://192.168.8.2"},
		{"direct falls back to dns", Target{DNSURL: "https://a.home.example.com"}, ModeDirect, "https://a.home.example.com"},
		{"dns falls back to direct", Target{DirectIP: "10.0.0.1", DirectPort: ip(3000)}, ModeDNS, "http://10.0.0.1:3000"},
		{"ipv6", Target{DirectIP: "fd00::1", DirectPort: ip(8080)}, ModeDirect, "http://[fd00::1]:8080"},
		{"nothing", Target{}, ModeDNS, ""},
	}
	for _, c := range cases {
		if got := c.t.Resolve(c.mode); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

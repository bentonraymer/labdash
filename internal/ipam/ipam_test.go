package ipam

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseAndNextFree(t *testing.T) {
	s, err := Parse("192.168.8.0/24", "192.168.8.1", "192.168.8.100", "192.168.8.199")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.All()); got != 256 {
		t.Fatalf("All = %d", got)
	}
	if s.Broadcast().String() != "192.168.8.255" {
		t.Fatalf("broadcast = %s", s.Broadcast())
	}
	used := map[netip.Addr]bool{netip.MustParseAddr("192.168.8.2"): true, netip.MustParseAddr("192.168.8.3"): true}
	a, ok := s.NextFree(used)
	if !ok || a.String() != "192.168.8.4" {
		t.Fatalf("next free = %s", a)
	}
	// Fill everything below DHCP: next free jumps past the range.
	for i := 2; i < 100; i++ {
		used[netip.AddrFrom4([4]byte{192, 168, 8, byte(i)})] = true
	}
	if a, _ := s.NextFree(used); a.String() != "192.168.8.200" {
		t.Fatalf("next free after DHCP = %s", a)
	}
	if !s.InDHCP(netip.MustParseAddr("192.168.8.150")) || s.InDHCP(netip.MustParseAddr("192.168.8.200")) {
		t.Fatal("InDHCP wrong")
	}
}

func TestParseErrors(t *testing.T) {
	bad := [][4]string{
		{"nope", "", "", ""},
		{"10.0.0.0/16", "", "", ""},               // too large
		{"192.168.8.0/24", "192.168.9.1", "", ""}, // gateway outside
		{"192.168.8.0/24", "", "192.168.8.10", ""},
		{"192.168.8.0/24", "", "192.168.8.50", "192.168.8.10"},
		{"fd00::/64", "", "", ""},
	}
	for _, b := range bad {
		if _, err := Parse(b[0], b[1], b[2], b[3]); err == nil {
			t.Errorf("expected error for %v", b)
		}
	}
	// Host bits are masked off.
	s, err := Parse("192.168.8.77/24", "", "", "")
	if err != nil || s.Prefix.String() != "192.168.8.0/24" {
		t.Fatalf("masking: %v %v", s.Prefix, err)
	}
}

func TestSortAndPorts(t *testing.T) {
	ips := []string{"192.168.8.100", "192.168.8.9", "bad", "192.168.8.20"}
	SortAddrs(ips)
	if !reflect.DeepEqual(ips, []string{"192.168.8.9", "192.168.8.20", "192.168.8.100", "bad"}) {
		t.Fatalf("sort = %v", ips)
	}
	if p, _ := NextFreePort(8080, map[int]bool{8080: true, 8081: true}); p != 8082 {
		t.Fatalf("port = %d", p)
	}
}

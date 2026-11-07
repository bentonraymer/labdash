// Package ipam provides IPv4 subnet math for the IP address manager.
package ipam

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// MaxHosts caps how large a subnet LabDash will enumerate (a /22).
const MaxHosts = 1024

type Subnet struct {
	Prefix    netip.Prefix
	Gateway   netip.Addr
	DHCPStart netip.Addr
	DHCPEnd   netip.Addr
}

// Parse validates an IPv4 CIDR and the optional gateway/DHCP range.
func Parse(cidr, gateway, dhcpStart, dhcpEnd string) (Subnet, error) {
	var s Subnet
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return s, fmt.Errorf("invalid CIDR %q", cidr)
	}
	if !p.Addr().Is4() {
		return s, errors.New("only IPv4 subnets are supported")
	}
	s.Prefix = p.Masked()
	if s.Prefix.Bits() < 22 {
		return s, fmt.Errorf("subnet too large (max /22, %d addresses)", MaxHosts)
	}
	parseIn := func(v, what string) (netip.Addr, error) {
		if v == "" {
			return netip.Addr{}, nil
		}
		a, err := netip.ParseAddr(v)
		if err != nil || !s.Prefix.Contains(a) {
			return netip.Addr{}, fmt.Errorf("%s %q is not inside %s", what, v, s.Prefix)
		}
		return a, nil
	}
	if s.Gateway, err = parseIn(gateway, "gateway"); err != nil {
		return s, err
	}
	if s.DHCPStart, err = parseIn(dhcpStart, "DHCP start"); err != nil {
		return s, err
	}
	if s.DHCPEnd, err = parseIn(dhcpEnd, "DHCP end"); err != nil {
		return s, err
	}
	if s.DHCPStart.IsValid() != s.DHCPEnd.IsValid() {
		return s, errors.New("set both DHCP start and end, or neither")
	}
	if s.DHCPStart.IsValid() && s.DHCPEnd.Less(s.DHCPStart) {
		return s, errors.New("DHCP end is before DHCP start")
	}
	return s, nil
}

// Network and Broadcast addresses (both unusable for hosts in /30 and larger).
func (s Subnet) Network() netip.Addr { return s.Prefix.Addr() }

func (s Subnet) Broadcast() netip.Addr {
	a := s.Prefix.Addr().As4()
	host := uint32(1)<<(32-s.Prefix.Bits()) - 1
	v := (uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])) | host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// All returns every address in the subnet, network and broadcast included.
func (s Subnet) All() []netip.Addr {
	var out []netip.Addr
	for a := s.Network(); s.Prefix.Contains(a); a = a.Next() {
		out = append(out, a)
		if len(out) > MaxHosts {
			break
		}
	}
	return out
}

func (s Subnet) InDHCP(a netip.Addr) bool {
	if !s.DHCPStart.IsValid() {
		return false
	}
	return !a.Less(s.DHCPStart) && !s.DHCPEnd.Less(a)
}

// Reserved reports network/broadcast/gateway addresses.
func (s Subnet) Reserved(a netip.Addr) bool {
	if s.Prefix.Bits() <= 30 && (a == s.Network() || a == s.Broadcast()) {
		return true
	}
	return s.Gateway.IsValid() && a == s.Gateway
}

// NextFree returns the lowest usable static address that isn't reserved, in
// the DHCP range, or in `used`.
func (s Subnet) NextFree(used map[netip.Addr]bool) (netip.Addr, bool) {
	for _, a := range s.All() {
		if s.Reserved(a) || s.InDHCP(a) || used[a] {
			continue
		}
		return a, true
	}
	return netip.Addr{}, false
}

// SortAddrs sorts dotted IPv4 strings numerically; invalid strings sort last.
func SortAddrs(ips []string) {
	sort.SliceStable(ips, func(i, j int) bool {
		a, ea := netip.ParseAddr(ips[i])
		b, eb := netip.ParseAddr(ips[j])
		if ea != nil || eb != nil {
			return ea == nil
		}
		return a.Less(b)
	})
}

// NextFreePort returns the first port >= from (wrapping to 1024) not in used.
func NextFreePort(from int, used map[int]bool) (int, bool) {
	if from < 1 || from > 65535 {
		from = 8000
	}
	for p := from; p <= 65535; p++ {
		if !used[p] {
			return p, true
		}
	}
	for p := 1024; p < from; p++ {
		if !used[p] {
			return p, true
		}
	}
	return 0, false
}

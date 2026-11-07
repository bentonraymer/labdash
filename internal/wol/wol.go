// Package wol sends Wake-on-LAN magic packets.
package wol

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

// MagicPacket builds 6×0xFF followed by the MAC repeated 16 times.
func MagicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(mac))
	if err != nil || len(hw) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q", mac)
	}
	var b bytes.Buffer
	b.Write(bytes.Repeat([]byte{0xff}, 6))
	for i := 0; i < 16; i++ {
		b.Write(hw)
	}
	return b.Bytes(), nil
}

// Send broadcasts the packet on UDP 9 to the global broadcast address and,
// if given, a subnet broadcast (e.g. 192.168.8.255). Needs host networking
// in Docker to reach the LAN.
func Send(mac string, broadcasts ...string) error {
	pkt, err := MagicPacket(mac)
	if err != nil {
		return err
	}
	targets := append([]string{"255.255.255.255"}, broadcasts...)
	var lastErr error
	sent := 0
	for _, t := range targets {
		if t == "" {
			continue
		}
		conn, err := net.Dial("udp4", net.JoinHostPort(t, "9"))
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := conn.Write(pkt); err != nil {
			lastErr = err
		} else {
			sent++
		}
		conn.Close()
	}
	if sent == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

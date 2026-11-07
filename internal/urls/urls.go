// Package urls builds a service's launch URL for the current access mode.
package urls

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	ModeDNS    = "dns"
	ModeDirect = "direct"
)

type Target struct {
	DNSURL       string
	DirectScheme string
	DirectIP     string
	DirectPort   *int
	DirectPath   string
}

// Direct builds scheme://ip[:port]/path, or "" if no IP is set.
func (t Target) Direct() string {
	if t.DirectIP == "" {
		return ""
	}
	scheme := t.DirectScheme
	if scheme == "" {
		scheme = "http"
	}
	host := t.DirectIP
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") { // IPv6
		host = "[" + host + "]"
	}
	if t.DirectPort != nil && !isDefaultPort(scheme, *t.DirectPort) {
		host = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(*t.DirectPort))
	}
	path := t.DirectPath
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

// Resolve returns the URL for mode, falling back to the other form when the
// preferred one is empty.
func (t Target) Resolve(mode string) string {
	direct := t.Direct()
	if mode == ModeDirect {
		if direct != "" {
			return direct
		}
		return t.DNSURL
	}
	if t.DNSURL != "" {
		return t.DNSURL
	}
	return direct
}

func isDefaultPort(scheme string, port int) bool {
	return (scheme == "http" && port == 80) || (scheme == "https" && port == 443)
}

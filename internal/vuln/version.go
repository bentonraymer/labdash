// Package vuln finds known vulnerabilities on scanned devices: CVEs matched
// by product version against a local NVD copy, safe Nuclei web checks and
// nmap configuration checks (TLS, unauthenticated services).
package vuln

import (
	"strconv"
	"strings"
	"unicode"
)

// tokens splits "9.2p1" into ["9","2","p","1"] and "1.18.0-rc2" into
// ["1","18","0","rc","2"].
func tokens(v string) []string {
	var out []string
	var cur strings.Builder
	kind := 0 // 1 digit, 2 letter
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(strings.TrimSpace(v)) {
		k := 0
		switch {
		case unicode.IsDigit(r):
			k = 1
		case unicode.IsLetter(r):
			k = 2
		}
		if k == 0 {
			flush()
			kind = 0
			continue
		}
		if k != kind {
			flush()
		}
		kind = k
		cur.WriteRune(r)
	}
	flush()
	return out
}

// preRelease markers sort before the release itself (1.0rc1 < 1.0).
var preRelease = map[string]bool{"alpha": true, "a": true, "beta": true, "b": true, "rc": true, "pre": true, "dev": true}

// CompareVersions returns -1, 0 or 1. Numeric parts compare numerically,
// letters lexically; "9.2" < "9.2p1" < "9.3" and "1.0rc1" < "1.0".
func CompareVersions(a, b string) int {
	ta, tb := tokens(a), tokens(b)
	for i := 0; i < len(ta) || i < len(tb); i++ {
		if i >= len(ta) {
			if preRelease[tb[i]] {
				return 1
			}
			return -1
		}
		if i >= len(tb) {
			if preRelease[ta[i]] {
				return -1
			}
			return 1
		}
		x, y := ta[i], tb[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil: // number vs letters: "1.0.1" > "1.0a"? treat letters as lower
			return 1
		case yerr == nil:
			return -1
		default:
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

// CPE is the part of a CPE name LabDash uses for matching.
type CPE struct {
	Part    string // a (application) | o (OS) | h (hardware)
	Vendor  string
	Product string
	Version string // includes the "update" field (9.2 + p1 → 9.2p1)
}

func unescape(s string) string { return strings.ReplaceAll(s, `\`, "") }

// ParseCPE accepts CPE 2.2 URIs (cpe:/a:openbsd:openssh:9.2p1, as nmap
// prints them) and CPE 2.3 strings (cpe:2.3:a:openbsd:openssh:9.2:p1:...).
func ParseCPE(s string) (CPE, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	var f []string
	switch {
	case strings.HasPrefix(s, "cpe:2.3:"):
		f = strings.Split(s[len("cpe:2.3:"):], ":")
	case strings.HasPrefix(s, "cpe:/"):
		f = strings.Split(s[len("cpe:/"):], ":")
	default:
		return CPE{}, false
	}
	if len(f) < 3 || f[1] == "" || f[2] == "" {
		return CPE{}, false
	}
	c := CPE{Part: f[0], Vendor: unescape(f[1]), Product: unescape(f[2])}
	if len(f) > 3 && f[3] != "*" && f[3] != "-" {
		c.Version = unescape(f[3])
		if len(f) > 4 && f[4] != "*" && f[4] != "-" && f[4] != "" {
			c.Version += unescape(f[4])
		}
	}
	return c, true
}

// distroHint reports whether a banner suggests a distro package, whose
// maintainers often backport security fixes without changing the version.
func distroHint(banner string) bool {
	b := strings.ToLower(banner)
	for _, d := range []string{"debian", "ubuntu", "deb1", "+deb", "red hat", "rhel", "centos", "rocky", "alma", "fedora", "suse", "alpine", "el7", "el8", "el9"} {
		if strings.Contains(b, d) {
			return true
		}
	}
	return false
}

package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/urls"
)

// CheckSpec is everything needed to run one check.
type CheckSpec struct {
	Kind           string // http | tcp | ping | dns
	Target         string
	Timeout        time.Duration
	ExpectedStatus string // e.g. "200-399,401"
	Keyword        string
	IgnoreTLS      bool
}

type Result struct {
	Up      bool
	Latency time.Duration
	Message string
}

func Check(ctx context.Context, s CheckSpec) Result {
	if s.Timeout <= 0 {
		s.Timeout = 10 * time.Second
	}
	if s.Target == "" {
		return Result{Message: "no target: set a URL/IP on the service or an explicit monitor target"}
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	switch s.Kind {
	case "http":
		return checkHTTP(ctx, s)
	case "tcp":
		return checkTCP(ctx, s.Target)
	case "ping":
		return checkPing(ctx, s.Target, s.Timeout)
	case "dns":
		return checkDNS(ctx, s.Target)
	}
	return Result{Message: "unknown monitor type " + s.Kind}
}

func checkHTTP(ctx context.Context, s CheckSpec) Result {
	tr := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: s.IgnoreTLS}, //nolint:gosec // opt-in for self-signed homelab certs
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Target, nil)
	if err != nil {
		return Result{Message: err.Error()}
	}
	req.Header.Set("User-Agent", "LabDash-Monitor/1.0")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{Latency: time.Since(start), Message: cleanErr(err)}
	}
	defer resp.Body.Close()
	var body []byte
	if s.Keyword != "" {
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	lat := time.Since(start)
	if !StatusMatches(resp.StatusCode, s.ExpectedStatus) {
		return Result{Latency: lat, Message: fmt.Sprintf("unexpected status %s", resp.Status)}
	}
	if s.Keyword != "" && !bytes.Contains(body, []byte(s.Keyword)) {
		return Result{Latency: lat, Message: fmt.Sprintf("keyword %q not found", s.Keyword)}
	}
	return Result{Up: true, Latency: lat, Message: resp.Status}
}

// StatusMatches checks a code against "200-399,401,5xx"-style specs.
func StatusMatches(code int, spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "200-399"
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		switch {
		case part == "":
			continue
		case len(part) == 3 && strings.HasSuffix(part, "xx"):
			if d, err := strconv.Atoi(part[:1]); err == nil && code/100 == d {
				return true
			}
		case strings.Contains(part, "-"):
			lo, hi, _ := strings.Cut(part, "-")
			l, e1 := strconv.Atoi(strings.TrimSpace(lo))
			h, e2 := strconv.Atoi(strings.TrimSpace(hi))
			if e1 == nil && e2 == nil && code >= l && code <= h {
				return true
			}
		default:
			if n, err := strconv.Atoi(part); err == nil && n == code {
				return true
			}
		}
	}
	return false
}

func checkTCP(ctx context.Context, target string) Result {
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", target)
	lat := time.Since(start)
	if err != nil {
		return Result{Latency: lat, Message: cleanErr(err)}
	}
	conn.Close()
	return Result{Up: true, Latency: lat, Message: "port open"}
}

var pingTimeRe = regexp.MustCompile(`time[=<]([\d.]+)\s*ms`)

// checkPing shells out to the system ping (iputils in the container), which
// avoids needing raw-socket code here.
func checkPing(ctx context.Context, host string, timeout time.Duration) Result {
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	start := time.Now()
	out, err := exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(secs), host).CombinedOutput()
	lat := time.Since(start)
	if m := pingTimeRe.FindSubmatch(out); m != nil {
		if ms, perr := strconv.ParseFloat(string(m[1]), 64); perr == nil {
			lat = time.Duration(ms * float64(time.Millisecond))
		}
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return Result{Latency: lat, Message: "no reply"}
		}
		return Result{Latency: lat, Message: cleanErr(err)}
	}
	return Result{Up: true, Latency: lat, Message: "reply received"}
}

// checkDNS resolves a hostname. "host@1.2.3.4" queries a specific server.
func checkDNS(ctx context.Context, target string) Result {
	host, server, _ := strings.Cut(target, "@")
	r := net.DefaultResolver
	if server != "" {
		if !strings.Contains(server, ":") {
			server += ":53"
		}
		r = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, server)
		}}
	}
	start := time.Now()
	addrs, err := r.LookupHost(ctx, host)
	lat := time.Since(start)
	if err != nil {
		return Result{Latency: lat, Message: cleanErr(err)}
	}
	if len(addrs) == 0 {
		return Result{Latency: lat, Message: "no records"}
	}
	return Result{Up: true, Latency: lat, Message: strings.Join(addrs, ", ")}
}

func cleanErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// ServiceTarget is the subset of a service used to derive a monitor target.
type ServiceTarget = urls.Target

// DeriveTarget picks what to check when a monitor has no explicit target.
// HTTP follows the global access mode so monitors match how links open;
// TCP and ping prefer the direct IP since that's what they test.
func DeriveTarget(kind string, t ServiceTarget, mode string) string {
	dnsHost, dnsPort := "", ""
	if u, err := url.Parse(t.DNSURL); err == nil && u.Host != "" {
		dnsHost = u.Hostname()
		dnsPort = u.Port()
		if dnsPort == "" {
			dnsPort = map[string]string{"https": "443", "http": "80"}[u.Scheme]
		}
	}
	switch kind {
	case "http":
		return t.Resolve(mode)
	case "tcp":
		if t.DirectIP != "" && t.DirectPort != nil {
			return net.JoinHostPort(t.DirectIP, strconv.Itoa(*t.DirectPort))
		}
		if dnsHost != "" && dnsPort != "" {
			return net.JoinHostPort(dnsHost, dnsPort)
		}
	case "ping":
		if t.DirectIP != "" {
			return t.DirectIP
		}
		return dnsHost
	case "dns":
		return dnsHost
	}
	return ""
}

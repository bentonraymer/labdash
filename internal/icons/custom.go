package icons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Custom icons (uploads and fetched favicons) live in <dir>/custom and are
// served at /icons/custom/<sha>.<ext>.

const maxIconBytes = 2 << 20

var extByMime = map[string]string{
	"image/png":                ".png",
	"image/jpeg":               ".jpg",
	"image/gif":                ".gif",
	"image/webp":               ".webp",
	"image/svg+xml":            ".svg",
	"image/x-icon":             ".ico",
	"image/vnd.microsoft.icon": ".ico",
}

// SniffImage returns the file extension for icon bytes, or an error if the
// data is not a supported image type.
func SniffImage(b []byte) (string, error) {
	mime := http.DetectContentType(b)
	if i := strings.IndexByte(mime, ';'); i > 0 {
		mime = mime[:i]
	}
	if ext, ok := extByMime[mime]; ok {
		return ext, nil
	}
	// DetectContentType reports SVG as text/xml or text/plain.
	head := bytes.ToLower(b[:min(len(b), 1024)])
	if bytes.Contains(head, []byte("<svg")) {
		return ".svg", nil
	}
	return "", fmt.Errorf("unsupported image type %q", mime)
}

// SaveCustom stores icon bytes content-addressed and returns its public URL.
func (c *Cache) SaveCustom(b []byte) (string, error) {
	if len(b) == 0 || len(b) > maxIconBytes {
		return "", errors.New("icon must be between 1 byte and 2 MB")
	}
	ext, err := SniffImage(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	name := hex.EncodeToString(sum[:12]) + ext
	dir := filepath.Join(c.dir, "custom")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, b, 0o640); err != nil {
			return "", err
		}
	}
	return "/icons/custom/" + name, nil
}

// ServeCustom handles /icons/custom/{file}.
func (c *Cache) ServeCustom(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	if !customNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(c.dir, "custom", name))
}

var (
	customNameRe = regexp.MustCompile(`^[a-f0-9]{24}\.(png|jpg|gif|webp|svg|ico)$`)
	linkTagRe    = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	relRe        = regexp.MustCompile(`(?is)\brel\s*=\s*["']?([^"'>]+)`)
	hrefRe       = regexp.MustCompile(`(?is)\bhref\s*=\s*["']?([^"'\s>]+)`)
	sizesRe      = regexp.MustCompile(`(?is)\bsizes\s*=\s*["']?(\d+)`)
)

// favClient tolerates self-signed certs: homelab services commonly use them.
var favClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // homelab self-signed certs
	},
}

// FetchFavicon loads pageURL, finds the best icon link (largest declared size,
// apple-touch-icon preferred), downloads it and stores it as a custom icon.
func (c *Cache) FetchFavicon(ctx context.Context, pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("need an http(s) URL")
	}
	candidates := []string{}
	if body, final, err := get(ctx, u.String(), 512<<10); err == nil {
		candidates = append(candidates, findIconLinks(body, final)...)
	}
	candidates = append(candidates, u.Scheme+"://"+u.Host+"/favicon.ico", u.Scheme+"://"+u.Host+"/apple-touch-icon.png")
	var lastErr error = errors.New("no icon found")
	for _, cand := range candidates {
		b, _, err := get(ctx, cand, maxIconBytes)
		if err != nil {
			lastErr = err
			continue
		}
		if path, err := c.SaveCustom(b); err == nil {
			return path, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

func get(ctx context.Context, rawURL string, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "LabDash/1.0 (+favicon fetch)")
	resp, err := favClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, resp.Request.URL, err
}

// findIconLinks returns absolute icon URLs from an HTML page, best first.
func findIconLinks(html []byte, base *url.URL) []string {
	type cand struct {
		url   string
		score int
	}
	var cs []cand
	for _, tag := range linkTagRe.FindAll(html, -1) {
		rel := relRe.FindSubmatch(tag)
		href := hrefRe.FindSubmatch(tag)
		if rel == nil || href == nil {
			continue
		}
		r := strings.ToLower(string(rel[1]))
		if !strings.Contains(r, "icon") {
			continue
		}
		ref, err := url.Parse(strings.TrimSpace(string(href[1])))
		if err != nil {
			continue
		}
		score := 10
		if strings.Contains(r, "apple-touch") {
			score = 180
		}
		if m := sizesRe.FindSubmatch(tag); m != nil {
			var n int
			fmt.Sscanf(string(m[1]), "%d", &n)
			if n > 0 && n <= 512 {
				score = max(score, n)
			}
		}
		if strings.HasSuffix(strings.ToLower(ref.Path), ".svg") {
			score = 400
		}
		cs = append(cs, cand{base.ResolveReference(ref).String(), score})
	}
	// stable sort by score desc
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].score > cs[j-1].score; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.url
	}
	return out
}

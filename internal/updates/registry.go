// Package updates finds out which services have newer versions available:
// container images (registry digests and tags), GitHub releases, Proxmox
// Helper Script apps (via the GitHub repo their script installs from) and
// manually entered versions. It only reads; nothing is ever updated.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/vuln"
)

// ImageRef is a parsed image reference like ghcr.io/owner/app:1.2.
type ImageRef struct {
	Registry string // registry host, e.g. registry-1.docker.io
	Repo     string // e.g. library/nginx
	Tag      string
}

func (r ImageRef) String() string {
	host := r.Registry
	if host == "registry-1.docker.io" {
		host = "docker.io"
	}
	return host + "/" + r.Repo + ":" + r.Tag
}

// ParseImage parses a Docker image reference (digests are dropped).
func ParseImage(s string) (ImageRef, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "sha256:") {
		return ImageRef{}, errors.New("no image name")
	}
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	ref := ImageRef{Registry: "registry-1.docker.io", Tag: "latest"}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 2 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		ref.Registry, s = parts[0], parts[1]
	}
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.Tag, s = s[i+1:], s[:i]
	}
	switch ref.Registry {
	case "docker.io", "index.docker.io":
		ref.Registry = "registry-1.docker.io"
	}
	if ref.Registry == "registry-1.docker.io" && !strings.Contains(s, "/") {
		s = "library/" + s
	}
	ref.Repo = s
	return ref, nil
}

// Registry talks to OCI registries anonymously (public images).
type Registry struct {
	HTTP   *http.Client
	Scheme string // "https" (tests use "http")
	mu     sync.Mutex
	tokens map[string]string // realm+scope → bearer token
}

func NewRegistry() *Registry {
	return &Registry{HTTP: &http.Client{Timeout: 20 * time.Second}, Scheme: "https", tokens: map[string]string{}}
}

var authParamRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// do runs a registry request, fetching an anonymous pull token on 401.
func (r *Registry) do(ctx context.Context, method, rawURL string, ref ImageRef) (*http.Response, error) {
	try := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", manifestAccept)
		req.Header.Set("User-Agent", "LabDash update checker")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return r.HTTP.Do(req)
	}
	key := ref.Registry + "/" + ref.Repo
	r.mu.Lock()
	tok := r.tokens[key]
	r.mu.Unlock()
	resp, err := try(tok)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	challenge := resp.Header.Get("Www-Authenticate")
	resp.Body.Close()
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return nil, errors.New("registry wants credentials (private image?)")
	}
	params := map[string]string{}
	for _, m := range authParamRe.FindAllStringSubmatch(challenge, -1) {
		params[strings.ToLower(m[1])] = m[2]
	}
	if params["realm"] == "" {
		return nil, errors.New("registry auth challenge without realm")
	}
	q := url.Values{}
	if params["service"] != "" {
		q.Set("service", params["service"])
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + ref.Repo + ":pull"
	}
	q.Set("scope", scope)
	treq, err := http.NewRequestWithContext(ctx, http.MethodGet, params["realm"]+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	tresp, err := r.HTTP.Do(treq)
	if err != nil {
		return nil, err
	}
	defer tresp.Body.Close()
	if tresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry token: %s (private image?)", tresp.Status)
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tresp.Body).Decode(&t); err != nil {
		return nil, err
	}
	tok = t.Token
	if tok == "" {
		tok = t.AccessToken
	}
	r.mu.Lock()
	r.tokens[key] = tok
	r.mu.Unlock()
	return try(tok)
}

// Digest returns the registry's current digest for ref's tag.
func (r *Registry) Digest(ctx context.Context, ref ImageRef) (string, error) {
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", r.Scheme, ref.Registry, ref.Repo, ref.Tag)
	resp, err := r.do(ctx, http.MethodHead, u, ref)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("tag %q not found in %s", ref.Tag, ref.Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry: %s", resp.Status)
	}
	d := resp.Header.Get("Docker-Content-Digest")
	if d == "" {
		return "", errors.New("registry didn't return a digest")
	}
	return d, nil
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// Tags lists a repository's tags (up to maxPages pages of 1000).
func (r *Registry) Tags(ctx context.Context, ref ImageRef, maxPages int) ([]string, error) {
	next := fmt.Sprintf("%s://%s/v2/%s/tags/list?n=1000", r.Scheme, ref.Registry, ref.Repo)
	var all []string
	for page := 0; next != "" && page < maxPages; page++ {
		resp, err := r.do(ctx, http.MethodGet, next, ref)
		if err != nil {
			return all, err
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body)
		link := resp.Header.Get("Link")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("registry tags: %s", resp.Status)
		}
		if err != nil {
			return all, err
		}
		all = append(all, body.Tags...)
		next = ""
		if m := linkNextRe.FindStringSubmatch(link); m != nil {
			if strings.HasPrefix(m[1], "/") {
				next = r.Scheme + "://" + ref.Registry + m[1]
			} else {
				next = m[1]
			}
		}
	}
	return all, nil
}

// ── version tags ──────────────────────────────────────────────────────────

var (
	versionTagRe = regexp.MustCompile(`^v?\d+(\.\d+)+`)
	digitsRe     = regexp.MustCompile(`\d+`)
	preReleaseRe = regexp.MustCompile(`(?i)(alpha|beta|rc|dev|pre|nightly|snapshot|test)`)
)

// IsVersionTag reports whether a tag looks like a version (1.2, v3.4.5-alpine).
func IsVersionTag(tag string) bool { return versionTagRe.MatchString(tag) }

// tagShape reduces a tag to its pattern, e.g. "v1.27.3-alpine" → "vN.N.N-alpine",
// so only tags of the same flavour are compared.
func tagShape(tag string) string { return digitsRe.ReplaceAllString(tag, "N") }

// NewestTag returns the highest tag with the same shape as current (and
// no pre-release marker unless current has one), or "" if none is newer.
func NewestTag(current string, tags []string) string {
	if !IsVersionTag(current) {
		return ""
	}
	shape := tagShape(current)
	pre := preReleaseRe.MatchString(current)
	var cands []string
	for _, t := range tags {
		if tagShape(t) != shape || (!pre && preReleaseRe.MatchString(t)) {
			continue
		}
		if vuln.CompareVersions(strings.TrimPrefix(t, "v"), strings.TrimPrefix(current, "v")) > 0 {
			cands = append(cands, t)
		}
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		return vuln.CompareVersions(strings.TrimPrefix(cands[i], "v"), strings.TrimPrefix(cands[j], "v")) > 0
	})
	return cands[0]
}

var compareVersions = vuln.CompareVersions

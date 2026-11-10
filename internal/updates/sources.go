package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ── Docker (through a read-only docker-socket-proxy) ─────────────────────

// Container is the subset of /containers/json LabDash needs.
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Labels  map[string]string `json:"Labels"`
}

// Name is the container name without the leading slash.
func (c Container) Name() string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

// ImageInfo is the subset of /images/{id}/json LabDash needs.
type ImageInfo struct {
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// Docker reads containers and images from a socket proxy.
type Docker struct{ HTTP *http.Client }

func NewDocker() *Docker { return &Docker{HTTP: &http.Client{Timeout: 15 * time.Second}} }

func (d *Docker) get(ctx context.Context, base, path string, out any) error {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return errors.New("Docker URL must look like http://192.168.8.22:2375")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach Docker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return errors.New("the socket proxy refused (enable CONTAINERS=1 and IMAGES=1)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Docker: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

func (d *Docker) Containers(ctx context.Context, base string) ([]Container, error) {
	var cs []Container
	err := d.get(ctx, base, "/containers/json?all=1", &cs)
	return cs, err
}

func (d *Docker) Image(ctx context.Context, base, id string) (ImageInfo, error) {
	var im ImageInfo
	err := d.get(ctx, base, "/images/"+id+"/json", &im)
	return im, err
}

// ── GitHub releases ───────────────────────────────────────────────────────

// Release is a project's latest release (or tag when it has no releases).
type Release struct {
	Version   string `json:"version"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Notes     string `json:"notes"`
	Published string `json:"published"`
}

// GitHub reads releases; Token is optional (raises the 60/hour limit).
type GitHub struct {
	HTTP  *http.Client
	Base  string // https://api.github.com
	Token func(ctx context.Context) string
	mu    sync.Mutex
	cache map[string]cachedRelease
}

type cachedRelease struct {
	r   Release
	err error
	at  time.Time
}

func NewGitHub() *GitHub {
	return &GitHub{HTTP: &http.Client{Timeout: 15 * time.Second}, Base: "https://api.github.com", cache: map[string]cachedRelease{}}
}

var repoRe = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

// NormalizeRepo accepts owner/repo or a github.com URL.
func NormalizeRepo(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	if parts := strings.Split(s, "/"); len(parts) > 2 {
		s = parts[0] + "/" + parts[1]
	}
	return s, repoRe.MatchString(s)
}

func (g *GitHub) api(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.Base+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LabDash update checker")
	if g.Token != nil {
		if t := g.Token(ctx); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if resp.Header.Get("X-Ratelimit-Remaining") == "0" {
			return resp.StatusCode, errors.New("GitHub rate limit reached; add a GitHub token in Settings → Updates")
		}
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("GitHub: %s", resp.Status)
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// Latest returns the latest release (cached for an hour).
func (g *GitHub) Latest(ctx context.Context, repo string) (Release, error) {
	g.mu.Lock()
	c, ok := g.cache[repo]
	g.mu.Unlock()
	if ok && time.Since(c.at) < time.Hour {
		return c.r, c.err
	}
	var rel struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		HTMLURL     string `json:"html_url"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
	}
	var out Release
	code, err := g.api(ctx, "/repos/"+repo+"/releases/latest", &rel)
	switch {
	case err == nil:
		out = Release{Version: rel.TagName, Name: rel.Name, URL: rel.HTMLURL, Notes: trimNotes(rel.Body), Published: rel.PublishedAt}
	case code == http.StatusNotFound:
		// No releases: fall back to the newest tag.
		var tags []struct {
			Name string `json:"name"`
		}
		if _, terr := g.api(ctx, "/repos/"+repo+"/tags?per_page=30", &tags); terr != nil {
			err = fmt.Errorf("no releases or tags found for %s", repo)
		} else if t := newestPlainTag(tags); t != "" {
			out, err = Release{Version: t, URL: "https://github.com/" + repo + "/releases/tag/" + t}, nil
		} else {
			err = fmt.Errorf("no releases or version tags found for %s", repo)
		}
	}
	g.mu.Lock()
	g.cache[repo] = cachedRelease{out, err, time.Now()}
	g.mu.Unlock()
	return out, err
}

func newestPlainTag(tags []struct {
	Name string `json:"name"`
}) string {
	var names []string
	for _, t := range tags {
		if IsVersionTag(t.Name) && !preReleaseRe.MatchString(t.Name) {
			names = append(names, t.Name)
		}
	}
	best := ""
	for _, n := range names {
		if best == "" || compareLoose(n, best) > 0 {
			best = n
		}
	}
	return best
}

func trimNotes(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 6000 {
		s = string(r[:6000]) + "\n\n…"
	}
	return s
}

// ── Proxmox VE Helper Scripts ─────────────────────────────────────────────

// HelperScripts resolves a community-scripts app slug to the GitHub repo
// its update function installs from.
type HelperScripts struct {
	HTTP  *http.Client
	Base  string // raw URL of the ct/ folder
	mu    sync.Mutex
	cache map[string]cachedRepo
}

type cachedRepo struct {
	repo string
	err  error
	at   time.Time
}

func NewHelperScripts() *HelperScripts {
	return &HelperScripts{HTTP: &http.Client{Timeout: 15 * time.Second},
		Base: "https://raw.githubusercontent.com/community-scripts/ProxmoxVE/main/ct/", cache: map[string]cachedRepo{}}
}

var (
	slugRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	ghReleaseRe   = regexp.MustCompile(`check_for_gh_release\s+"[^"]*"\s+"([\w.-]+/[\w.-]+)"`)
	latestRelRe   = regexp.MustCompile(`get_latest_github_release\s+"([\w.-]+/[\w.-]+)"`)
	headerRepoRe  = regexp.MustCompile(`(?m)^#.*(?:Github|Source):\s*https://github\.com/([\w.-]+/[\w.-]+)`)
	apiReleasesRe = regexp.MustCompile(`api\.github\.com/repos/([\w.-]+/[\w.-]+)/releases`)
)

// RepoFromScript finds the app's own release repo in a ct/<slug>.sh script.
func RepoFromScript(script string) string {
	for _, re := range []*regexp.Regexp{ghReleaseRe, latestRelRe, headerRepoRe, apiReleasesRe} {
		if m := re.FindStringSubmatch(script); m != nil && !strings.HasPrefix(m[1], "community-scripts/") {
			return strings.TrimSuffix(m[1], ".git")
		}
	}
	return ""
}

// Repo resolves a slug (cached for a day).
func (h *HelperScripts) Repo(ctx context.Context, slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRe.MatchString(slug) {
		return "", errors.New("enter the script's name as in its URL, e.g. homepage")
	}
	h.mu.Lock()
	c, ok := h.cache[slug]
	h.mu.Unlock()
	if ok && time.Since(c.at) < 24*time.Hour {
		return c.repo, c.err
	}
	repo, err := h.fetch(ctx, slug)
	h.mu.Lock()
	h.cache[slug] = cachedRepo{repo, err, time.Now()}
	h.mu.Unlock()
	return repo, err
}

func (h *HelperScripts) fetch(ctx context.Context, slug string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.Base+slug+".sh", nil)
	if err != nil {
		return "", err
	}
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no helper script called %q", slug)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("helper scripts: %s", resp.Status)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	repo := RepoFromScript(string(body))
	if repo == "" {
		return "", errors.New("this script doesn't install from GitHub releases (e.g. it uses apt); set a GitHub repo instead")
	}
	return repo, nil
}

// ── version comparison ────────────────────────────────────────────────────

var leadingJunkRe = regexp.MustCompile(`^[^0-9]*`)

// CleanVersion strips prefixes like "v" or "app-v" ("v2.1.0" → "2.1.0").
func CleanVersion(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "0123456789"); i > 0 {
		s = leadingJunkRe.ReplaceAllString(s, "")
	}
	return s
}

// compareLoose compares two version strings after cleaning them.
func compareLoose(a, b string) int {
	return compareVersions(CleanVersion(a), CleanVersion(b))
}

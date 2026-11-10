package updates

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/events"
	"github.com/bentonraymer/labdash/internal/proxmox"
)

const tsFormat = "2006-01-02T15:04:05.000Z"

// Result is the outcome of checking one service.
type Result struct {
	UsedSource     string
	Status         string // current | outdated | unknown | error | untracked
	RunningVersion string
	LatestVersion  string
	Image          string
	NewDigest      bool
	LatestURL      string
	Notes          string
	Published      string
	ResolvedRepo   string
	Detail         string
	Error          string
}

// Config is how a service's updates are tracked (one update_checks row).
type Config struct {
	ServiceID        int64
	Name             string
	Source           string
	DockerEndpointID *int64
	Container        string
	GitHubRepo       string
	HelperSlug       string
	CurrentVersion   string
	IgnoredVersion   string
	// From the service's hosting
	HostID        *int64
	Runtime       string
	ContainerName string
}

// Endpoint is a read-only Docker API (socket proxy).
type Endpoint struct {
	ID     int64
	Name   string
	URL    string
	HostID *int64
}

// Checker runs update checks on a schedule and sends the weekly digest.
type Checker struct {
	DB       *sql.DB
	Hub      *events.Hub
	Docker   *Docker
	Registry *Registry
	GitHub   *GitHub
	Helper   *HelperScripts
	// Proxmox returns the configured connections (nil = none).
	Proxmox func(ctx context.Context) []proxmox.Target
	// Digest sends the weekly summary; DigestOn says whether it's enabled.
	Digest   func(ctx context.Context, title, body string)
	DigestOn func(ctx context.Context) bool

	mu       sync.Mutex
	checking bool
	lastRun  string
	nodes    []proxmox.NodeUpdates
	nodesAt  string
}

func NewChecker(db *sql.DB, hub *events.Hub) *Checker {
	return &Checker{DB: db, Hub: hub, Docker: NewDocker(), Registry: NewRegistry(), GitHub: NewGitHub(), Helper: NewHelperScripts()}
}

// State is what the API reports besides per-service rows.
type State struct {
	Checking bool                  `json:"checking"`
	LastRun  string                `json:"lastRun"`
	Nodes    []proxmox.NodeUpdates `json:"nodes"`
	NodesAt  string                `json:"nodesAt"`
}

func (c *Checker) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return State{Checking: c.checking, LastRun: c.lastRun, Nodes: append([]proxmox.NodeUpdates{}, c.nodes...), NodesAt: c.nodesAt}
}

func (c *Checker) publish() {
	if c.Hub != nil {
		c.Hub.Publish("updates", map[string]bool{"checking": c.State().Checking})
	}
}

// Run checks everything shortly after start and then every 6 hours, and
// sends the weekly digest on Monday mornings.
func (c *Checker) Run(ctx context.Context) {
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	hourly := time.NewTicker(time.Hour)
	defer hourly.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.CheckAll(ctx)
			timer.Reset(6 * time.Hour)
		case <-hourly.C:
			c.maybeDigest(ctx, time.Now())
		}
	}
}

// CheckAll checks every (non-archived) service and the Proxmox nodes.
func (c *Checker) CheckAll(ctx context.Context) error {
	c.mu.Lock()
	if c.checking {
		c.mu.Unlock()
		return errors.New("a check is already running")
	}
	c.checking = true
	c.mu.Unlock()
	c.publish()
	defer func() {
		c.mu.Lock()
		c.checking = false
		c.lastRun = time.Now().UTC().Format(tsFormat)
		c.mu.Unlock()
		c.publish()
	}()

	cfgs, err := c.configs(ctx, 0)
	if err != nil {
		return err
	}
	eps, err := c.endpoints(ctx)
	if err != nil {
		return err
	}
	cache := map[int64][]Container{}
	cacheErr := map[int64]error{}
	for _, cfg := range cfgs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res := c.check(ctx, cfg, eps, cache, cacheErr)
		if err := c.save(ctx, cfg.ServiceID, res); err != nil {
			return err
		}
	}
	if c.Proxmox != nil {
		if targets := c.Proxmox(ctx); len(targets) > 0 {
			nodes := proxmox.FetchNodeUpdates(ctx, targets)
			c.mu.Lock()
			c.nodes, c.nodesAt = nodes, time.Now().UTC().Format(tsFormat)
			c.mu.Unlock()
		}
	}
	slog.Info("update check done", "services", len(cfgs))
	return nil
}

// CheckOne re-checks a single service.
func (c *Checker) CheckOne(ctx context.Context, serviceID int64) error {
	cfgs, err := c.configs(ctx, serviceID)
	if err != nil || len(cfgs) == 0 {
		return err
	}
	eps, err := c.endpoints(ctx)
	if err != nil {
		return err
	}
	res := c.check(ctx, cfgs[0], eps, map[int64][]Container{}, map[int64]error{})
	err = c.save(ctx, serviceID, res)
	c.publish()
	return err
}

func (c *Checker) configs(ctx context.Context, only int64) ([]Config, error) {
	q := `SELECT s.id, s.name, COALESCE(u.source, 'auto'), u.docker_endpoint_id, COALESCE(u.container, ''), COALESCE(u.github_repo, ''),
		COALESCE(u.helper_slug, ''), COALESCE(u.current_version, ''), COALESCE(u.ignored_version, ''),
		d.host_id, COALESCE(d.runtime, ''), COALESCE(d.container_name, '')
		FROM services s LEFT JOIN update_checks u ON u.service_id = s.id LEFT JOIN deployments d ON d.service_id = s.id
		WHERE s.archived_at IS NULL`
	args := []any{}
	if only > 0 {
		q += ` AND s.id = ?`
		args = append(args, only)
	}
	rows, err := c.DB.QueryContext(ctx, q+` ORDER BY s.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Config
	for rows.Next() {
		var cfg Config
		var ep, host sql.NullInt64
		if err := rows.Scan(&cfg.ServiceID, &cfg.Name, &cfg.Source, &ep, &cfg.Container, &cfg.GitHubRepo, &cfg.HelperSlug,
			&cfg.CurrentVersion, &cfg.IgnoredVersion, &host, &cfg.Runtime, &cfg.ContainerName); err != nil {
			return nil, err
		}
		if ep.Valid {
			cfg.DockerEndpointID = &ep.Int64
		}
		if host.Valid {
			cfg.HostID = &host.Int64
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func (c *Checker) endpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := c.DB.QueryContext(ctx, `SELECT id, name, url, host_id FROM docker_endpoints ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		var h sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &h); err != nil {
			return nil, err
		}
		if h.Valid {
			e.HostID = &h.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (c *Checker) save(ctx context.Context, id int64, r Result) error {
	_, err := c.DB.ExecContext(ctx, `INSERT INTO update_checks(service_id, used_source, status, running_version, latest_version, image, new_digest,
		latest_url, release_notes, published_at, resolved_repo, detail, error, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(service_id) DO UPDATE SET used_source = excluded.used_source, status = excluded.status,
		running_version = excluded.running_version, latest_version = excluded.latest_version, image = excluded.image,
		new_digest = excluded.new_digest, latest_url = excluded.latest_url, release_notes = excluded.release_notes,
		published_at = excluded.published_at, resolved_repo = excluded.resolved_repo, detail = excluded.detail,
		error = excluded.error, checked_at = excluded.checked_at`,
		id, r.UsedSource, r.Status, r.RunningVersion, r.LatestVersion, r.Image, r.NewDigest, r.LatestURL, r.Notes, r.Published,
		r.ResolvedRepo, r.Detail, r.Error, time.Now().UTC().Format(tsFormat))
	return err
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(nonSlugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// check works out one service's status (never returns an error: failures
// are reported in the result).
func (c *Checker) check(ctx context.Context, cfg Config, eps []Endpoint, cache map[int64][]Container, cacheErr map[int64]error) Result {
	switch cfg.Source {
	case "none":
		return Result{UsedSource: "none", Status: "untracked", Detail: "Not tracked"}
	case "github", "manual":
		return c.checkGitHub(ctx, cfg, cfg.GitHubRepo, "github")
	case "helper":
		return c.checkHelper(ctx, cfg)
	}
	// docker / auto
	ep, ctr, why := c.findContainer(ctx, cfg, eps, cache, cacheErr)
	if ctr != nil {
		return c.checkDocker(ctx, cfg, ep, *ctr)
	}
	if cfg.Source == "docker" {
		return Result{UsedSource: "docker", Status: "error", Error: why}
	}
	if cfg.GitHubRepo != "" {
		return c.checkGitHub(ctx, cfg, cfg.GitHubRepo, "github")
	}
	if cfg.HelperSlug != "" {
		return c.checkHelper(ctx, cfg)
	}
	detail := "Not linked to a source yet. Set one with Configure."
	if cfg.Runtime == "docker" && why != "" {
		detail = why
	}
	return Result{UsedSource: "", Status: "untracked", Detail: detail}
}

func (c *Checker) containersOf(ctx context.Context, ep Endpoint, cache map[int64][]Container, cacheErr map[int64]error) ([]Container, error) {
	if cs, ok := cache[ep.ID]; ok {
		return cs, cacheErr[ep.ID]
	}
	cs, err := c.Docker.Containers(ctx, ep.URL)
	cache[ep.ID], cacheErr[ep.ID] = cs, err
	return cs, err
}

// findContainer locates the service's container: on the chosen endpoint,
// else the endpoint linked to its host, else any endpoint.
func (c *Checker) findContainer(ctx context.Context, cfg Config, eps []Endpoint, cache map[int64][]Container, cacheErr map[int64]error) (Endpoint, *Container, string) {
	if len(eps) == 0 {
		return Endpoint{}, nil, "No Docker hosts connected yet (Settings → Docker hosts)."
	}
	names := []string{}
	for _, n := range []string{cfg.Container, cfg.ContainerName} {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		if cfg.Source != "docker" && cfg.Runtime != "docker" {
			return Endpoint{}, nil, ""
		}
		names = append(names, cfg.Name, slugify(cfg.Name))
	}
	var order []Endpoint
	for _, e := range eps {
		if cfg.DockerEndpointID != nil && e.ID == *cfg.DockerEndpointID {
			order = append([]Endpoint{e}, order...)
		} else if cfg.DockerEndpointID == nil {
			if cfg.HostID != nil && e.HostID != nil && *e.HostID == *cfg.HostID {
				order = append([]Endpoint{e}, order...)
			} else {
				order = append(order, e)
			}
		}
	}
	var errs []string
	for _, e := range order {
		cs, err := c.containersOf(ctx, e, cache, cacheErr)
		if err != nil {
			errs = append(errs, e.Name+": "+err.Error())
			continue
		}
		for i := range cs {
			for _, n := range names {
				if strings.EqualFold(cs[i].Name(), n) {
					return e, &cs[i], ""
				}
			}
		}
	}
	why := fmt.Sprintf("No container named %q found.", names[0])
	if len(errs) > 0 {
		why += " " + strings.Join(errs, "; ")
	}
	return Endpoint{}, nil, why
}

func (c *Checker) checkDocker(ctx context.Context, cfg Config, ep Endpoint, ctr Container) Result {
	r := Result{UsedSource: "docker", Status: "unknown", Detail: fmt.Sprintf("Container %s on %s", ctr.Name(), ep.Name)}
	img, err := c.Docker.Image(ctx, ep.URL, ctr.ImageID)
	if err != nil {
		r.Status, r.Error = "error", err.Error()
		return r
	}
	name := ctr.Image
	if strings.HasPrefix(name, "sha256:") && len(img.RepoTags) > 0 {
		name = img.RepoTags[0]
	}
	ref, err := ParseImage(name)
	if err != nil {
		r.Status, r.Error = "error", "can't tell which image this container runs"
		return r
	}
	r.Image = ref.String()
	r.RunningVersion = ref.Tag
	if v := img.Config.Labels["org.opencontainers.image.version"]; v != "" && !IsVersionTag(ref.Tag) {
		r.RunningVersion = v
	}
	if len(img.RepoDigests) == 0 {
		r.Detail += " · built locally (no registry digest)"
	}

	// Same tag, newer image?
	if digest, err := c.Registry.Digest(ctx, ref); err != nil {
		r.Error = err.Error()
	} else if len(img.RepoDigests) > 0 {
		have := false
		for _, rd := range img.RepoDigests {
			if strings.HasSuffix(rd, "@"+digest) {
				have = true
			}
		}
		r.NewDigest = !have
		if r.NewDigest && cfg.IgnoredVersion == "digest:"+digest {
			r.NewDigest = false
		}
	}
	// A newer version tag?
	if IsVersionTag(ref.Tag) {
		if tags, err := c.Registry.Tags(ctx, ref, 10); err == nil {
			r.LatestVersion = NewestTag(ref.Tag, tags)
		} else if r.Error == "" {
			r.Error = err.Error()
		}
	}
	// Release notes from GitHub when a repo is linked.
	if repo, ok := NormalizeRepo(cfg.GitHubRepo); ok {
		if rel, err := c.GitHub.Latest(ctx, repo); err == nil {
			r.LatestURL, r.Notes, r.Published, r.ResolvedRepo = rel.URL, rel.Notes, rel.Published, repo
			if r.LatestVersion == "" && compareLoose(rel.Version, r.RunningVersion) > 0 && IsVersionTag(CleanVersion(r.RunningVersion)) {
				r.LatestVersion = rel.Version
			}
		}
	}
	if r.LatestVersion != "" && r.LatestVersion == cfg.IgnoredVersion {
		r.LatestVersion = ""
		r.Detail += " · skipped " + cfg.IgnoredVersion
	}
	switch {
	case r.LatestVersion != "" || r.NewDigest:
		r.Status = "outdated"
		if r.LatestVersion == "" {
			r.LatestVersion = ref.Tag + " (newer image)"
		}
	case r.Error != "":
		r.Status = "error"
	default:
		r.Status = "current"
	}
	return r
}

func (c *Checker) checkGitHub(ctx context.Context, cfg Config, rawRepo, source string) Result {
	r := Result{UsedSource: source, Status: "unknown", RunningVersion: cfg.CurrentVersion}
	repo, ok := NormalizeRepo(rawRepo)
	if !ok {
		r.Status, r.Error = "error", "set a GitHub repo like owner/name"
		return r
	}
	r.ResolvedRepo = repo
	rel, err := c.GitHub.Latest(ctx, repo)
	if err != nil {
		r.Status, r.Error = "error", err.Error()
		return r
	}
	r.LatestVersion, r.LatestURL, r.Notes, r.Published = rel.Version, rel.URL, rel.Notes, rel.Published
	switch {
	case cfg.CurrentVersion == "":
		r.Detail = "Enter the installed version to compare."
	case rel.Version == cfg.IgnoredVersion || compareLoose(rel.Version, cfg.CurrentVersion) <= 0:
		r.Status = "current"
	default:
		r.Status = "outdated"
	}
	return r
}

func (c *Checker) checkHelper(ctx context.Context, cfg Config) Result {
	slug := cfg.HelperSlug
	if slug == "" {
		slug = slugify(cfg.Name)
	}
	repo := cfg.GitHubRepo
	if repo == "" {
		var err error
		if repo, err = c.Helper.Repo(ctx, slug); err != nil {
			return Result{UsedSource: "helper", Status: "error", Error: err.Error(), RunningVersion: cfg.CurrentVersion}
		}
	}
	r := c.checkGitHub(ctx, cfg, repo, "helper")
	if cfg.CurrentVersion == "" && r.Error == "" {
		r.Detail = fmt.Sprintf("Enter the installed version (in the LXC: cat /opt/%s_version.txt).", slug)
	}
	return r
}

// ── weekly digest ─────────────────────────────────────────────────────────

// Outdated is one row of the digest.
type Outdated struct {
	Name, Running, Latest string
}

func (c *Checker) outdated(ctx context.Context) ([]Outdated, error) {
	rows, err := c.DB.QueryContext(ctx, `SELECT s.name, u.running_version, u.latest_version FROM update_checks u
		JOIN services s ON s.id = u.service_id WHERE u.status = 'outdated' AND s.archived_at IS NULL ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outdated
	for rows.Next() {
		var o Outdated
		if rows.Scan(&o.Name, &o.Running, &o.Latest) == nil {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

// DigestMessage builds the weekly summary ("" title = nothing to send).
func DigestMessage(svcs []Outdated, nodes []proxmox.NodeUpdates) (title, body string) {
	var pkgNodes []string
	pkgs := 0
	for _, n := range nodes {
		if len(n.Packages) > 0 {
			pkgs += len(n.Packages)
			pkgNodes = append(pkgNodes, fmt.Sprintf("%s (%d)", n.Node, len(n.Packages)))
		}
	}
	if len(svcs) == 0 && pkgs == 0 {
		return "", ""
	}
	var b strings.Builder
	for i, s := range svcs {
		if i == 15 {
			fmt.Fprintf(&b, "…and %d more\n", len(svcs)-15)
			break
		}
		from := s.Running
		if from == "" {
			from = "?"
		}
		fmt.Fprintf(&b, "<b>%s</b> %s → %s\n", htmlEsc(s.Name), htmlEsc(from), htmlEsc(s.Latest))
	}
	if pkgs > 0 {
		sort.Strings(pkgNodes)
		fmt.Fprintf(&b, "Proxmox: %d package update%s on %s\n", pkgs, plural(pkgs), strings.Join(pkgNodes, ", "))
	}
	parts := []string{}
	if len(svcs) > 0 {
		parts = append(parts, fmt.Sprintf("%d service update%s", len(svcs), plural(len(svcs))))
	}
	if pkgs > 0 {
		parts = append(parts, "Proxmox packages")
	}
	return "⬆️ Weekly updates: " + strings.Join(parts, " + "), strings.TrimSpace(b.String())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// maybeDigest sends the digest once per week, on Monday from 09:00 local.
func (c *Checker) maybeDigest(ctx context.Context, now time.Time) {
	if c.Digest == nil || (c.DigestOn != nil && !c.DigestOn(ctx)) {
		return
	}
	if now.Weekday() != time.Monday || now.Hour() < 9 {
		return
	}
	week := now.Format("2006-01-02")
	var raw string
	_ = c.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'updates_last_digest'`).Scan(&raw)
	var last string
	_ = json.Unmarshal([]byte(raw), &last)
	if last == week {
		return
	}
	svcs, err := c.outdated(ctx)
	if err != nil {
		return
	}
	enc, _ := json.Marshal(week)
	_, _ = c.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES ('updates_last_digest', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(enc))
	if title, body := DigestMessage(svcs, c.State().Nodes); title != "" {
		c.Digest(ctx, title, body)
	}
}

var timeNow = time.Now

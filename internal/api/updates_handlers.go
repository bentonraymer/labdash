package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/updates"
)

type UpdateItem struct {
	ServiceID int64  `json:"serviceId"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	// Config
	Source           string `json:"source"`
	DockerEndpointID *int64 `json:"dockerEndpointId"`
	Container        string `json:"container"`
	GitHubRepo       string `json:"githubRepo"`
	HelperSlug       string `json:"helperSlug"`
	CurrentVersion   string `json:"currentVersion"`
	IgnoredVersion   string `json:"ignoredVersion"`
	// Hosting hints
	Runtime       string `json:"runtime"`
	ContainerName string `json:"containerName"`
	// Last result
	UsedSource     string  `json:"usedSource"`
	Status         string  `json:"status"`
	RunningVersion string  `json:"runningVersion"`
	LatestVersion  string  `json:"latestVersion"`
	Image          string  `json:"image"`
	NewDigest      bool    `json:"newDigest"`
	LatestURL      string  `json:"latestUrl"`
	ReleaseNotes   string  `json:"releaseNotes"`
	PublishedAt    string  `json:"publishedAt"`
	ResolvedRepo   string  `json:"resolvedRepo"`
	Detail         string  `json:"detail"`
	Error          string  `json:"error"`
	CheckedAt      *string `json:"checkedAt"`
}

type DockerEndpoint struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	HostID   *int64 `json:"hostId"`
	HostName string `json:"hostName"`
}

type githubSecret struct {
	Token string `json:"token"`
}

// GitHubToken is the optional token for release lookups.
func (s *Server) GitHubToken(ctx context.Context) string {
	var sec githubSecret
	if _, err := s.Integrations.Get(ctx, "github", &struct{}{}, &sec); err != nil {
		return ""
	}
	return sec.Token
}

// GET /api/updates
func (s *Server) handleListUpdates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id, s.name, s.icon, COALESCE(u.source, 'auto'), u.docker_endpoint_id, COALESCE(u.container, ''),
		COALESCE(u.github_repo, ''), COALESCE(u.helper_slug, ''), COALESCE(u.current_version, ''), COALESCE(u.ignored_version, ''),
		COALESCE(d.runtime, ''), COALESCE(d.container_name, ''),
		COALESCE(u.used_source, ''), CASE WHEN u.checked_at IS NULL THEN 'untracked' ELSE u.status END, COALESCE(u.running_version, ''), COALESCE(u.latest_version, ''),
		COALESCE(u.image, ''), COALESCE(u.new_digest, 0), COALESCE(u.latest_url, ''), COALESCE(u.release_notes, ''),
		COALESCE(u.published_at, ''), COALESCE(u.resolved_repo, ''), CASE WHEN u.checked_at IS NULL THEN 'Not checked yet.' ELSE u.detail END, COALESCE(u.error, ''), u.checked_at
		FROM services s LEFT JOIN update_checks u ON u.service_id = s.id LEFT JOIN deployments d ON d.service_id = s.id
		WHERE s.archived_at IS NULL ORDER BY s.name COLLATE NOCASE`)
	if err != nil {
		serverErr(w, err)
		return
	}
	items := []UpdateItem{}
	for rows.Next() {
		var it UpdateItem
		var ep sql.NullInt64
		var checked sql.NullString
		if err := rows.Scan(&it.ServiceID, &it.Name, &it.Icon, &it.Source, &ep, &it.Container, &it.GitHubRepo, &it.HelperSlug,
			&it.CurrentVersion, &it.IgnoredVersion, &it.Runtime, &it.ContainerName, &it.UsedSource, &it.Status, &it.RunningVersion,
			&it.LatestVersion, &it.Image, &it.NewDigest, &it.LatestURL, &it.ReleaseNotes, &it.PublishedAt, &it.ResolvedRepo,
			&it.Detail, &it.Error, &checked); err != nil {
			rows.Close()
			serverErr(w, err)
			return
		}
		if ep.Valid {
			it.DockerEndpointID = &ep.Int64
		}
		if checked.Valid {
			it.CheckedAt = &checked.String
		}
		items = append(items, it)
	}
	rows.Close()
	eps, err := s.listDockerEndpoints(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	out := map[string]any{"items": items, "endpoints": eps, "hasGithubToken": s.GitHubToken(ctx) != ""}
	if s.Updates != nil {
		out["state"] = s.Updates.State()
	}
	writeJSON(w, http.StatusOK, out)
}

// PUT /api/updates/{id}: how a service's updates are tracked.
func (s *Server) handlePutUpdateConfig(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in struct {
		Source           string  `json:"source"`
		DockerEndpointID *int64  `json:"dockerEndpointId"`
		Container        string  `json:"container"`
		GitHubRepo       string  `json:"githubRepo"`
		HelperSlug       string  `json:"helperSlug"`
		CurrentVersion   string  `json:"currentVersion"`
		IgnoredVersion   *string `json:"ignoredVersion"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Source == "" {
		in.Source = "auto"
	}
	if oneOf("auto", "docker", "github", "helper", "none")(in.Source) != nil {
		writeErr(w, http.StatusBadRequest, "unknown source")
		return
	}
	in.GitHubRepo = strings.TrimSpace(in.GitHubRepo)
	if in.GitHubRepo != "" {
		repo, ok := updates.NormalizeRepo(in.GitHubRepo)
		if !ok {
			writeErr(w, http.StatusBadRequest, "GitHub repo should look like owner/name")
			return
		}
		in.GitHubRepo = repo
	}
	if in.Source == "github" && in.GitHubRepo == "" {
		writeErr(w, http.StatusBadRequest, "enter the GitHub repo")
		return
	}
	for _, v := range []string{in.Container, in.HelperSlug, in.CurrentVersion} {
		if len(v) > 200 {
			writeErr(w, http.StatusBadRequest, "value too long")
			return
		}
	}
	var exists bool
	_ = s.DB.QueryRowContext(r.Context(), `SELECT 1 FROM services WHERE id = ?`, id).Scan(&exists)
	if !exists {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `INSERT INTO update_checks(service_id, source, docker_endpoint_id, container, github_repo, helper_slug, current_version, ignored_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE(?, ''))
		ON CONFLICT(service_id) DO UPDATE SET source = excluded.source, docker_endpoint_id = excluded.docker_endpoint_id,
		container = excluded.container, github_repo = excluded.github_repo, helper_slug = excluded.helper_slug,
		current_version = excluded.current_version, ignored_version = COALESCE(?, ignored_version)`,
		id, in.Source, in.DockerEndpointID, strings.TrimSpace(in.Container), in.GitHubRepo, strings.ToLower(strings.TrimSpace(in.HelperSlug)),
		strings.TrimSpace(in.CurrentVersion), in.IgnoredVersion, in.IgnoredVersion)
	if err != nil {
		serverErr(w, err)
		return
	}
	// Re-check right away so the page shows the effect.
	if s.Updates != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = s.Updates.CheckOne(ctx, id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/updates/check {serviceId?}
func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		writeErr(w, http.StatusServiceUnavailable, "update checks are unavailable")
		return
	}
	var in struct {
		ServiceID int64 `json:"serviceId"`
	}
	_ = readJSON(r, &in)
	if in.ServiceID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := s.Updates.CheckOne(ctx, in.ServiceID); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if s.Updates.State().Checking {
		writeErr(w, http.StatusConflict, "a check is already running")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = s.Updates.CheckAll(ctx)
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// PUT /api/updates/github-token {token} ("" removes it)
func (s *Server) handlePutGitHubToken(w http.ResponseWriter, r *http.Request) {
	var in githubSecret
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Token = strings.TrimSpace(in.Token)
	if len(in.Token) > 300 {
		writeErr(w, http.StatusBadRequest, "token too long")
		return
	}
	if err := s.Integrations.Put(r.Context(), "github", in.Token != "", struct{}{}, in); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── Docker endpoints ──────────────────────────────────────────────────────

func (s *Server) listDockerEndpoints(ctx context.Context) ([]DockerEndpoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id, e.name, e.url, e.host_id, COALESCE(h.name, '') FROM docker_endpoints e
		LEFT JOIN hosts h ON h.id = e.host_id ORDER BY e.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DockerEndpoint{}
	for rows.Next() {
		var e DockerEndpoint
		var h sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &h, &e.HostName); err != nil {
			return nil, err
		}
		if h.Valid {
			e.HostID = &h.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Server) handleListDockerEndpoints(w http.ResponseWriter, r *http.Request) {
	eps, err := s.listDockerEndpoints(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eps)
}

func (s *Server) handleSaveDockerEndpoint(w http.ResponseWriter, r *http.Request) {
	var in DockerEndpoint
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Name, in.URL = strings.TrimSpace(in.Name), strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if in.Name == "" || len(in.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "enter a name")
		return
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "URL should look like http://192.168.8.22:2375")
		return
	}
	if r.Method == http.MethodPost {
		res, err := s.DB.ExecContext(r.Context(), `INSERT INTO docker_endpoints(name, url, host_id) VALUES (?, ?, ?)`, in.Name, in.URL, in.HostID)
		if err != nil {
			serverErr(w, err)
			return
		}
		in.ID, _ = res.LastInsertId()
		writeJSON(w, http.StatusCreated, map[string]int64{"id": in.ID})
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE docker_endpoints SET name = ?, url = ?, host_id = ? WHERE id = ?`, in.Name, in.URL, in.HostID, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleDeleteDockerEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM docker_endpoints WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/docker-endpoints/test {url}: lists containers through the proxy.
func (s *Server) handleTestDockerEndpoint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cs, err := updates.NewDocker().Containers(ctx, in.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name())
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": len(cs), "names": names})
}

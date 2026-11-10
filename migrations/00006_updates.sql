-- +goose Up
-- Read-only Docker API endpoints (a docker-socket-proxy per Docker host).
CREATE TABLE docker_endpoints (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL,
    host_id    INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- How each service's updates are tracked, plus the latest result.
CREATE TABLE update_checks (
    service_id         INTEGER PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
    -- auto: Docker if the service's container can be found, else GitHub if a repo is set
    source             TEXT    NOT NULL DEFAULT 'auto'
                       CHECK (source IN ('auto','docker','github','helper','manual','none')),
    docker_endpoint_id INTEGER REFERENCES docker_endpoints(id) ON DELETE SET NULL,
    container          TEXT    NOT NULL DEFAULT '',
    github_repo        TEXT    NOT NULL DEFAULT '',
    helper_slug        TEXT    NOT NULL DEFAULT '',
    current_version    TEXT    NOT NULL DEFAULT '',  -- entered by hand
    ignored_version    TEXT    NOT NULL DEFAULT '',  -- "skip this version"
    -- results of the last check
    used_source        TEXT    NOT NULL DEFAULT '',
    status             TEXT    NOT NULL DEFAULT 'unknown'
                       CHECK (status IN ('current','outdated','unknown','error','untracked')),
    running_version    TEXT    NOT NULL DEFAULT '',
    latest_version     TEXT    NOT NULL DEFAULT '',
    image              TEXT    NOT NULL DEFAULT '',
    new_digest         INTEGER NOT NULL DEFAULT 0,   -- same tag, newer image
    latest_url         TEXT    NOT NULL DEFAULT '',
    release_notes      TEXT    NOT NULL DEFAULT '',
    published_at       TEXT    NOT NULL DEFAULT '',
    resolved_repo      TEXT    NOT NULL DEFAULT '',
    detail             TEXT    NOT NULL DEFAULT '',
    error              TEXT    NOT NULL DEFAULT '',
    checked_at         TEXT
);

-- +goose Down
DROP TABLE update_checks;
DROP TABLE docker_endpoints;

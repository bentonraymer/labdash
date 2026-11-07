-- +goose Up

-- ── System ────────────────────────────────────────────────────────────────
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at TEXT    NOT NULL
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Credentials for NPM / Proxmox / Pushover. `secret` is AES-GCM encrypted JSON.
CREATE TABLE integrations (
    kind       TEXT    PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 0,
    config     TEXT    NOT NULL DEFAULT '{}',
    secret     BLOB,
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- ── Launchpad ─────────────────────────────────────────────────────────────
CREATE TABLE sections (
    id        INTEGER PRIMARY KEY,
    name      TEXT    NOT NULL,
    icon      TEXT    NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0,
    collapsed INTEGER NOT NULL DEFAULT 0,
    tile_size TEXT    NOT NULL DEFAULT 'md' CHECK (tile_size IN ('sm','md','lg'))
);

-- Physical machines, Proxmox nodes, NAS, router, and VMs/LXCs that host
-- other things (e.g. a Docker VM). parent_id builds the topology tree.
CREATE TABLE hosts (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL DEFAULT 'server'
               CHECK (kind IN ('proxmox_node','server','nas','router','vm','lxc','device')),
    parent_id  INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    vmid       INTEGER,
    ip         TEXT    NOT NULL DEFAULT '',
    mac        TEXT    NOT NULL DEFAULT '',
    os         TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE services (
    id            INTEGER PRIMARY KEY,
    section_id    INTEGER REFERENCES sections(id) ON DELETE SET NULL,
    name          TEXT    NOT NULL,
    icon          TEXT    NOT NULL DEFAULT '',
    description   TEXT    NOT NULL DEFAULT '',
    position      INTEGER NOT NULL DEFAULT 0,
    dns_url       TEXT    NOT NULL DEFAULT '',
    direct_scheme TEXT    NOT NULL DEFAULT 'http',
    direct_ip     TEXT    NOT NULL DEFAULT '',
    direct_port   INTEGER,
    direct_path   TEXT    NOT NULL DEFAULT '',
    quick_links   TEXT    NOT NULL DEFAULT '[]',
    tags          TEXT    NOT NULL DEFAULT '[]',
    new_tab       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX services_section ON services(section_id, position);

-- Where a service runs. host_id is the machine it runs on (a Proxmox node for
-- a VM/LXC service, or a Docker VM for a container).
CREATE TABLE deployments (
    id             INTEGER PRIMARY KEY,
    service_id     INTEGER NOT NULL UNIQUE REFERENCES services(id) ON DELETE CASCADE,
    host_id        INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    runtime        TEXT    NOT NULL DEFAULT 'docker'
                   CHECK (runtime IN ('vm','lxc','docker','bare_metal','other')),
    vmid           INTEGER,
    container_name TEXT    NOT NULL DEFAULT '',
    proxmox_stats  INTEGER NOT NULL DEFAULT 0
);

-- ── Network / IPAM ────────────────────────────────────────────────────────
CREATE TABLE subnets (
    id         INTEGER PRIMARY KEY,
    cidr       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL DEFAULT '',
    vlan       INTEGER,
    gateway    TEXT NOT NULL DEFAULT '',
    dhcp_start TEXT NOT NULL DEFAULT '',
    dhcp_end   TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE ip_assignments (
    id         INTEGER PRIMARY KEY,
    ip         TEXT    NOT NULL UNIQUE,
    subnet_id  INTEGER REFERENCES subnets(id) ON DELETE SET NULL,
    kind       TEXT    NOT NULL DEFAULT 'static' CHECK (kind IN ('static','reserved','dhcp')),
    service_id INTEGER REFERENCES services(id) ON DELETE SET NULL,
    host_id    INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    label      TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE port_claims (
    id         INTEGER PRIMARY KEY,
    ip         TEXT    NOT NULL,
    port       INTEGER NOT NULL,
    proto      TEXT    NOT NULL DEFAULT 'tcp' CHECK (proto IN ('tcp','udp')),
    service_id INTEGER REFERENCES services(id) ON DELETE CASCADE,
    label      TEXT    NOT NULL DEFAULT '',
    UNIQUE (ip, port, proto)
);

-- ── Monitoring ────────────────────────────────────────────────────────────
CREATE TABLE monitors (
    id                   INTEGER PRIMARY KEY,
    service_id           INTEGER REFERENCES services(id) ON DELETE CASCADE,
    name                 TEXT    NOT NULL DEFAULT '',
    kind                 TEXT    NOT NULL DEFAULT 'http' CHECK (kind IN ('http','tcp','ping','dns')),
    target               TEXT    NOT NULL DEFAULT '',   -- '' = derive from service + access mode
    interval_s           INTEGER NOT NULL DEFAULT 60,
    timeout_s            INTEGER NOT NULL DEFAULT 10,
    retries              INTEGER NOT NULL DEFAULT 2,
    expected_status      TEXT    NOT NULL DEFAULT '200-399',
    keyword              TEXT    NOT NULL DEFAULT '',
    ignore_tls           INTEGER NOT NULL DEFAULT 1,
    enabled              INTEGER NOT NULL DEFAULT 1,
    muted                INTEGER NOT NULL DEFAULT 0,
    pushover_priority    INTEGER NOT NULL DEFAULT 0,
    pushover_sound       TEXT    NOT NULL DEFAULT '',
    status               TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','up','down','maintenance','paused')),
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_check_at        TEXT,
    last_change_at       TEXT,
    last_latency_ms      INTEGER,
    last_message         TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE heartbeats (
    id         INTEGER PRIMARY KEY,
    monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    ts         TEXT    NOT NULL,
    up         INTEGER NOT NULL,           -- 1 up, 0 down, 2 maintenance
    latency_ms INTEGER,
    message    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX heartbeats_monitor_ts ON heartbeats(monitor_id, ts);

CREATE TABLE heartbeat_hourly (
    monitor_id     INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    hour           TEXT    NOT NULL,
    up_count       INTEGER NOT NULL,
    total_count    INTEGER NOT NULL,
    avg_latency_ms INTEGER,
    PRIMARY KEY (monitor_id, hour)
);

CREATE TABLE incidents (
    id         INTEGER PRIMARY KEY,
    monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    started_at TEXT    NOT NULL,
    ended_at   TEXT,
    cause      TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE maintenance_windows (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    scope        TEXT    NOT NULL DEFAULT 'global' CHECK (scope IN ('global','service','host')),
    scope_id     INTEGER,
    starts_at    TEXT    NOT NULL,
    ends_at      TEXT    NOT NULL,
    recurrence   TEXT    NOT NULL DEFAULT '',   -- '' one-off, else cron expression for start
    duration_min INTEGER NOT NULL DEFAULT 60,
    enabled      INTEGER NOT NULL DEFAULT 1
);

-- ── Scanning ──────────────────────────────────────────────────────────────
CREATE TABLE scan_targets (
    id           INTEGER PRIMARY KEY,
    cidr         TEXT    NOT NULL,
    name         TEXT    NOT NULL DEFAULT '',
    schedule     TEXT    NOT NULL DEFAULT '0 */6 * * *',
    port_profile TEXT    NOT NULL DEFAULT 'top1000' CHECK (port_profile IN ('top100','top1000','full','custom')),
    custom_ports TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    last_run_at  TEXT
);

CREATE TABLE scans (
    id          INTEGER PRIMARY KEY,
    target_id   INTEGER REFERENCES scan_targets(id) ON DELETE SET NULL,
    cidr        TEXT    NOT NULL,
    started_at  TEXT    NOT NULL,
    finished_at TEXT,
    status      TEXT    NOT NULL DEFAULT 'running' CHECK (status IN ('running','done','failed')),
    hosts_up    INTEGER NOT NULL DEFAULT 0,
    error       TEXT    NOT NULL DEFAULT ''
);

-- A device is keyed by MAC when known (same L2), else by IP.
CREATE TABLE devices (
    id           INTEGER PRIMARY KEY,
    key          TEXT    NOT NULL UNIQUE,
    mac          TEXT    NOT NULL DEFAULT '',
    ip           TEXT    NOT NULL DEFAULT '',
    hostname     TEXT    NOT NULL DEFAULT '',
    vendor       TEXT    NOT NULL DEFAULT '',
    os_name      TEXT    NOT NULL DEFAULT '',
    os_family    TEXT    NOT NULL DEFAULT '',
    device_type  TEXT    NOT NULL DEFAULT '',
    is_up        INTEGER NOT NULL DEFAULT 0,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    ignored      INTEGER NOT NULL DEFAULT 0,
    host_id      INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    notes        TEXT    NOT NULL DEFAULT '',
    first_seen   TEXT    NOT NULL,
    last_seen    TEXT    NOT NULL
);
CREATE INDEX devices_ip ON devices(ip);

CREATE TABLE device_ports (
    id           INTEGER PRIMARY KEY,
    device_id    INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    port         INTEGER NOT NULL,
    proto        TEXT    NOT NULL DEFAULT 'tcp',
    state        TEXT    NOT NULL DEFAULT 'open',
    service_name TEXT    NOT NULL DEFAULT '',
    product      TEXT    NOT NULL DEFAULT '',
    version      TEXT    NOT NULL DEFAULT '',
    http_title   TEXT    NOT NULL DEFAULT '',
    favicon      TEXT    NOT NULL DEFAULT '',
    last_seen    TEXT    NOT NULL,
    UNIQUE (device_id, port, proto)
);

CREATE TABLE device_events (
    id        INTEGER PRIMARY KEY,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts        TEXT    NOT NULL,
    kind      TEXT    NOT NULL,
    detail    TEXT    NOT NULL DEFAULT ''
);

-- ── Docs ──────────────────────────────────────────────────────────────────
CREATE TABLE folders (
    id        INTEGER PRIMARY KEY,
    parent_id INTEGER REFERENCES folders(id) ON DELETE CASCADE,
    name      TEXT    NOT NULL,
    position  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE notes (
    id         INTEGER PRIMARY KEY,
    folder_id  INTEGER REFERENCES folders(id) ON DELETE SET NULL,
    service_id INTEGER REFERENCES services(id) ON DELETE SET NULL,
    title      TEXT    NOT NULL DEFAULT 'Untitled',
    body       TEXT    NOT NULL DEFAULT '',
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE note_tags (
    note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag     TEXT    NOT NULL COLLATE NOCASE,
    PRIMARY KEY (note_id, tag)
);

CREATE TABLE note_versions (
    id         INTEGER PRIMARY KEY,
    note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE attachments (
    id         INTEGER PRIMARY KEY,
    note_id    INTEGER REFERENCES notes(id) ON DELETE SET NULL,
    filename   TEXT    NOT NULL,
    mime       TEXT    NOT NULL,
    size       INTEGER NOT NULL,
    path       TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE VIRTUAL TABLE notes_fts USING fts5(title, body, content='notes', content_rowid='id');

-- +goose StatementBegin
CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_au AFTER UPDATE OF title, body ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
    INSERT INTO notes_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
-- +goose StatementEnd

-- Spotlight frecency
CREATE TABLE spotlight_usage (
    kind      TEXT    NOT NULL,
    ref       TEXT    NOT NULL,
    count     INTEGER NOT NULL DEFAULT 0,
    last_used TEXT    NOT NULL,
    PRIMARY KEY (kind, ref)
);

-- +goose Down
SELECT 'irreversible';

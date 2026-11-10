-- +goose Up
-- CPE identifiers from -sV, used to match CVEs by version.
ALTER TABLE device_ports ADD COLUMN cpe TEXT NOT NULL DEFAULT '';

-- Vulnerability scans run on their own (weekly) schedule per scan target.
ALTER TABLE scan_targets ADD COLUMN vuln_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE scan_targets ADD COLUMN vuln_schedule TEXT NOT NULL DEFAULT '0 4 * * 0';
ALTER TABLE scan_targets ADD COLUMN vuln_last_run TEXT;

CREATE TABLE vuln_findings (
    id          INTEGER PRIMARY KEY,
    device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip          TEXT    NOT NULL,
    port        INTEGER NOT NULL DEFAULT 0,
    source      TEXT    NOT NULL CHECK (source IN ('cve','nuclei','config')),
    key         TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    severity    TEXT    NOT NULL,
    cvss        REAL    NOT NULL DEFAULT 0,
    kev         INTEGER NOT NULL DEFAULT 0,
    description TEXT    NOT NULL DEFAULT '',
    evidence    TEXT    NOT NULL DEFAULT '',
    refs        TEXT    NOT NULL DEFAULT '[]',
    fixed_in    TEXT    NOT NULL DEFAULT '',
    confidence  TEXT    NOT NULL DEFAULT 'high',
    status      TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open','fixed','accepted','ignored')),
    note        TEXT    NOT NULL DEFAULT '',
    first_seen  TEXT    NOT NULL,
    last_seen   TEXT    NOT NULL,
    resolved_at TEXT,
    UNIQUE (device_id, port, source, key)
);
CREATE INDEX vuln_findings_status ON vuln_findings(status, severity);

CREATE TABLE vuln_scans (
    id          INTEGER PRIMARY KEY,
    target_id   INTEGER REFERENCES scan_targets(id) ON DELETE SET NULL,
    cidr        TEXT    NOT NULL,
    started_at  TEXT    NOT NULL,
    finished_at TEXT,
    status      TEXT    NOT NULL DEFAULT 'running' CHECK (status IN ('running','done','failed')),
    devices     INTEGER NOT NULL DEFAULT 0,
    new_count   INTEGER NOT NULL DEFAULT 0,
    open_count  INTEGER NOT NULL DEFAULT 0,
    notes       TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE vuln_scans;
DROP TABLE vuln_findings;
ALTER TABLE scan_targets DROP COLUMN vuln_last_run;
ALTER TABLE scan_targets DROP COLUMN vuln_schedule;
ALTER TABLE scan_targets DROP COLUMN vuln_enabled;
ALTER TABLE device_ports DROP COLUMN cpe;

-- +goose Up
-- Open vulnerability counts per day (last write of the day wins).
CREATE TABLE vuln_history (
    day        TEXT    PRIMARY KEY,  -- YYYY-MM-DD, server-local
    critical   INTEGER NOT NULL DEFAULT 0,
    high       INTEGER NOT NULL DEFAULT 0,
    medium     INTEGER NOT NULL DEFAULT 0,
    low        INTEGER NOT NULL DEFAULT 0,
    info       INTEGER NOT NULL DEFAULT 0,
    kev        INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT    NOT NULL
);

-- +goose Down
DROP TABLE vuln_history;

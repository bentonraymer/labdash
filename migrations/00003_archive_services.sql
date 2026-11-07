-- +goose Up
-- Archived services keep their docs/history but leave the dashboard,
-- monitoring, IP/port usage and Spotlight.
ALTER TABLE services ADD COLUMN archived_at TEXT;

-- +goose Down
ALTER TABLE services DROP COLUMN archived_at;

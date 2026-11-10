-- +goose Up
-- A service can run under another service (containers under Dockge, a site
-- under its web server). Children without their own hosting inherit it.
ALTER TABLE services ADD COLUMN parent_id INTEGER REFERENCES services(id) ON DELETE SET NULL;
CREATE INDEX services_parent ON services(parent_id);

-- +goose Down
DROP INDEX services_parent;
ALTER TABLE services DROP COLUMN parent_id;

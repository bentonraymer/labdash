-- +goose Up
-- A friendly name you give a device (sticks to its MAC across IP changes),
-- and the name the GL.iNet router reports for it.
ALTER TABLE devices ADD COLUMN custom_name TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN router_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN router_name;
ALTER TABLE devices DROP COLUMN custom_name;

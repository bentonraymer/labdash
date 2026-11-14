-- +goose Up
-- Reminders: one-off, fixed calendar (every N units from an anchor), or
-- N units after the last completion. Sent by Pushover and re-sent daily
-- until marked done.
CREATE TABLE reminders (
    id            INTEGER PRIMARY KEY,
    title         TEXT    NOT NULL,
    notes         TEXT    NOT NULL DEFAULT '',
    kind          TEXT    NOT NULL DEFAULT 'once' CHECK (kind IN ('once','calendar','after_done')),
    every_n       INTEGER NOT NULL DEFAULT 1,
    unit          TEXT    NOT NULL DEFAULT 'month' CHECK (unit IN ('day','week','month','quarter','year')),
    anchor_at     TEXT    NOT NULL,          -- first due time (also sets the time of day)
    due_at        TEXT    NOT NULL,          -- current/next occurrence
    enabled       INTEGER NOT NULL DEFAULT 1,
    snoozed_until TEXT,
    last_sent_at  TEXT,
    last_done_at  TEXT,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX reminders_due ON reminders(enabled, due_at);

CREATE TABLE reminder_events (
    id          INTEGER PRIMARY KEY,
    reminder_id INTEGER NOT NULL REFERENCES reminders(id) ON DELETE CASCADE,
    ts          TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('sent','done','skipped','snoozed')),
    due_at      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX reminder_events_by ON reminder_events(reminder_id, ts);

-- Todos: tasks and goals (goals contain tasks), grouped into lists.
CREATE TABLE todo_lists (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE todos (
    id               INTEGER PRIMARY KEY,
    list_id          INTEGER REFERENCES todo_lists(id) ON DELETE SET NULL,
    parent_id        INTEGER REFERENCES todos(id) ON DELETE SET NULL,  -- the goal a task belongs to
    kind             TEXT    NOT NULL DEFAULT 'task' CHECK (kind IN ('task','goal')),
    title            TEXT    NOT NULL,
    notes            TEXT    NOT NULL DEFAULT '',
    priority         INTEGER NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 4),
    status           TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open','doing','done')),
    due_date         TEXT,                    -- YYYY-MM-DD
    remind           INTEGER NOT NULL DEFAULT 0,
    position         REAL    NOT NULL DEFAULT 0,
    service_id       INTEGER REFERENCES services(id) ON DELETE SET NULL,
    host_id          INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
    created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    completed_at     TEXT,
    last_reminded_at TEXT
);
CREATE INDEX todos_list ON todos(list_id, status);
CREATE INDEX todos_parent ON todos(parent_id);

CREATE TABLE todo_checklist (
    id       INTEGER PRIMARY KEY,
    todo_id  INTEGER NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    text     TEXT    NOT NULL,
    done     INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX todo_checklist_by ON todo_checklist(todo_id, position);

-- +goose Down
DROP TABLE todo_checklist;
DROP TABLE todos;
DROP TABLE todo_lists;
DROP TABLE reminder_events;
DROP TABLE reminders;

package store

const walPragma = `PRAGMA journal_mode = WAL;`

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS worlds (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL UNIQUE,
    tick INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    world_id   INTEGER NOT NULL REFERENCES worlds(id) ON DELETE CASCADE,
    id         INTEGER NOT NULL,
    tick       INTEGER NOT NULL,
    kind       TEXT    NOT NULL,
    agent_id   INTEGER NOT NULL DEFAULT 0,
    entity_id  INTEGER NOT NULL DEFAULT 0,
    rel_id     INTEGER NOT NULL DEFAULT 0,
    type_label TEXT    NOT NULL DEFAULT '',
    props      TEXT    NOT NULL DEFAULT '',
    from_id    INTEGER NOT NULL DEFAULT 0,
    to_id      INTEGER NOT NULL DEFAULT 0,
    rel_kind   TEXT    NOT NULL DEFAULT '',
    cascade    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (world_id, id)
);

CREATE INDEX IF NOT EXISTS events_by_tick ON events (world_id, tick, id);

CREATE TABLE IF NOT EXISTS memory_records (
    world_name   TEXT    NOT NULL,
    agent_id     INTEGER NOT NULL,
    id           INTEGER NOT NULL,
    kind         TEXT    NOT NULL,
    content      TEXT    NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT 0,
    created_wall TEXT    NOT NULL DEFAULT '',
    importance   REAL    NOT NULL DEFAULT 0,
    last_access  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (world_name, agent_id, id)
);

CREATE INDEX IF NOT EXISTS memory_by_world ON memory_records (world_name);
`

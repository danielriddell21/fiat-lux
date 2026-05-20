package store

// schema is the SQLite DDL for the fiat-lux store. The store is
// event-sourced: worlds carry only their name and current tick, and
// every World mutation is appended to the events table. Live state
// (live entities, live relationships) is reconstructed at load time
// by replaying events through world.ApplyEventForLoad. Agents'
// memory streams live in their own table so they survive a session
// boundary independently of the world replay.
const schema = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

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
`

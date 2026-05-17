// Package store persists worlds, entities, relationships, memories,
// events, and per-tick token usage to SQLite via modernc.org/sqlite
// (pure Go, no CGO). The event history is what makes replay exact.
package store

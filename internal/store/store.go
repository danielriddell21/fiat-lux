package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

var ErrWorldNotFound = errors.New("store: world not found")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	driver, dsn, err := resolveDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open (%s): %w", driver, err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: apply schema (%s): %w", driver, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("store: close db: %w", err)
	}
	return nil
}

func (s *Store) Save(ctx context.Context, w *world.World) error {
	if w == nil {
		return errors.New("store: cannot save nil world")
	}
	events := w.Events()
	name := w.Name()
	tick := w.Tick()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO worlds (name, tick) VALUES (?, ?)
        ON CONFLICT(name) DO UPDATE SET tick = excluded.tick
    `, name, tick); err != nil {
		return fmt.Errorf("store: upsert world: %w", err)
	}

	var worldID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM worlds WHERE name = ?`, name).
		Scan(&worldID); err != nil {
		return fmt.Errorf("store: lookup world id: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE world_id = ?`, worldID); err != nil {
		return fmt.Errorf("store: clear events: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO events (
            world_id, id, tick, kind, agent_id, entity_id, rel_id,
            type_label, props, from_id, to_id, rel_kind, cascade
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `)
	if err != nil {
		return fmt.Errorf("store: prepare event insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range events {
		propsJSON, err := marshalProps(e.Props)
		if err != nil {
			return fmt.Errorf("store: marshal event %d props: %w", e.ID, err)
		}
		cascade := 0
		if e.Cascade {
			cascade = 1
		}
		if _, err := stmt.ExecContext(ctx,
			worldID, int64(e.ID), int64(e.Tick), string(e.Kind),
			int64(e.Agent), int64(e.EntityID), int64(e.RelID),
			e.TypeLabel, propsJSON,
			int64(e.From), int64(e.To), e.RelKind, cascade,
		); err != nil {
			return fmt.Errorf("store: insert event %d: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

func (s *Store) Load(ctx context.Context, name string) (*world.World, error) {
	var worldID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM worlds WHERE name = ?`, name).
		Scan(&worldID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrWorldNotFound, name)
		}
		return nil, fmt.Errorf("store: lookup world: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, tick, kind, agent_id, entity_id, rel_id,
               type_label, props, from_id, to_id, rel_kind, cascade
        FROM events
        WHERE world_id = ?
        ORDER BY id ASC
    `, worldID)
	if err != nil {
		return nil, fmt.Errorf("store: query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	w, err := world.New(name)
	if err != nil {
		return nil, fmt.Errorf("store: construct world: %w", err)
	}

	for rows.Next() {
		var (
			id, tick, agent, ent, rel    int64
			from, to                     int64
			cascadeInt                   int64
			kind, typeLabel, props, rKnd string
		)
		if err := rows.Scan(
			&id, &tick, &kind, &agent, &ent, &rel,
			&typeLabel, &props, &from, &to, &rKnd, &cascadeInt,
		); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}

		propsMap, err := unmarshalProps(props)
		if err != nil {
			return nil, fmt.Errorf("store: unmarshal event %d props: %w", id, err)
		}

		ev := world.Event{
			ID:        world.EventID(id),
			Tick:      world.Tick(tick),
			Kind:      world.EventKind(kind),
			Agent:     world.AgentID(agent),
			EntityID:  world.EntityID(ent),
			RelID:     world.RelationshipID(rel),
			TypeLabel: typeLabel,
			Props:     propsMap,
			From:      world.EntityID(from),
			To:        world.EntityID(to),
			RelKind:   rKnd,
			Cascade:   cascadeInt != 0,
		}
		if err := w.ApplyEventForLoad(ev); err != nil {
			return nil, fmt.Errorf("store: replay event %d: %w", id, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate events: %w", err)
	}
	return w, nil
}

func (s *Store) ListWorlds(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM worlds ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list worlds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("store: scan world name: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate world names: %w", err)
	}
	return out, nil
}

func marshalProps(p world.Properties) (string, error) {
	if p == nil {
		return "", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("store: marshal props: %w", err)
	}
	return string(b), nil
}

func unmarshalProps(s string) (world.Properties, error) {
	if s == "" {
		return nil, nil
	}
	var p world.Properties
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, fmt.Errorf("store: unmarshal props: %w", err)
	}
	return p, nil
}

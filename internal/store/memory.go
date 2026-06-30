package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func (s *Store) SaveMemoryRecords(ctx context.Context, worldName string, records []memory.Record) error {
	if worldName == "" {
		return errors.New("store: SaveMemoryRecords needs a non-empty world name")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin memory tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_records WHERE world_name = ?`, worldName); err != nil {
		return fmt.Errorf("store: clear memory: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO memory_records (
            world_name, agent_id, id, kind, content,
            created_at, created_wall, importance, last_access
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `)
	if err != nil {
		return fmt.Errorf("store: prepare memory insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range records {
		wall := ""
		if !r.CreatedWall.IsZero() {
			wall = r.CreatedWall.UTC().Format(time.RFC3339Nano)
		}
		if _, err := stmt.ExecContext(ctx,
			worldName, int64(r.AgentID), int64(r.ID),
			string(r.Kind), r.Content,
			int64(r.CreatedAt), wall,
			r.Importance, int64(r.LastAccessTick),
		); err != nil {
			return fmt.Errorf("store: insert memory record %d: %w", r.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit memory: %w", err)
	}
	return nil
}

func (s *Store) LoadMemoryRecords(ctx context.Context, worldName string) ([]memory.Record, error) {
	if worldName == "" {
		return nil, errors.New("store: LoadMemoryRecords needs a non-empty world name")
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT agent_id, id, kind, content,
               created_at, created_wall, importance, last_access
        FROM memory_records
        WHERE world_name = ?
        ORDER BY agent_id ASC, id ASC
    `, worldName)
	if err != nil {
		return nil, fmt.Errorf("store: query memory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []memory.Record
	for rows.Next() {
		var (
			agentID, id, createdAt, lastAccess int64
			kind, content, wall                string
			importance                         float64
		)
		if err := rows.Scan(&agentID, &id, &kind, &content,
			&createdAt, &wall, &importance, &lastAccess); err != nil {
			return nil, fmt.Errorf("store: scan memory: %w", err)
		}
		rec := memory.Record{
			ID:             memory.RecordID(id),
			AgentID:        world.AgentID(agentID),
			Kind:           memory.Kind(kind),
			Content:        content,
			CreatedAt:      world.Tick(createdAt),
			Importance:     importance,
			LastAccessTick: world.Tick(lastAccess),
		}
		if wall != "" {
			if t, err := time.Parse(time.RFC3339Nano, wall); err == nil {
				rec.CreatedWall = t
			}
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate memory: %w", err)
	}
	return out, nil
}

func MemoryRecordsByAgent(records []memory.Record) map[world.AgentID][]memory.Record {
	out := make(map[world.AgentID][]memory.Record)
	for _, r := range records {
		out[r.AgentID] = append(out[r.AgentID], r)
	}
	for k := range out {
		sort.SliceStable(out[k], func(i, j int) bool {
			return out[k][i].ID < out[k][j].ID
		})
	}
	return out
}

var _ = (*sql.DB)(nil)

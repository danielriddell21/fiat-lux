package store

import (
	"context"
	"testing"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func mustStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleRecords() []memory.Record {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	return []memory.Record{
		{ID: 1, AgentID: 1, Kind: memory.KindObservation, Content: "saw a planet form", CreatedAt: 1, CreatedWall: now, Importance: 3.0},
		{ID: 2, AgentID: 1, Kind: memory.KindAction, Content: "Create planet", CreatedAt: 1, CreatedWall: now, Importance: 5.0, LastAccessTick: 2},
		{ID: 3, AgentID: 1, Kind: memory.KindReflection, Content: "the world wants depth", CreatedAt: 5, CreatedWall: now, Importance: 8.5},
		{ID: 1, AgentID: 7, Kind: memory.KindObservation, Content: "watcher saw the creator", CreatedAt: 5, CreatedWall: now, Importance: 4.0},
	}
}

func TestMemory_RoundTrip(t *testing.T) {
	t.Parallel()
	s := mustStore(t)
	ctx := context.Background()
	want := sampleRecords()

	if err := s.SaveMemoryRecords(ctx, "kosmos", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryRecords(ctx, "kosmos")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, g := range got {
		w := want[i]
		if g.ID != w.ID || g.AgentID != w.AgentID || g.Kind != w.Kind ||
			g.Content != w.Content || g.CreatedAt != w.CreatedAt ||
			g.Importance != w.Importance || g.LastAccessTick != w.LastAccessTick {
			t.Errorf("record[%d] mismatch:\n  got  %+v\n  want %+v", i, g, w)
		}
		if !g.CreatedWall.Equal(w.CreatedWall) {
			t.Errorf("record[%d] CreatedWall = %v, want %v", i, g.CreatedWall, w.CreatedWall)
		}
	}
}

func TestMemory_SaveReplacesPriorRecords(t *testing.T) {
	t.Parallel()
	s := mustStore(t)
	ctx := context.Background()
	first := sampleRecords()
	if err := s.SaveMemoryRecords(ctx, "kosmos", first); err != nil {
		t.Fatal(err)
	}
	// Save a shorter set; the deleted ones must not linger.
	second := []memory.Record{
		{ID: 1, AgentID: 1, Kind: memory.KindReflection, Content: "starting over", CreatedAt: 100, Importance: 9.0},
	}
	if err := s.SaveMemoryRecords(ctx, "kosmos", second); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryRecords(ctx, "kosmos")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len after re-save = %d, want 1", len(got))
	}
	if got[0].Content != "starting over" {
		t.Errorf("content = %q, want %q", got[0].Content, "starting over")
	}
}

func TestMemory_WorldIsolation(t *testing.T) {
	t.Parallel()
	s := mustStore(t)
	ctx := context.Background()
	if err := s.SaveMemoryRecords(ctx, "kosmos", []memory.Record{{ID: 1, AgentID: 1, Kind: memory.KindThought, Content: "k"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryRecords(ctx, "anima", []memory.Record{{ID: 1, AgentID: 1, Kind: memory.KindThought, Content: "a"}}); err != nil {
		t.Fatal(err)
	}
	k, _ := s.LoadMemoryRecords(ctx, "kosmos")
	a, _ := s.LoadMemoryRecords(ctx, "anima")
	if len(k) != 1 || k[0].Content != "k" {
		t.Errorf("kosmos = %+v", k)
	}
	if len(a) != 1 || a[0].Content != "a" {
		t.Errorf("anima = %+v", a)
	}
}

func TestMemory_RejectsEmptyWorld(t *testing.T) {
	t.Parallel()
	s := mustStore(t)
	ctx := context.Background()
	if err := s.SaveMemoryRecords(ctx, "", nil); err == nil {
		t.Errorf("expected error for empty world")
	}
	if _, err := s.LoadMemoryRecords(ctx, ""); err == nil {
		t.Errorf("expected error for empty world")
	}
}

func TestMemory_GroupByAgent(t *testing.T) {
	t.Parallel()
	grouped := MemoryRecordsByAgent(sampleRecords())
	if len(grouped) != 2 {
		t.Fatalf("want 2 agents, got %d", len(grouped))
	}
	if len(grouped[world.AgentID(1)]) != 3 {
		t.Errorf("agent 1 = %d records, want 3", len(grouped[world.AgentID(1)]))
	}
	if len(grouped[world.AgentID(7)]) != 1 {
		t.Errorf("agent 7 = %d records, want 1", len(grouped[world.AgentID(7)]))
	}
}

func TestStream_Restore(t *testing.T) {
	t.Parallel()
	stream := memory.New(memory.DefaultWeights(), 100)
	stream.Restore(sampleRecords())
	if got := stream.Len(); got != 4 {
		t.Errorf("Len after restore = %d, want 4", got)
	}
	// Adding a new record should pick up where restored IDs left off
	// (highest restored ID was 3 → next should be 4).
	r, err := stream.Add(context.Background(), world.AgentID(1), memory.KindThought, "new", world.Tick(10), memory.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 4 {
		t.Errorf("next record ID = %d, want 4 (restored max was 3)", r.ID)
	}
}

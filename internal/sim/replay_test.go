package sim

import (
	"context"
	"errors"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

func captureEvents(t *testing.T) []world.Event {
	t.Helper()
	w, _ := world.New("kosmos")
	a, err := w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Create(world.NoAgent, "ocean", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AdvanceTick()
	if err := w.Modify(world.NoAgent, b, world.Properties{"depth": float64(5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Relate(world.NoAgent, b, a, "part of"); err != nil {
		t.Fatal(err)
	}
	_ = w.Destroy(world.NoAgent, a)
	return w.Events()
}

func TestReplay_AppliesEventsInOrder(t *testing.T) {
	t.Parallel()
	events := captureEvents(t)
	if len(events) == 0 {
		t.Fatal("no events captured")
	}

	r, err := NewReplay("kosmos", events)
	if err != nil {
		t.Fatal(err)
	}

	// Step until done.
	steps := 0
	for !r.Done() {
		res, err := r.Step(context.Background())
		if err != nil {
			t.Fatalf("step %d: %v", steps, err)
		}
		if res.ToolName == "" {
			t.Errorf("step %d returned empty ToolName", steps)
		}
		steps++
	}
	if steps != len(events) {
		t.Errorf("steps = %d, want %d", steps, len(events))
	}

	// One more step should return ErrReplayDone.
	_, err = r.Step(context.Background())
	if !errors.Is(err, ErrReplayDone) {
		t.Errorf("err = %v, want ErrReplayDone", err)
	}
}

func TestReplay_RebuildsWorldExactly(t *testing.T) {
	t.Parallel()
	events := captureEvents(t)

	r, err := NewReplay("kosmos", events)
	if err != nil {
		t.Fatal(err)
	}
	for !r.Done() {
		if _, err := r.Step(context.Background()); err != nil {
			t.Fatalf("step: %v", err)
		}
	}

	// After replay the world's IDs/tick should match what the
	// original sequence produced.
	if got := r.World.NextEntityID(); got != 3 {
		t.Errorf("NextEntityID = %d, want 3 (2 created + 1)", got)
	}
	if got := r.World.NextRelationshipID(); got != 2 {
		t.Errorf("NextRelationshipID = %d, want 2", got)
	}
	if r.World.EntityCount() != 1 {
		t.Errorf("live EntityCount = %d, want 1 (ocean survives)", r.World.EntityCount())
	}
}

func TestReplay_ProgressTracksApplied(t *testing.T) {
	t.Parallel()
	events := captureEvents(t)
	r, _ := NewReplay("kosmos", events)

	applied, total := r.Progress()
	if applied != 0 || total != len(events) {
		t.Errorf("initial progress = %d/%d, want 0/%d", applied, total, len(events))
	}
	_, _ = r.Step(context.Background())
	applied, _ = r.Progress()
	if applied != 1 {
		t.Errorf("after one step, applied = %d, want 1", applied)
	}
}

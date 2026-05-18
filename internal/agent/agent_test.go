package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type nullBrain struct{}

func (nullBrain) Decide(_ context.Context, _ brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	return brain.Decision{}, errors.New("not used")
}
func (nullBrain) Close() error     { return nil }
func (nullBrain) Model() string    { return "null" }
func (nullBrain) Provider() string { return "null" }

func TestNew_RequiresBrainAndTools(t *testing.T) {
	t.Parallel()
	if _, err := New(1, "x", "p", nil, tools.Default()); err == nil {
		t.Errorf("expected error with nil brain")
	}
	if _, err := New(1, "x", "p", nullBrain{}, nil); err == nil {
		t.Errorf("expected error with nil tools")
	}
}

func TestBuildPerception_EmptyWorld(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	a, err := New(1, "x", "p", nullBrain{}, tools.Default())
	if err != nil {
		t.Fatal(err)
	}
	p := a.BuildPerception(w, nil, nil)
	if p.EntityCount != 0 {
		t.Errorf("EntityCount = %d, want 0", p.EntityCount)
	}
	if len(p.AliveEntities) != 0 {
		t.Errorf("AliveEntities len = %d, want 0", len(p.AliveEntities))
	}
	if len(p.RecentEvents) != 0 {
		t.Errorf("RecentEvents len = %d, want 0", len(p.RecentEvents))
	}
}

func TestBuildPerception_PopulatedWorld(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	a, _ := New(1, "x", "p", nullBrain{}, tools.Default())

	planet, _ := w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	ocean, _ := w.Create(world.NoAgent, "ocean", nil)
	_, _ = w.Relate(world.NoAgent, ocean, planet, "part of")

	p := a.BuildPerception(w, nil, nil)
	if p.EntityCount != 2 {
		t.Errorf("EntityCount = %d, want 2", p.EntityCount)
	}
	if len(p.AliveEntities) != 2 {
		t.Errorf("AliveEntities len = %d, want 2", len(p.AliveEntities))
	}
	if p.AliveEntities[0].TypeLabel != "planet" {
		t.Errorf("first entity type = %q, want planet", p.AliveEntities[0].TypeLabel)
	}
	if len(p.AliveRelationships) != 1 {
		t.Errorf("AliveRelationships len = %d, want 1", len(p.AliveRelationships))
	}
	if p.AliveRelationships[0].Kind != "part of" {
		t.Errorf("relationship kind = %q, want 'part of'", p.AliveRelationships[0].Kind)
	}
	if len(p.RecentEvents) != 3 {
		t.Errorf("RecentEvents = %d, want 3 (create create relate)", len(p.RecentEvents))
	}
}

func TestRecentEvents_FiltersBySeenWatermark(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	a, _ := New(1, "x", "p", nullBrain{}, tools.Default())

	_, _ = w.Create(world.NoAgent, "x", nil)
	_, _ = w.Create(world.NoAgent, "y", nil)
	a.MarkSeen(w)

	if got := a.BuildPerception(w, nil, nil); len(got.RecentEvents) != 0 {
		t.Errorf("RecentEvents after MarkSeen = %d, want 0", len(got.RecentEvents))
	}

	_, _ = w.Create(world.NoAgent, "z", nil)
	p := a.BuildPerception(w, nil, nil)
	if len(p.RecentEvents) != 1 {
		t.Errorf("RecentEvents = %d, want 1 (the new create)", len(p.RecentEvents))
	}
	if p.RecentEvents[0].Kind != "create" {
		t.Errorf("recent event kind = %q, want create", p.RecentEvents[0].Kind)
	}
}

func TestMarkSeen_NoopOnEmpty(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	a, _ := New(1, "x", "p", nullBrain{}, tools.Default())
	a.MarkSeen(w) // must not panic
	if a.SeenEventID != 0 {
		t.Errorf("SeenEventID = %d, want 0", a.SeenEventID)
	}
}

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

// hierarchy builds a "contains" tree to exercise the frontier and
// focus computations. Returns the root planet, the continent, the
// two forest leaves, and the world it created them in.
func hierarchy(t *testing.T) (*world.World, *Agent, world.EntityID, world.EntityID, world.EntityID, world.EntityID) {
	t.Helper()
	w, _ := world.New("kosmos")
	a, _ := New(1, "x", "p", nullBrain{}, tools.Default())
	planet, _ := w.Create(world.NoAgent, "planet", nil)
	continent, _ := w.Create(world.NoAgent, "continent", nil)
	forest1, _ := w.Create(world.NoAgent, "forest", world.Properties{"name": "north"})
	forest2, _ := w.Create(world.NoAgent, "forest", world.Properties{"name": "south"})
	if _, err := w.Relate(world.NoAgent, planet, continent, "contains"); err != nil {
		t.Fatalf("relate planet->continent: %v", err)
	}
	if _, err := w.Relate(world.NoAgent, continent, forest1, "contains"); err != nil {
		t.Fatalf("relate continent->forest1: %v", err)
	}
	if _, err := w.Relate(world.NoAgent, continent, forest2, "contains"); err != nil {
		t.Fatalf("relate continent->forest2: %v", err)
	}
	return w, a, planet, continent, forest1, forest2
}

func TestBuildPerception_FrontierLeaves(t *testing.T) {
	t.Parallel()
	w, a, planet, continent, forest1, forest2 := hierarchy(t)
	// An orphan idea is not part of the containment graph; it must
	// NOT appear in the frontier.
	_, _ = w.Create(world.NoAgent, "idea", nil)

	p := a.BuildPerception(w, nil, nil)
	ids := map[uint64]int{}
	for _, l := range p.Frontier.Leaves {
		ids[l.EntityID] = l.Depth
	}
	if ids[uint64(forest1)] != 2 || ids[uint64(forest2)] != 2 {
		t.Errorf("forest leaves missing or wrong depth; got %+v", p.Frontier.Leaves)
	}
	if _, ok := ids[uint64(planet)]; ok {
		t.Errorf("planet appeared as a leaf; it has children")
	}
	if _, ok := ids[uint64(continent)]; ok {
		t.Errorf("continent appeared as a leaf; it has children")
	}
	for _, l := range p.Frontier.Leaves {
		if l.TypeLabel == "idea" {
			t.Errorf("orphan idea appeared as a leaf: %+v", l)
		}
	}
}

func TestBuildPerception_DeepestPath(t *testing.T) {
	t.Parallel()
	w, a, planet, continent, forest1, _ := hierarchy(t)
	tree, _ := w.Create(world.NoAgent, "tree", nil)
	if _, err := w.Relate(world.NoAgent, forest1, tree, "contains"); err != nil {
		t.Fatalf("relate forest1->tree: %v", err)
	}

	p := a.BuildPerception(w, nil, nil)
	if len(p.Frontier.DeepestPath) != 4 {
		t.Fatalf("DeepestPath length = %d, want 4; got %+v",
			len(p.Frontier.DeepestPath), p.Frontier.DeepestPath)
	}
	want := []uint64{uint64(planet), uint64(continent), uint64(forest1), uint64(tree)}
	for i, step := range p.Frontier.DeepestPath {
		if step.EntityID != want[i] {
			t.Errorf("step[%d] EntityID = %d, want %d (chain=%+v)",
				i, step.EntityID, want[i], p.Frontier.DeepestPath)
		}
	}
}

func TestBuildPerception_FocusSubtree(t *testing.T) {
	t.Parallel()
	w, a, planet, continent, forest1, forest2 := hierarchy(t)

	a.Focus = continent
	a.FocusTurnsLeft = 5

	p := a.BuildPerception(w, nil, nil)
	if p.Focus == nil {
		t.Fatal("Focus is nil; expected populated FocusView")
	}
	if p.Focus.EntityID != uint64(continent) {
		t.Errorf("Focus.EntityID = %d, want %d", p.Focus.EntityID, continent)
	}
	subtreeIDs := map[uint64]bool{}
	for _, e := range p.Focus.Subtree {
		subtreeIDs[e.ID] = true
	}
	if !subtreeIDs[uint64(forest1)] || !subtreeIDs[uint64(forest2)] {
		t.Errorf("focus subtree missing forests: %+v", p.Focus.Subtree)
	}
	if subtreeIDs[uint64(planet)] {
		t.Errorf("focus subtree contained the planet (ancestor); should not")
	}
	// Global entity list still contains the planet — subtree is additive.
	var hasPlanet bool
	for _, e := range p.AliveEntities {
		if e.ID == uint64(planet) {
			hasPlanet = true
		}
	}
	if !hasPlanet {
		t.Errorf("planet missing from global AliveEntities")
	}
	if p.Focus.TurnsLeft != 5 {
		t.Errorf("Focus.TurnsLeft = %d, want 5", p.Focus.TurnsLeft)
	}
}

func TestBuildPerception_FocusExpires(t *testing.T) {
	t.Parallel()
	w, a, _, continent, _, _ := hierarchy(t)
	a.Focus = continent
	a.FocusTurnsLeft = 1

	p1 := a.BuildPerception(w, nil, nil)
	if p1.Focus == nil {
		t.Fatal("first BuildPerception lost focus immediately")
	}

	p2 := a.BuildPerception(w, nil, nil)
	if p2.Focus != nil {
		t.Errorf("Focus did not expire; got %+v", p2.Focus)
	}
	if a.Focus != 0 || a.FocusTurnsLeft != 0 {
		t.Errorf("agent focus state not cleared: id=%d turns=%d", a.Focus, a.FocusTurnsLeft)
	}
}

func TestBuildPerception_FocusClearsOnDestroy(t *testing.T) {
	t.Parallel()
	w, a, _, continent, _, _ := hierarchy(t)
	a.Focus = continent
	a.FocusTurnsLeft = 5

	if err := w.Destroy(world.NoAgent, continent); err != nil {
		t.Fatalf("destroy continent: %v", err)
	}
	p := a.BuildPerception(w, nil, nil)
	if p.Focus != nil {
		t.Errorf("Focus survived destroy of focused entity")
	}
	if a.Focus != 0 {
		t.Errorf("agent.Focus = %d, want 0", a.Focus)
	}
}

func TestBuildPerception_SuggestionDedupe(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	a, _ := New(1, "x", "p", nullBrain{}, tools.Default())

	parent, _ := w.Create(world.NoAgent, "continent", nil)
	for i := 0; i < 8; i++ {
		child, _ := w.Create(world.NoAgent, "forest", nil)
		if _, err := w.Relate(world.NoAgent, parent, child, "contains"); err != nil {
			t.Fatalf("relate: %v", err)
		}
	}

	// Tick 1: suggestion fires.
	_ = w.AdvanceTick()
	p1 := a.BuildPerception(w, nil, nil)
	if len(p1.Suggestions) == 0 {
		t.Fatalf("tick 1: expected at least one suggestion")
	}
	found := false
	for _, s := range p1.Suggestions {
		if s.EntityID == uint64(parent) {
			found = true
			if s.ChildCount != 8 {
				t.Errorf("ChildCount = %d, want 8", s.ChildCount)
			}
		}
	}
	if !found {
		t.Errorf("parent not surfaced in suggestions: %+v", p1.Suggestions)
	}

	// Ticks 2-10: same parent should be suppressed by cooldown.
	for i := 2; i <= 10; i++ {
		_ = w.AdvanceTick()
		p := a.BuildPerception(w, nil, nil)
		for _, s := range p.Suggestions {
			if s.EntityID == uint64(parent) {
				t.Errorf("tick %d: parent re-suggested before cooldown elapsed", i)
			}
		}
	}

	// Tick 11: cooldown has elapsed (last=1 + 10), so the suggestion
	// can fire again.
	_ = w.AdvanceTick()
	p11 := a.BuildPerception(w, nil, nil)
	again := false
	for _, s := range p11.Suggestions {
		if s.EntityID == uint64(parent) {
			again = true
		}
	}
	if !again {
		t.Errorf("tick 11: parent not re-suggested after cooldown")
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

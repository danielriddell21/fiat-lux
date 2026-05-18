package stub

import (
	"context"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/tools"
)

func TestDecide_EmptyWorldUsesCreateableTool(t *testing.T) {
	t.Parallel()
	b := New(1, 2, tools.Default())
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.ToolCall == nil {
		t.Fatalf("expected a tool call; got skip")
	}
	// On an empty world only Create / Observe / Reflect / Wait can
	// produce valid args. Whatever the stub picks must come from
	// that set.
	allowed := map[string]bool{"Create": true, "Observe": true, "Reflect": true, "Wait": true}
	if !allowed[d.ToolCall.Name] {
		t.Errorf("stub chose %q on empty world; allowed: %v", d.ToolCall.Name, allowed)
	}
}

func TestDecide_NeverPicksSpawnAgent(t *testing.T) {
	t.Parallel()
	b := New(7, 7, tools.Default())
	p := brain.Perception{
		AliveEntities: []brain.EntityView{{ID: 1, TypeLabel: "x"}, {ID: 2, TypeLabel: "y"}},
	}
	// Run many times - SpawnAgent should never be chosen.
	for i := 0; i < 200; i++ {
		d, err := b.Decide(context.Background(), p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d.ToolCall != nil && d.ToolCall.Name == "SpawnAgent" {
			t.Fatalf("stub picked SpawnAgent (disabled)")
		}
	}
}

func TestDecide_LeansHeavilyOnCreate(t *testing.T) {
	t.Parallel()
	b := New(13, 17, tools.Default())
	p := brain.Perception{
		AliveEntities: []brain.EntityView{
			{ID: 1, TypeLabel: "thing"},
			{ID: 2, TypeLabel: "thing"},
		},
		AliveRelationships: []brain.RelationshipView{
			{ID: 1, From: 1, To: 2, Kind: "knows"},
		},
	}
	counts := map[string]int{}
	const n = 300
	for i := 0; i < n; i++ {
		d, err := b.Decide(context.Background(), p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d.ToolCall != nil {
			counts[d.ToolCall.Name]++
		}
	}
	// Create should dominate by a noticeable margin.
	if counts["Create"] < n/3 {
		t.Errorf("Create chosen %d/%d times; expected at least %d. counts=%v", counts["Create"], n, n/3, counts)
	}
}

func TestDecide_ReturnsThought(t *testing.T) {
	t.Parallel()
	b := New(0, 0, tools.Default())
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Thought == "" {
		t.Errorf("expected non-empty thought")
	}
}

func TestClose_NoOp(t *testing.T) {
	t.Parallel()
	if err := New(0, 0, tools.Default()).Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

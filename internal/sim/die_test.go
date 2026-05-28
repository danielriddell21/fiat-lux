package sim

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func TestDie_RecordsEventAndMarksDead(t *testing.T) {
	t.Parallel()
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Die", Args: json.RawMessage(`{"final":"goodbye"}`)}},
	}}
	w, _ := world.New("kosmos")
	s, err := New(Options{World: w, Brain: br})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.ToolErr != nil {
		t.Fatalf("Die: %v", res.ToolErr)
	}
	var dieEvent *world.Event
	for i, ev := range w.Events() {
		if ev.Kind == world.EventDie {
			dieEvent = &w.Events()[i]
		}
	}
	if dieEvent == nil {
		t.Fatalf("no EventDie in log: %v", w.Events())
	}
	if dieEvent.Props["final"] != "goodbye" {
		t.Errorf("final props = %v, want 'goodbye'", dieEvent.Props["final"])
	}
	// The agent's entity should be soft-destroyed.
	ents := w.Entities()
	if len(ents) != 0 {
		t.Errorf("expected agent entity to be soft-destroyed; alive = %v", ents)
	}
}

func TestDie_SkippedAfterDeath(t *testing.T) {
	t.Parallel()
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Die"}},
		{ToolCall: &brain.ToolCall{Name: "Wait", Args: json.RawMessage(`{"n":1}`)}},
	}}
	w, _ := world.New("kosmos")
	s, _ := New(Options{World: w, Brain: br})
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("expected post-death step to be skipped")
	}
}

func TestDie_ChildrenInheritDigest(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	creator := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: json.RawMessage(`{
			"name": "heir",
			"system_prompt": "carry forward",
			"brain_config": {"provider": "stub"}
		}`)}},
		{ToolCall: &brain.ToolCall{Name: "Die", Args: json.RawMessage(`{"final":"the torch passes"}`)}},
	}}
	heir := &scriptedBrain{decisions: []brain.Decision{{Thought: "ok"}}}
	factory := func(_ context.Context, _ string) (brain.Brain, error) { return heir, nil }
	s, err := New(Options{World: w, Brain: creator, BrainFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	// Tick 1: creator spawns heir.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Pre-populate the creator's memory so the digest has something
	// non-empty to copy.
	creatorAgent := s.Agents()[0]
	tick := w.Tick()
	if _, err := creatorAgent.Memory.Add(context.Background(), creatorAgent.EntityID,
		memory.KindThought, "I have witnessed wonders", tick, memory.AddOptions{}); err != nil {
		t.Fatal(err)
	}
	// Tick 2: heir's turn (thought "ok"). Tick 3: creator dies.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	heirAgent := s.Agents()[1]
	if heirAgent.Memory == nil {
		t.Fatal("heir has no memory stream")
	}
	var inherited int
	for _, r := range heirAgent.Memory.All() {
		if r.Kind == memory.KindInheritance {
			inherited++
			if !strings.Contains(r.Content, "from") {
				t.Errorf("inheritance content lacks attribution: %q", r.Content)
			}
		}
	}
	if inherited == 0 {
		t.Errorf("heir did not receive any inheritance records: %+v", heirAgent.Memory.All())
	}
}

func TestDie_OnlyDirectChildrenInherit(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	creator := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: json.RawMessage(`{
			"name": "child",
			"system_prompt": "child",
			"brain_config": {"provider": "stub"}
		}`)}},
		{ToolCall: &brain.ToolCall{Name: "Die"}},
	}}
	// The child spawns a grandchild on its turn.
	child := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: json.RawMessage(`{
			"name": "grandchild",
			"system_prompt": "grand",
			"brain_config": {"provider": "stub"}
		}`)}},
	}}
	grand := &scriptedBrain{decisions: []brain.Decision{{Thought: "ok"}}}
	called := 0
	factory := func(_ context.Context, _ string) (brain.Brain, error) {
		called++
		if called == 1 {
			return child, nil
		}
		return grand, nil
	}
	s, err := New(Options{World: w, Brain: creator, BrainFactory: factory, MaxAgents: 8, MaxSpawnDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Tick 1: creator spawns child. Tick 2: child spawns grandchild.
	// Tick 3: grandchild thinks. Tick 4: creator dies.
	_, _ = s.Step(context.Background())
	_, _ = s.Step(context.Background())
	_, _ = s.Step(context.Background())
	tick := w.Tick()
	creatorAgent := s.Agents()[0]
	_, _ = creatorAgent.Memory.Add(context.Background(), creatorAgent.EntityID,
		memory.KindThought, "I birthed worlds", tick, memory.AddOptions{})
	_, _ = s.Step(context.Background())

	// Only the direct child should have inherited records.
	childAgent := s.Agents()[1]
	grandAgent := s.Agents()[2]
	if !hasInheritance(childAgent) {
		t.Errorf("direct child missed inheritance")
	}
	if hasInheritance(grandAgent) {
		t.Errorf("grandchild incorrectly inherited from grandparent")
	}
}

func hasInheritance(ag *agent.Agent) bool {
	if ag == nil || ag.Memory == nil {
		return false
	}
	for _, r := range ag.Memory.All() {
		if r.Kind == memory.KindInheritance {
			return true
		}
	}
	return false
}

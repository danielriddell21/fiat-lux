package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/brain/stub"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func mustNew(t *testing.T, br brain.Brain) *Sim {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{World: w, Brain: br})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNew_SpawnsRootAgentAsEntity(t *testing.T) {
	t.Parallel()
	s := mustNew(t, stub.New(1, 2, tools.Default()))
	if s.World.EntityCount() != 1 {
		t.Fatalf("expected 1 entity (the creator), got %d", s.World.EntityCount())
	}
	ents := s.World.Entities()
	if ents[0].TypeLabel != "agent" {
		t.Errorf("type_label = %q, want agent", ents[0].TypeLabel)
	}
	if ents[0].Properties["name"] != "the creator" {
		t.Errorf("name = %v", ents[0].Properties["name"])
	}
	if s.Agent().EntityID != ents[0].ID {
		t.Errorf("agent EntityID = %d, want %d", s.Agent().EntityID, ents[0].ID)
	}
}

func TestNew_RequiresWorldAndBrain(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Errorf("expected error with nil world")
	}
	w, _ := world.New("kosmos")
	_, err := New(Options{World: w})
	if !errors.Is(err, ErrNoBrain) {
		t.Errorf("err = %v, want ErrNoBrain", err)
	}
}

func TestStep_ProducesActionAndAdvancesTick(t *testing.T) {
	t.Parallel()
	s := mustNew(t, stub.New(1, 2, tools.Default()))
	before := s.World.Tick()
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Tick != before+1 {
		t.Errorf("res.Tick = %d, want %d", res.Tick, before+1)
	}
	if res.Skipped {
		t.Errorf("first step should not be skipped")
	}
	if res.ToolName == "" {
		t.Errorf("expected a tool call; got skip")
	}
}

func TestStep_RepeatedRunsFillTheWorld(t *testing.T) {
	t.Parallel()
	s := mustNew(t, stub.New(42, 99, tools.Default()))
	for i := 0; i < 30; i++ {
		if _, err := s.Step(context.Background()); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	// The stub favours Create; after 30 ticks there should be many
	// entities beyond the root creator (1).
	if s.World.EntityCount() < 5 {
		t.Errorf("after 30 steps, EntityCount = %d; expected the stub to have created several entities", s.World.EntityCount())
	}
}

// fixedBrain returns a pre-set decision regardless of input. Useful
// to drive specific step outcomes in tests.
type fixedBrain struct {
	decisions []brain.Decision
	idx       int
}

func (f *fixedBrain) Decide(_ context.Context, _ brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	d := f.decisions[f.idx%len(f.decisions)]
	f.idx++
	return d, nil
}
func (f *fixedBrain) Close() error     { return nil }
func (f *fixedBrain) Model() string    { return "fixed" }
func (f *fixedBrain) Provider() string { return "fixed" }

func TestStep_ObserverCalled(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fb := &fixedBrain{
		decisions: []brain.Decision{{
			Thought: "let there be light",
			ToolCall: &brain.ToolCall{
				Name: "Create",
				Args: json.RawMessage(`{"type_label":"light"}`),
			},
		}},
	}
	var calls []StepResult
	s, err := New(Options{
		World: w, Brain: fb,
		Observer: func(r StepResult) { calls = append(calls, r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("observer called %d times, want 1", len(calls))
	}
	if calls[0].ToolName != "Create" || calls[0].Thought != "let there be light" {
		t.Errorf("observer received unexpected result: %+v", calls[0])
	}
}

func TestStep_RecordsToolError(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fb := &fixedBrain{
		decisions: []brain.Decision{{
			ToolCall: &brain.ToolCall{
				Name: "Modify",
				Args: json.RawMessage(`{"entity_id":9999,"properties_patch":{"x":1}}`),
			},
		}},
	}
	s, err := New(Options{World: w, Brain: fb})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.ToolErr == nil {
		t.Errorf("expected ToolErr from Modify on unknown entity; got nil")
	}
}

func TestStep_SkipsWhenNothingHappened(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	// fixedBrain alternates between a no-op decision (no tool call)
	// and a tool call. After the no-op step, the next step should
	// still run because the agent's mark-seen advances past the
	// tick_start event.
	fb := &fixedBrain{
		decisions: []brain.Decision{
			{Thought: "thinking", ToolCall: nil}, // no tool call - only emits tick_start
		},
	}
	s, err := New(Options{World: w, Brain: fb})
	if err != nil {
		t.Fatal(err)
	}
	// First step: nothing happens beyond tick_start. Agent then
	// MarkSeens that event.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second step: shouldSkip - high event ID == agent.SeenEventID -
	// returns true. Skip is emitted.
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("expected skipped step; got %+v", res)
	}
}

// memoryInspectingBrain captures the Perception the sim passed in
// so we can assert what memories were threaded through.
type memoryInspectingBrain struct {
	seen brain.Perception
}

func (m *memoryInspectingBrain) Decide(_ context.Context, p brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	m.seen = p
	return brain.Decision{
		ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"x"}`)},
	}, nil
}
func (m *memoryInspectingBrain) Model() string    { return "test" }
func (m *memoryInspectingBrain) Provider() string { return "test" }
func (m *memoryInspectingBrain) Close() error     { return nil }

func TestStep_RecordsAndInjectsMemories(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &memoryInspectingBrain{}
	s, err := New(Options{World: w, Brain: br})
	if err != nil {
		t.Fatal(err)
	}
	// First step: no prior memories.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(br.seen.Memories) != 0 {
		t.Errorf("first perception memories = %d, want 0", len(br.seen.Memories))
	}
	if got := s.Agent().Memory.Len(); got != 2 {
		t.Errorf("after one step, memory len = %d, want 2 (action + outcome)", got)
	}

	// Second step: prior memories should now be retrievable.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(br.seen.Memories) == 0 {
		t.Errorf("second perception memories = 0; expected the prior action / outcome to be retrieved")
	}
}

// reflectingBrain returns a fixed action tool call on the first
// N calls and a multi-line reflection thought afterwards. The
// reflection pass uses Decide with empty tools and reads .Thought.
type reflectingBrain struct {
	createCalls int
}

func (r *reflectingBrain) Decide(_ context.Context, _ brain.Perception, tools []brain.ToolDef) (brain.Decision, error) {
	if len(tools) == 0 {
		// Reflection call.
		return brain.Decision{
			Thought: "- the world is taking shape\n- there are many things now",
		}, nil
	}
	r.createCalls++
	return brain.Decision{
		ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"thing"}`)},
	}, nil
}
func (r *reflectingBrain) Model() string    { return "test" }
func (r *reflectingBrain) Provider() string { return "test" }
func (r *reflectingBrain) Close() error     { return nil }

func TestStep_ReflectsOnInterval(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &reflectingBrain{}
	s, err := New(Options{
		World:           w,
		Brain:           br,
		ReflectInterval: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Drive 5 steps. The 5th should trigger a reflection.
	var lastWithReflection StepResult
	for i := 0; i < 5; i++ {
		res, err := s.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Reflections) > 0 {
			lastWithReflection = res
		}
	}
	if len(lastWithReflection.Reflections) == 0 {
		t.Fatalf("expected reflections by tick 5; none surfaced")
	}
	if lastWithReflection.Tick != 5 {
		t.Errorf("reflection fired at tick %d, want 5", lastWithReflection.Tick)
	}

	// Memory should now contain reflection records.
	var reflections int
	for _, r := range s.Agent().Memory.All() {
		if r.Kind == "reflection" {
			reflections++
		}
	}
	if reflections == 0 {
		t.Errorf("no reflection records persisted to memory stream")
	}
}

// scriptedBrain returns decisions from a queue, looping when
// exhausted. Records the last perception it received.
type scriptedBrain struct {
	decisions []brain.Decision
	idx       int
	seen      brain.Perception
}

func (s *scriptedBrain) Decide(_ context.Context, p brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	s.seen = p
	d := s.decisions[s.idx%len(s.decisions)]
	s.idx++
	return d, nil
}
func (s *scriptedBrain) Model() string    { return "scripted" }
func (s *scriptedBrain) Provider() string { return "scripted" }
func (s *scriptedBrain) Close() error     { return nil }

func TestSpawnAgent_RegistersChildAndTicksAlongside(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	creatorBrain := &scriptedBrain{
		decisions: []brain.Decision{
			{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: json.RawMessage(`{
				"name": "observer",
				"system_prompt": "watch and reflect",
				"brain_config": {"provider": "stub"},
				"granted_tools": ["Observe", "Reflect", "Wait"]
			}`)}},
			{ToolCall: &brain.ToolCall{Name: "Wait", Args: json.RawMessage(`{"n":1}`)}},
		},
	}
	childBrain := &scriptedBrain{
		decisions: []brain.Decision{
			{ToolCall: &brain.ToolCall{Name: "Wait", Args: json.RawMessage(`{"n":1}`)}},
		},
	}
	factory := func(_ context.Context, spec string) (brain.Brain, error) {
		if spec != "stub" {
			return nil, fmt.Errorf("unexpected spec %q", spec)
		}
		return childBrain, nil
	}

	s, err := New(Options{
		World:        w,
		Brain:        creatorBrain,
		BrainFactory: factory,
		MaxAgents:    8,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: creator spawns observer.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	agents := s.Agents()
	if len(agents) != 2 {
		t.Fatalf("agents after spawn = %d, want 2", len(agents))
	}
	if agents[1].Name != "observer" {
		t.Errorf("child name = %q, want observer", agents[1].Name)
	}
	if agents[1].SpawnDepth != 1 {
		t.Errorf("child SpawnDepth = %d, want 1", agents[1].SpawnDepth)
	}

	// Step 2: observer's turn now.
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.AgentName != "observer" {
		t.Errorf("step 2 agent = %q, want observer", res.AgentName)
	}
	if childBrain.idx == 0 {
		t.Errorf("child brain never invoked")
	}
}

func TestSpawnAgent_HonoursMaxAgents(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	spawnCall := &brain.ToolCall{Name: "SpawnAgent", Args: json.RawMessage(`{
		"name": "child",
		"system_prompt": "x",
		"brain_config": {"provider": "stub"}
	}`)}
	br := &scriptedBrain{decisions: []brain.Decision{{ToolCall: spawnCall}}}
	child := &scriptedBrain{decisions: []brain.Decision{{Thought: "ok"}}}
	factory := func(_ context.Context, _ string) (brain.Brain, error) { return child, nil }
	s, err := New(Options{
		World: w, Brain: br, BrainFactory: factory, MaxAgents: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Tick 1: creator spawns -> agents=2 (root+child). OK.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Agents()) != 2 {
		t.Fatalf("after 1 spawn agents=%d", len(s.Agents()))
	}
	// Tick 2: child does nothing significant.
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Tick 3: creator tries to spawn again -> should hit cap.
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.ToolErr == nil {
		t.Fatalf("expected ErrAgentCap; got nil")
	}
	if !errors.Is(res.ToolErr, ErrAgentCap) {
		t.Errorf("ToolErr = %v, want ErrAgentCap", res.ToolErr)
	}
}

func TestSpeak_DeliversToOtherAgents(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	spawnArgs := json.RawMessage(`{"name":"listener","system_prompt":"x","brain_config":{"provider":"stub"}}`)
	creatorBrain := &scriptedBrain{
		decisions: []brain.Decision{
			{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: spawnArgs}},
			{ToolCall: &brain.ToolCall{Name: "Speak", Args: json.RawMessage(`{"content":"hello listener"}`)}},
		},
	}
	listenerBrain := &scriptedBrain{
		decisions: []brain.Decision{
			{ToolCall: &brain.ToolCall{Name: "Wait", Args: json.RawMessage(`{"n":1}`)}},
		},
	}
	factory := func(_ context.Context, _ string) (brain.Brain, error) { return listenerBrain, nil }
	s, err := New(Options{
		World: w, Brain: creatorBrain, BrainFactory: factory,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1: creator spawns listener
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 2: listener takes its turn (no speak heard yet)
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(listenerBrain.seen.Heard) != 0 {
		t.Errorf("listener perception had Heard before any Speak: %+v", listenerBrain.seen.Heard)
	}
	// 3: creator speaks
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 4: listener's next turn drains the inbox into perception
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(listenerBrain.seen.Heard) == 0 {
		t.Fatalf("listener did not receive the Speak; Heard=%v", listenerBrain.seen.Heard)
	}
	if listenerBrain.seen.Heard[0].Content != "hello listener" {
		t.Errorf("Heard content = %q", listenerBrain.seen.Heard[0].Content)
	}
	if listenerBrain.seen.Heard[0].SpeakerName != "the creator" {
		t.Errorf("Speaker = %q", listenerBrain.seen.Heard[0].SpeakerName)
	}
}

func TestSpawnAgent_HonoursSpawnDepth(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	spawnArgs := json.RawMessage(`{"name":"child","system_prompt":"x","brain_config":{"provider":"stub"}}`)
	br := &scriptedBrain{decisions: []brain.Decision{{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: spawnArgs}}}}
	child := &scriptedBrain{decisions: []brain.Decision{{ToolCall: &brain.ToolCall{Name: "SpawnAgent", Args: spawnArgs}}}}
	grandchild := &scriptedBrain{decisions: []brain.Decision{{Thought: "ok"}}}
	var calls int
	factory := func(_ context.Context, _ string) (brain.Brain, error) {
		calls++
		if calls == 1 {
			return child, nil
		}
		return grandchild, nil
	}
	s, err := New(Options{
		World: w, Brain: br, BrainFactory: factory,
		MaxSpawnDepth: 1, // creator depth 0 -> child depth 1, but child trying to spawn at depth 2 should be blocked
		MaxAgents:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1: creator spawns child OK (depth 1 <= 1)
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Agents()) != 2 {
		t.Fatalf("agents = %d after first spawn, want 2", len(s.Agents()))
	}
	// 2: child tries to spawn grandchild -> ErrSpawnDepth
	res, err := s.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(res.ToolErr, ErrSpawnDepth) {
		t.Errorf("ToolErr = %v, want ErrSpawnDepth", res.ToolErr)
	}
}

func TestClose_ClosesBrain(t *testing.T) {
	t.Parallel()
	s := mustNew(t, stub.New(0, 0, tools.Default()))
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

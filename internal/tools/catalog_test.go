package tools

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func newRng() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

func mustWorld(t *testing.T) *world.World {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestDefaultRegistry_HasAllNamedTools(t *testing.T) {
	t.Parallel()
	reg := Default()
	for _, name := range []string{"Create", "Modify", "Destroy", "Relate", "Unrelate", "Observe", "Reflect", "SpawnAgent", "Speak", "Wait"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing tool %q", name)
		}
	}
	if got := len(reg.All()); got != 10 {
		t.Errorf("registry size = %d, want 10", got)
	}
}

func TestRegistry_DefsMatchTools(t *testing.T) {
	t.Parallel()
	reg := Default()
	defs := reg.Defs()
	if len(defs) != len(reg.All()) {
		t.Fatalf("len(defs)=%d, len(All)=%d", len(defs), len(reg.All()))
	}
	for _, d := range defs {
		if d.Schema == nil {
			t.Errorf("tool %q has nil schema", d.Name)
		}
	}
}

func TestRegistry_Filter(t *testing.T) {
	t.Parallel()
	reg := Default()
	sub, err := reg.Filter([]string{"Create", "Wait"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sub.All()); got != 2 {
		t.Errorf("filtered size = %d, want 2", got)
	}
	if _, err := reg.Filter([]string{"Bogus"}); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("err = %v, want ErrUnknownTool", err)
	}
	// Empty filter returns all tools.
	all, err := reg.Filter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(all.All()); got != len(reg.All()) {
		t.Errorf("empty filter returned %d tools, want %d", got, len(reg.All()))
	}
}

func TestCreate_ApplyHappy(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	tool := Create()
	args, _ := json.Marshal(createArgs{TypeLabel: "planet", Properties: map[string]any{"name": "Erith"}})
	out, err := tool.Apply(w, world.AgentID(1), args)
	if err != nil {
		t.Fatal(err)
	}
	if w.EntityCount() != 1 {
		t.Errorf("EntityCount = %d, want 1", w.EntityCount())
	}
	if out == "" {
		t.Errorf("Apply returned empty result string")
	}
}

func TestCreate_ApplyBadArgs(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	if _, err := Create().Apply(w, 1, json.RawMessage(`not json`)); err == nil {
		t.Errorf("expected error for malformed JSON args")
	}
	if _, err := Create().Apply(w, 1, json.RawMessage(`{"type_label":""}`)); err == nil {
		t.Errorf("expected error for empty type_label")
	}
}

func TestModify_FailsOnEmptyWorld(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	args, _ := json.Marshal(modifyArgs{EntityID: 99, PropertiesPatch: map[string]any{"x": 1}})
	if _, err := Modify().Apply(w, 1, args); err == nil {
		t.Errorf("expected error for unknown entity")
	}
}

func TestModify_AppliesPatch(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	id, _ := w.Create(1, "thing", world.Properties{"colour": "red"})
	args, _ := json.Marshal(modifyArgs{EntityID: uint64(id), PropertiesPatch: map[string]any{"colour": "blue", "size": "large"}})
	if _, err := Modify().Apply(w, 1, args); err != nil {
		t.Fatal(err)
	}
	e, _ := w.Entity(id)
	if e.Properties["colour"] != "blue" || e.Properties["size"] != "large" {
		t.Errorf("properties after Modify = %#v", e.Properties)
	}
}

func TestRelate_AndUnrelate(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	a, _ := w.Create(1, "a", nil)
	b, _ := w.Create(1, "b", nil)
	args, _ := json.Marshal(relateArgs{From: uint64(a), To: uint64(b), Kind: "knows"})
	if _, err := Relate().Apply(w, 1, args); err != nil {
		t.Fatal(err)
	}
	rels := w.Relationships()
	if len(rels) != 1 {
		t.Fatalf("relationships = %d, want 1", len(rels))
	}
	uargs, _ := json.Marshal(unrelateArgs{RelID: uint64(rels[0].ID)})
	if _, err := Unrelate().Apply(w, 1, uargs); err != nil {
		t.Fatal(err)
	}
	if w.RelationshipCount() != 0 {
		t.Errorf("live relationships after unrelate = %d, want 0", w.RelationshipCount())
	}
}

func TestObserve_FiltersByType(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	_, _ = w.Create(1, "planet", nil)
	_, _ = w.Create(1, "planet", nil)
	_, _ = w.Create(1, "star", nil)

	args, _ := json.Marshal(observeArgs{TypeLabel: "planet"})
	out, err := Observe().Apply(w, 1, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := out; got == "" || !contains(got, "2") || !contains(got, "planet") {
		t.Errorf("filter observe result = %q", got)
	}

	out, err = Observe().Apply(w, 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "3") {
		t.Errorf("unfiltered observe missing total: %q", out)
	}
}

func TestReflect_RequiresContent(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	if _, err := Reflect().Apply(w, 1, json.RawMessage(`{"content":""}`)); err == nil {
		t.Errorf("expected error for empty content")
	}
	out, err := Reflect().Apply(w, 1, json.RawMessage(`{"content":"this is new"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "this is new") {
		t.Errorf("reflect result missing content: %q", out)
	}
}

func TestSpawnAgent_CreatesEntity(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	args, _ := json.Marshal(spawnAgentArgs{Name: "watcher", SystemPrompt: "observe only", GrantedTools: []string{"Observe", "Reflect"}})
	if _, err := SpawnAgent().Apply(w, 1, args); err != nil {
		t.Fatal(err)
	}
	if w.EntityCount() != 1 {
		t.Fatalf("expected 1 entity, got %d", w.EntityCount())
	}
	e := w.Entities()[0]
	if e.TypeLabel != "agent" {
		t.Errorf("type_label = %q, want agent", e.TypeLabel)
	}
	if e.Properties["name"] != "watcher" {
		t.Errorf("name = %v", e.Properties["name"])
	}
}

func TestWait_ReturnsCount(t *testing.T) {
	t.Parallel()
	out, err := Wait().Apply(nil, 1, json.RawMessage(`{"n":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "3") {
		t.Errorf("wait result missing count: %q", out)
	}
}

func TestRandomArgs_Empty(t *testing.T) {
	t.Parallel()
	p := brain.Perception{} // empty world
	rng := newRng()

	if _, ok := Create().RandomArgs(rng, p); !ok {
		t.Errorf("Create.RandomArgs failed on empty world")
	}
	if _, ok := Modify().RandomArgs(rng, p); ok {
		t.Errorf("Modify.RandomArgs returned true on empty world")
	}
	if _, ok := Destroy().RandomArgs(rng, p); ok {
		t.Errorf("Destroy.RandomArgs returned true on empty world")
	}
	if _, ok := Relate().RandomArgs(rng, p); ok {
		t.Errorf("Relate.RandomArgs returned true with <2 entities")
	}
	if _, ok := Unrelate().RandomArgs(rng, p); ok {
		t.Errorf("Unrelate.RandomArgs returned true on empty world")
	}
	if _, ok := Reflect().RandomArgs(rng, p); !ok {
		t.Errorf("Reflect.RandomArgs failed on empty world")
	}
	if _, ok := Wait().RandomArgs(rng, p); !ok {
		t.Errorf("Wait.RandomArgs failed on empty world")
	}
}

func TestRandomArgs_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	p := brain.Perception{
		AliveEntities: []brain.EntityView{
			{ID: 1, TypeLabel: "planet"},
			{ID: 2, TypeLabel: "ocean"},
		},
		AliveRelationships: []brain.RelationshipView{
			{ID: 1, From: 2, To: 1, Kind: "part of"},
		},
	}
	rng := newRng()

	for _, tc := range []struct {
		name string
		tool Tool
	}{
		{"Create", Create()},
		{"Modify", Modify()},
		{"Destroy", Destroy()},
		{"Relate", Relate()},
		{"Unrelate", Unrelate()},
		{"Observe", Observe()},
		{"Reflect", Reflect()},
		{"Wait", Wait()},
	} {
		args, ok := tc.tool.RandomArgs(rng, p)
		if !ok {
			t.Errorf("%s.RandomArgs returned false", tc.name)
			continue
		}
		// Args must be valid JSON.
		var any json.RawMessage
		if err := json.Unmarshal(args, &any); err != nil {
			t.Errorf("%s.RandomArgs produced invalid JSON: %v\n%s", tc.name, err, args)
		}
	}
}

func TestRegistry_InvokeUnknown(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	reg := Default()
	_, err := reg.Invoke(brain.ToolCall{Name: "Bogus"}, w, 1)
	if !errors.Is(err, ErrUnknownTool) {
		t.Errorf("err = %v, want ErrUnknownTool", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

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
	for _, name := range []string{
		"Create", "Modify", "Destroy", "Relate", "Unrelate",
		"Observe", "Reflect", "SpawnAgent", "Speak", "DefineTool", "Die", "Wait",
		"FindByType", "FindByProperty", "FindRelated",
		"Zoom", "Unzoom",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing tool %q", name)
		}
	}
	if got := len(reg.All()); got != 17 {
		t.Errorf("registry size = %d, want 17", got)
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

func TestFindByType_Matches(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	_, _ = w.Create(1, "planet", world.Properties{"name": "Erith"})
	_, _ = w.Create(1, "planet", world.Properties{"name": "Pelor"})
	_, _ = w.Create(1, "star", world.Properties{"name": "Helios"})

	out, err := FindByType().Apply(w, 1, json.RawMessage(`{"type_label":"planet"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Matches []findHit `json:"matches"`
		Count   int       `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode result: %v\nraw: %s", err, out)
	}
	if got.Count != 2 || len(got.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", got.Count, got)
	}
	names := map[string]bool{got.Matches[0].Name: true, got.Matches[1].Name: true}
	if !names["Erith"] || !names["Pelor"] {
		t.Errorf("expected Erith and Pelor, got %+v", got.Matches)
	}
}

func TestFindByType_EmptyResult(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	out, err := FindByType().Apply(w, 1, json.RawMessage(`{"type_label":"unicorn"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, `"count":0`) {
		t.Errorf("empty result missing count:0: %q", out)
	}
}

func TestFindByType_RequiresType(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	if _, err := FindByType().Apply(w, 1, json.RawMessage(`{"type_label":""}`)); err == nil {
		t.Errorf("expected error for empty type_label")
	}
}

func TestFindByProperty_Matches(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	_, _ = w.Create(1, "flower", world.Properties{"colour": "blue"})
	_, _ = w.Create(1, "flower", world.Properties{"colour": "red"})
	_, _ = w.Create(1, "flower", world.Properties{"colour": "blue"})

	out, err := FindByProperty().Apply(w, 1, json.RawMessage(`{"key":"colour","value":"blue"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Matches []findHit `json:"matches"`
		Count   int       `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.Count != 2 {
		t.Errorf("expected 2 blue flowers, got %d", got.Count)
	}
}

func TestFindByProperty_NumberValue(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	_, _ = w.Create(1, "thing", world.Properties{"size": float64(42)})
	_, _ = w.Create(1, "thing", world.Properties{"size": float64(99)})

	out, err := FindByProperty().Apply(w, 1, json.RawMessage(`{"key":"size","value":42}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, `"count":1`) {
		t.Errorf("expected one match, got %q", out)
	}
}

func TestFindRelated_OutgoingAndIncoming(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	planet, _ := w.Create(1, "planet", world.Properties{"name": "Erith"})
	ocean, _ := w.Create(1, "ocean", world.Properties{"name": "Mare"})
	mountain, _ := w.Create(1, "mountain", nil)
	_, _ = w.Relate(1, ocean, planet, "part of")     // ocean -> planet
	_, _ = w.Relate(1, planet, mountain, "contains") // planet -> mountain

	out, err := FindRelated().Apply(w, 1, json.RawMessage(`{"entity_id":`+itoa(uint64(planet))+`}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Matches []relatedHit `json:"matches"`
		Count   int          `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 2 {
		t.Fatalf("expected 2 related, got %d: %+v", got.Count, got)
	}
	directions := map[string]string{}
	for _, h := range got.Matches {
		directions[h.Type] = h.Direction
	}
	if directions["ocean"] != "incoming" {
		t.Errorf("ocean direction = %q, want incoming", directions["ocean"])
	}
	if directions["mountain"] != "outgoing" {
		t.Errorf("mountain direction = %q, want outgoing", directions["mountain"])
	}
}

func TestFindRelated_KindFilter(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	a, _ := w.Create(1, "a", nil)
	b, _ := w.Create(1, "b", nil)
	c, _ := w.Create(1, "c", nil)
	_, _ = w.Relate(1, a, b, "knows")
	_, _ = w.Relate(1, a, c, "part of")

	out, err := FindRelated().Apply(w, 1, json.RawMessage(`{"entity_id":`+itoa(uint64(a))+`,"kind":"knows"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, `"count":1`) {
		t.Errorf("expected 1 match with kind filter, got %q", out)
	}
}

func TestFindRelated_RequiresEntityID(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	if _, err := FindRelated().Apply(w, 1, json.RawMessage(`{"entity_id":0}`)); err == nil {
		t.Errorf("expected error for entity_id=0")
	}
}

func TestZoom_ApplyStubMessage(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	out, err := Zoom().Apply(w, 1, json.RawMessage(`{"entity_id":42}`))
	if err != nil {
		t.Fatalf("Zoom.Apply: %v", err)
	}
	if !contains(out, "sim layer") {
		t.Errorf("Zoom.Apply result = %q, want sim-layer hint", out)
	}
	if _, err := Zoom().Apply(w, 1, json.RawMessage(`{"entity_id":0}`)); err == nil {
		t.Errorf("expected error for entity_id=0")
	}
}

func TestZoom_RandomArgs_SkipsWhenNoContainers(t *testing.T) {
	t.Parallel()
	rng := newRng()
	// Empty world.
	if _, ok := Zoom().RandomArgs(rng, brain.Perception{}); ok {
		t.Errorf("Zoom.RandomArgs returned true on empty world")
	}
	// Entities present but no containment edges.
	p := brain.Perception{
		AliveEntities: []brain.EntityView{
			{ID: 1, TypeLabel: "planet"},
			{ID: 2, TypeLabel: "ocean"},
		},
	}
	if _, ok := Zoom().RandomArgs(rng, p); ok {
		t.Errorf("Zoom.RandomArgs returned true with no containment edges")
	}
	// One container should be enough.
	p.AliveRelationships = []brain.RelationshipView{
		{ID: 1, From: 1, To: 2, Kind: "contains"},
	}
	args, ok := Zoom().RandomArgs(rng, p)
	if !ok {
		t.Fatalf("Zoom.RandomArgs returned false with a container present")
	}
	var got ZoomArgs
	if err := json.Unmarshal(args, &got); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if got.EntityID != 1 {
		t.Errorf("EntityID = %d, want 1", got.EntityID)
	}
}

func TestUnzoom_ApplyStubMessage(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	out, err := Unzoom().Apply(w, 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Unzoom.Apply: %v", err)
	}
	if !contains(out, "sim layer") {
		t.Errorf("Unzoom.Apply result = %q, want sim-layer hint", out)
	}
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

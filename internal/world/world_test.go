package world

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

const testAgent AgentID = 42

func mustNewWorld(t *testing.T, name string) *World {
	t.Helper()
	w, err := New(name)
	if err != nil {
		t.Fatalf("New(%q) failed: %v", name, err)
	}
	return w
}

func TestNew_EmptyWorldInvariants(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")

	if got := w.Name(); got != "kosmos" {
		t.Errorf("Name() = %q, want %q", got, "kosmos")
	}
	if got := w.Tick(); got != 0 {
		t.Errorf("Tick() = %d, want 0", got)
	}
	if got := w.EntityCount(); got != 0 {
		t.Errorf("EntityCount() = %d, want 0", got)
	}
	if got := w.RelationshipCount(); got != 0 {
		t.Errorf("RelationshipCount() = %d, want 0", got)
	}
	if got := len(w.Events()); got != 0 {
		t.Errorf("Events length = %d, want 0", got)
	}
	if got := len(w.Entities()); got != 0 {
		t.Errorf("Entities length = %d, want 0", got)
	}
	if got := len(w.Relationships()); got != 0 {
		t.Errorf("Relationships length = %d, want 0", got)
	}
}

func TestNew_EmptyNameRejected(t *testing.T) {
	t.Parallel()
	if _, err := New(""); !errors.Is(err, ErrEmptyName) {
		t.Errorf("New(\"\") err = %v, want ErrEmptyName", err)
	}
}

func TestCreate_AssignsIDAndEmitsEvent(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")

	id, err := w.Create(testAgent, "planet", Properties{"name": "Erith"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if id != 1 {
		t.Errorf("first EntityID = %d, want 1", id)
	}

	got, ok := w.Entity(id)
	if !ok {
		t.Fatalf("Entity(%d) not found after Create", id)
	}
	if got.TypeLabel != "planet" {
		t.Errorf("TypeLabel = %q, want planet", got.TypeLabel)
	}
	if got.Properties["name"] != "Erith" {
		t.Errorf("properties.name = %v, want Erith", got.Properties["name"])
	}
	if got.CreatedBy != testAgent {
		t.Errorf("CreatedBy = %d, want %d", got.CreatedBy, testAgent)
	}
	if !got.IsAlive() {
		t.Errorf("entity should be alive after Create")
	}

	events := w.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventCreate || events[0].EntityID != id {
		t.Errorf("event = %+v, want Create id=%d", events[0], id)
	}
}

func TestCreate_RejectsEmptyTypeLabel(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	if _, err := w.Create(testAgent, "", nil); !errors.Is(err, ErrEmptyTypeLabel) {
		t.Errorf("err = %v, want ErrEmptyTypeLabel", err)
	}
}

func TestCreate_DoesNotShareProperties(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	src := Properties{"k": "v"}
	id, err := w.Create(testAgent, "thing", src)
	if err != nil {
		t.Fatal(err)
	}

	src["k"] = "MUTATED"
	got, _ := w.Entity(id)
	if got.Properties["k"] != "v" {
		t.Errorf("registry shared the caller's properties map: %v", got.Properties)
	}

	got.Properties["k"] = "ALSO MUTATED"
	again, _ := w.Entity(id)
	if again.Properties["k"] != "v" {
		t.Errorf("Entity() returned a shared properties reference: %v", again.Properties)
	}
}

func TestModify_MergePatchAndDeletion(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	id, _ := w.Create(testAgent, "planet", Properties{"name": "old", "mass": float64(1)})

	if err := w.Modify(testAgent, id, Properties{"name": "new", "mass": nil, "habitable": true}); err != nil {
		t.Fatalf("Modify: %v", err)
	}

	got, _ := w.Entity(id)
	if got.Properties["name"] != "new" {
		t.Errorf("name = %v, want new", got.Properties["name"])
	}
	if _, ok := got.Properties["mass"]; ok {
		t.Errorf("mass should have been deleted: %v", got.Properties)
	}
	if got.Properties["habitable"] != true {
		t.Errorf("habitable = %v, want true", got.Properties["habitable"])
	}
}

func TestModify_NotFound(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	if err := w.Modify(testAgent, 99, Properties{"x": 1}); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("err = %v, want ErrEntityNotFound", err)
	}
}

func TestModify_OnDestroyedFails(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	id, _ := w.Create(testAgent, "thing", nil)
	if err := w.Destroy(testAgent, id); err != nil {
		t.Fatal(err)
	}
	if err := w.Modify(testAgent, id, Properties{"x": 1}); !errors.Is(err, ErrEntityDestroyed) {
		t.Errorf("err = %v, want ErrEntityDestroyed", err)
	}
}

func TestDestroy_SoftDeletesAndCascadesRelationships(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")

	a, _ := w.Create(testAgent, "planet", nil)
	b, _ := w.Create(testAgent, "ocean", nil)
	c, _ := w.Create(testAgent, "mountain", nil)

	r1, err := w.Relate(testAgent, b, a, "part of")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := w.Relate(testAgent, c, a, "part of")
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated to a:
	rOther, err := w.Relate(testAgent, b, c, "borders")
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Destroy(testAgent, a); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	entA, ok := w.Entity(a)
	if !ok {
		t.Fatalf("destroyed entity should still be retrievable")
	}
	if entA.IsAlive() {
		t.Errorf("entity %d should be soft-deleted", a)
	}
	if got := w.EntityCount(); got != 2 {
		t.Errorf("EntityCount() = %d, want 2 (a is destroyed)", got)
	}

	for _, id := range []RelationshipID{r1, r2} {
		r, _ := w.Relationship(id)
		if r.IsAlive() {
			t.Errorf("relationship %d should have cascaded; got alive", id)
		}
	}
	r, _ := w.Relationship(rOther)
	if !r.IsAlive() {
		t.Errorf("relationship %d should be unaffected; got destroyed", rOther)
	}

	// Cascade events should be flagged.
	var cascaded int
	for _, e := range w.Events() {
		if e.Kind == EventUnrelate && e.Cascade {
			cascaded++
		}
	}
	if cascaded != 2 {
		t.Errorf("cascade unrelate events = %d, want 2", cascaded)
	}
}

func TestDestroy_NotFoundAndDoubleDestroy(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	if err := w.Destroy(testAgent, 99); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("err = %v, want ErrEntityNotFound", err)
	}
	id, _ := w.Create(testAgent, "thing", nil)
	if err := w.Destroy(testAgent, id); err != nil {
		t.Fatal(err)
	}
	if err := w.Destroy(testAgent, id); !errors.Is(err, ErrEntityDestroyed) {
		t.Errorf("err = %v, want ErrEntityDestroyed", err)
	}
}

func TestRelate_RequiresLiveEndpointsAndKind(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	a, _ := w.Create(testAgent, "thing", nil)
	b, _ := w.Create(testAgent, "thing", nil)

	if _, err := w.Relate(testAgent, a, b, ""); !errors.Is(err, ErrEmptyKind) {
		t.Errorf("err = %v, want ErrEmptyKind", err)
	}
	if _, err := w.Relate(testAgent, a, 99, "x"); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("err = %v, want ErrEntityNotFound", err)
	}

	if err := w.Destroy(testAgent, b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Relate(testAgent, a, b, "x"); !errors.Is(err, ErrEntityDestroyed) {
		t.Errorf("err = %v, want ErrEntityDestroyed", err)
	}
}

func TestRelate_SelfRelationAllowed(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	id, _ := w.Create(testAgent, "thing", nil)
	if _, err := w.Relate(testAgent, id, id, "self"); err != nil {
		t.Errorf("self relate should be allowed: %v", err)
	}
}

func TestUnrelate(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	a, _ := w.Create(testAgent, "thing", nil)
	b, _ := w.Create(testAgent, "thing", nil)
	rid, _ := w.Relate(testAgent, a, b, "k")

	if err := w.Unrelate(testAgent, rid); err != nil {
		t.Fatal(err)
	}
	if err := w.Unrelate(testAgent, rid); !errors.Is(err, ErrRelationshipDestroyed) {
		t.Errorf("err = %v, want ErrRelationshipDestroyed", err)
	}
	if err := w.Unrelate(testAgent, 999); !errors.Is(err, ErrRelationshipNotFound) {
		t.Errorf("err = %v, want ErrRelationshipNotFound", err)
	}
}

func TestAdvanceTick(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	if got := w.AdvanceTick(); got != 1 {
		t.Errorf("first AdvanceTick = %d, want 1", got)
	}
	if got := w.AdvanceTick(); got != 2 {
		t.Errorf("second AdvanceTick = %d, want 2", got)
	}
	if got := w.Tick(); got != 2 {
		t.Errorf("Tick() = %d, want 2", got)
	}

	events := w.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 tick_start events, got %d", len(events))
	}
	for i, e := range events {
		if e.Kind != EventTickStart {
			t.Errorf("event[%d].Kind = %q, want %q", i, e.Kind, EventTickStart)
		}
	}
}

func TestEvents_OrderedAndTickStamped(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	a, _ := w.Create(testAgent, "x", nil)
	_ = w.AdvanceTick()
	b, _ := w.Create(testAgent, "y", nil)
	_, _ = w.Relate(testAgent, a, b, "k")

	events := w.Events()
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}

	for i, e := range events {
		want := EventID(i + 1)
		if e.ID != want {
			t.Errorf("event[%d].ID = %d, want %d", i, e.ID, want)
		}
	}
	if events[0].Tick != 0 || events[1].Tick != 1 || events[2].Tick != 1 || events[3].Tick != 1 {
		t.Errorf("event ticks unexpected: %v %v %v %v",
			events[0].Tick, events[1].Tick, events[2].Tick, events[3].Tick)
	}
}

func TestConcurrent_CreatesAreSerialised(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	var failures atomic.Int64
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := w.Create(testAgent, "thing", nil); err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := failures.Load(); got != 0 {
		t.Fatalf("%d concurrent Create calls failed", got)
	}
	if got := w.EntityCount(); got != n {
		t.Errorf("EntityCount = %d, want %d", got, n)
	}
	seen := make(map[EntityID]bool, n)
	for _, e := range w.Entities() {
		if seen[e.ID] {
			t.Errorf("duplicate entity ID issued: %d", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestApplyEventForLoad_ReplayRoundTrip(t *testing.T) {
	t.Parallel()
	w := mustNewWorld(t, "kosmos")
	a, _ := w.Create(testAgent, "planet", Properties{"name": "Erith"})
	b, _ := w.Create(testAgent, "ocean", Properties{"depth": float64(5)})
	_ = w.AdvanceTick()
	if err := w.Modify(testAgent, b, Properties{"depth": float64(7)}); err != nil {
		t.Fatal(err)
	}
	rid, _ := w.Relate(testAgent, b, a, "part of")
	_ = w.Unrelate(testAgent, rid)
	if err := w.Destroy(testAgent, a); err != nil {
		t.Fatal(err)
	}

	// Replay into a fresh world.
	replay := mustNewWorld(t, "kosmos")
	for _, e := range w.Events() {
		if err := replay.ApplyEventForLoad(e); err != nil {
			t.Fatalf("ApplyEventForLoad: %v", err)
		}
	}

	if got, want := replay.Tick(), w.Tick(); got != want {
		t.Errorf("replay tick = %d, want %d", got, want)
	}
	if got, want := replay.NextEntityID(), w.NextEntityID(); got != want {
		t.Errorf("replay nextEntity = %d, want %d", got, want)
	}
	if got, want := replay.NextRelationshipID(), w.NextRelationshipID(); got != want {
		t.Errorf("replay nextRel = %d, want %d", got, want)
	}
	if got, want := len(replay.Events()), len(w.Events()); got != want {
		t.Errorf("replay events = %d, want %d", got, want)
	}
	if got, want := replay.EntityCount(), w.EntityCount(); got != want {
		t.Errorf("replay EntityCount = %d, want %d", got, want)
	}
	if got, want := replay.RelationshipCount(), w.RelationshipCount(); got != want {
		t.Errorf("replay RelationshipCount = %d, want %d", got, want)
	}
}

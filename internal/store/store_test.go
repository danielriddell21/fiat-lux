package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

const testAgent world.AgentID = 7

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seededWorld(t *testing.T) *world.World {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatalf("world.New: %v", err)
	}
	a, err := w.Create(testAgent, "planet", world.Properties{"name": "Erith", "mass": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Create(testAgent, "ocean", world.Properties{"depth": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AdvanceTick()
	if err := w.Modify(testAgent, b, world.Properties{"depth": float64(7), "salinity": "high"}); err != nil {
		t.Fatal(err)
	}
	rid, err := w.Relate(testAgent, b, a, "part of")
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AdvanceTick()
	if err := w.Unrelate(testAgent, rid); err != nil {
		t.Fatal(err)
	}
	if err := w.Destroy(testAgent, a); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestOpen_AppliesSchema(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, ":memory:")
	worlds, err := s.ListWorlds(context.Background())
	if err != nil {
		t.Fatalf("ListWorlds: %v", err)
	}
	if len(worlds) != 0 {
		t.Errorf("fresh store has worlds: %v", worlds)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, ":memory:")
	w := seededWorld(t)

	ctx := context.Background()
	if err := s.Save(ctx, w); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := s.Load(ctx, "kosmos")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := loaded.Name(), w.Name(); got != want {
		t.Errorf("Name: got %q, want %q", got, want)
	}
	if got, want := loaded.Tick(), w.Tick(); got != want {
		t.Errorf("Tick: got %d, want %d", got, want)
	}
	if got, want := loaded.EntityCount(), w.EntityCount(); got != want {
		t.Errorf("EntityCount: got %d, want %d", got, want)
	}
	if got, want := loaded.RelationshipCount(), w.RelationshipCount(); got != want {
		t.Errorf("RelationshipCount: got %d, want %d", got, want)
	}
	if got, want := len(loaded.Events()), len(w.Events()); got != want {
		t.Errorf("len(Events): got %d, want %d", got, want)
	}
	if got, want := loaded.NextEntityID(), w.NextEntityID(); got != want {
		t.Errorf("NextEntityID: got %d, want %d", got, want)
	}
	if got, want := loaded.NextRelationshipID(), w.NextRelationshipID(); got != want {
		t.Errorf("NextRelationshipID: got %d, want %d", got, want)
	}

	wantEnts := w.EntitiesAll()
	gotEnts := loaded.EntitiesAll()
	if !reflect.DeepEqual(wantEnts, gotEnts) {
		t.Errorf("EntitiesAll differ\n want: %#v\n  got: %#v", wantEnts, gotEnts)
	}
	wantRels := w.RelationshipsAll()
	gotRels := loaded.RelationshipsAll()
	if !reflect.DeepEqual(wantRels, gotRels) {
		t.Errorf("RelationshipsAll differ\n want: %#v\n  got: %#v", wantRels, gotRels)
	}
	if !reflect.DeepEqual(w.Events(), loaded.Events()) {
		t.Errorf("Events differ")
	}
}

func TestSaveLoad_OnDisk(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fiatlux.db")
	ctx := context.Background()

	{
		s := mustOpen(t, path)
		if err := s.Save(ctx, seededWorld(t)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	s := mustOpen(t, path)
	loaded, err := s.Load(ctx, "kosmos")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.EntityCount() != 1 {
		t.Errorf("EntityCount after reload = %d, want 1", loaded.EntityCount())
	}
}

func TestSaveLoad_Resave(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, ":memory:")
	ctx := context.Background()
	w := seededWorld(t)

	for i := 0; i < 3; i++ {
		if err := s.Save(ctx, w); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}

	loaded, err := s.Load(ctx, "kosmos")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(loaded.Events()), len(w.Events()); got != want {
		t.Errorf("event count after resave = %d, want %d", got, want)
	}
}

func TestLoad_UnknownWorld(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, ":memory:")
	_, err := s.Load(context.Background(), "nope")
	if !errors.Is(err, ErrWorldNotFound) {
		t.Errorf("err = %v, want ErrWorldNotFound", err)
	}
}

func TestListWorlds(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, ":memory:")
	ctx := context.Background()

	for _, name := range []string{"beth", "kosmos", "gimel"} {
		w, err := world.New(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Create(testAgent, "thing", nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, w); err != nil {
			t.Fatalf("Save(%q): %v", name, err)
		}
	}

	got, err := s.ListWorlds(ctx)
	if err != nil {
		t.Fatalf("ListWorlds: %v", err)
	}
	want := []string{"beth", "gimel", "kosmos"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListWorlds = %v, want %v", got, want)
	}
}

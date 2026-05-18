package tui

import (
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

func mustWorld(t *testing.T) *world.World {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestBuildTreeView_Empty(t *testing.T) {
	t.Parallel()
	v := BuildTreeView(nil, nil)
	if len(v.Roots) != 0 {
		t.Errorf("expected no roots, got %d", len(v.Roots))
	}
}

func TestBuildTreeView_GroupsContainmentChildrenUnderParent(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)

	planet, _ := w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	ocean, _ := w.Create(world.NoAgent, "ocean", nil)
	continent, _ := w.Create(world.NoAgent, "continent", nil)
	star, _ := w.Create(world.NoAgent, "star", world.Properties{"name": "Helios"})

	if _, err := w.Relate(world.NoAgent, ocean, planet, "part of"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Relate(world.NoAgent, continent, planet, "part of"); err != nil {
		t.Fatal(err)
	}

	v := BuildTreeView(w.Entities(), w.Relationships())

	// Roots should be planet and star (star is unrelated).
	if len(v.Roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(v.Roots))
	}
	if v.Roots[0].ID != planet {
		t.Errorf("first root = %d, want %d (planet)", v.Roots[0].ID, planet)
	}
	if v.Roots[1].ID != star {
		t.Errorf("second root = %d, want %d (star)", v.Roots[1].ID, star)
	}
	if len(v.Roots[0].Children) != 2 {
		t.Fatalf("planet children = %d, want 2", len(v.Roots[0].Children))
	}
	// Children sorted by ID.
	if v.Roots[0].Children[0].ID != ocean {
		t.Errorf("first child = %d, want %d (ocean)", v.Roots[0].Children[0].ID, ocean)
	}
	if v.Roots[0].Children[1].ID != continent {
		t.Errorf("second child = %d, want %d (continent)", v.Roots[0].Children[1].ID, continent)
	}
}

func TestBuildTreeView_ContainsKindReversesDirection(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)

	box, _ := w.Create(world.NoAgent, "box", nil)
	cat, _ := w.Create(world.NoAgent, "cat", nil)
	if _, err := w.Relate(world.NoAgent, box, cat, "contains"); err != nil {
		t.Fatal(err)
	}

	v := BuildTreeView(w.Entities(), w.Relationships())
	if len(v.Roots) != 1 || v.Roots[0].ID != box {
		t.Fatalf("roots = %#v, want one root = box", v.Roots)
	}
	if len(v.Roots[0].Children) != 1 || v.Roots[0].Children[0].ID != cat {
		t.Errorf("box's children = %#v, want [cat]", v.Roots[0].Children)
	}
}

func TestBuildTreeView_IgnoresNonContainmentAndDeadRelationships(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	a, _ := w.Create(world.NoAgent, "thing", nil)
	b, _ := w.Create(world.NoAgent, "thing", nil)
	c, _ := w.Create(world.NoAgent, "thing", nil)

	if _, err := w.Relate(world.NoAgent, a, b, "borders"); err != nil {
		t.Fatal(err)
	}
	rid, err := w.Relate(world.NoAgent, c, a, "part of")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Unrelate(world.NoAgent, rid); err != nil {
		t.Fatal(err)
	}

	v := BuildTreeView(w.Entities(), w.Relationships())
	// All three entities should be roots (no live containment).
	if len(v.Roots) != 3 {
		t.Errorf("roots = %d, want 3", len(v.Roots))
	}
}

func TestRenderTree_EmptyPlaceholder(t *testing.T) {
	t.Parallel()
	s := DefaultStyles()
	out := RenderTree(BuildTreeView(nil, nil), s)
	if !strings.Contains(out, "void") {
		t.Errorf("empty tree render missing 'void' placeholder: %q", out)
	}
}

func TestRenderTree_NestedContainment(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	planet, _ := w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	continent, _ := w.Create(world.NoAgent, "continent", world.Properties{"name": "Rho"})
	mountain, _ := w.Create(world.NoAgent, "mountain", nil)

	if _, err := w.Relate(world.NoAgent, continent, planet, "part of"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Relate(world.NoAgent, mountain, continent, "part of"); err != nil {
		t.Fatal(err)
	}

	v := BuildTreeView(w.Entities(), w.Relationships())
	if len(v.Roots) != 1 || v.Roots[0].ID != planet {
		t.Fatalf("expected single planet root, got %#v", v.Roots)
	}
	if len(v.Roots[0].Children) != 1 || v.Roots[0].Children[0].ID != continent {
		t.Fatalf("expected continent child, got %#v", v.Roots[0].Children)
	}
	if len(v.Roots[0].Children[0].Children) != 1 || v.Roots[0].Children[0].Children[0].ID != mountain {
		t.Fatalf("expected mountain grandchild, got %#v", v.Roots[0].Children[0].Children)
	}

	out := RenderTree(v, DefaultStyles())
	for _, want := range []string{"planet", "Erith", "continent", "Rho", "mountain"} {
		if !strings.Contains(out, want) {
			t.Errorf("nested tree missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestRenderTree_ContainsAllEntities(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	_, _ = w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	_, _ = w.Create(world.NoAgent, "star", world.Properties{"name": "Helios"})

	out := RenderTree(BuildTreeView(w.Entities(), w.Relationships()), DefaultStyles())
	for _, want := range []string{"planet", "Erith", "star", "Helios"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree render missing %q\noutput:\n%s", want, out)
		}
	}
}

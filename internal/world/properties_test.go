package world

import (
	"reflect"
	"testing"
)

func TestProperties_Clone_DeepCopy(t *testing.T) {
	t.Parallel()

	original := Properties{
		"name":   "Erith",
		"nested": map[string]any{"depth": float64(3)},
		"tags":   []any{"alpha", "beta"},
	}

	clone := original.Clone()
	if !reflect.DeepEqual(original, clone) {
		t.Fatalf("clone differs from original\norig:  %#v\nclone: %#v", original, clone)
	}

	// Mutate the clone; the original must be untouched.
	clone["name"] = "Other"
	nested, ok := clone["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested map not preserved through clone: %T", clone["nested"])
	}
	nested["depth"] = float64(99)

	if original["name"] != "Erith" {
		t.Errorf("original.name mutated via clone: %v", original["name"])
	}
	if orig := original["nested"].(map[string]any)["depth"]; orig != float64(3) {
		t.Errorf("original.nested.depth mutated via clone: %v", orig)
	}
}

func TestProperties_Clone_NilSafe(t *testing.T) {
	t.Parallel()
	var p Properties
	if got := p.Clone(); got != nil {
		t.Errorf("Clone of nil Properties = %v, want nil", got)
	}
}

func TestProperties_ApplyMergePatch_AddReplaceDelete(t *testing.T) {
	t.Parallel()

	base := Properties{
		"keep":   "yes",
		"swap":   "old",
		"remove": "soon",
	}
	patch := Properties{
		"swap":   "new",
		"remove": nil,
		"add":    "fresh",
	}

	got := base.ApplyMergePatch(patch)
	want := Properties{
		"keep": "yes",
		"swap": "new",
		"add":  "fresh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge patch: got %#v, want %#v", got, want)
	}

	// The base must not have been mutated.
	if _, ok := base["add"]; ok {
		t.Errorf("base mutated by ApplyMergePatch: %#v", base)
	}
	if base["swap"] != "old" {
		t.Errorf("base.swap mutated by ApplyMergePatch: %v", base["swap"])
	}
}

func TestProperties_ApplyMergePatch_RecursiveMerge(t *testing.T) {
	t.Parallel()

	base := Properties{
		"location": map[string]any{
			"x": float64(1),
			"y": float64(2),
		},
	}
	patch := Properties{
		"location": map[string]any{
			"y": float64(99),
			"z": float64(5),
		},
	}

	got := base.ApplyMergePatch(patch)
	want := Properties{
		"location": map[string]any{
			"x": float64(1),
			"y": float64(99),
			"z": float64(5),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recursive merge: got %#v, want %#v", got, want)
	}
}

func TestProperties_ApplyMergePatch_ReplaceWhenTypeMismatch(t *testing.T) {
	t.Parallel()

	base := Properties{"x": "scalar"}
	patch := Properties{"x": map[string]any{"k": "v"}}
	got := base.ApplyMergePatch(patch)

	want := Properties{"x": map[string]any{"k": "v"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("type-mismatch merge: got %#v, want %#v", got, want)
	}
}

func TestProperties_ApplyMergePatch_OnNilBase(t *testing.T) {
	t.Parallel()

	var base Properties
	got := base.ApplyMergePatch(Properties{"a": float64(1)})
	want := Properties{"a": float64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nil-base merge: got %#v, want %#v", got, want)
	}
}

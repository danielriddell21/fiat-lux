package drives

import (
	"maps"
	"math"
	"slices"
	"testing"
)

func TestClone_Independent(t *testing.T) {
	t.Parallel()
	orig := State{"novelty": 0.7, "growth": 0.3}
	dup := orig.Clone()
	dup["novelty"] = 0.1
	if orig["novelty"] != 0.7 {
		t.Errorf("clone leaked: orig novelty = %v", orig["novelty"])
	}
}

func TestClone_NilPassthrough(t *testing.T) {
	t.Parallel()
	if got := State(nil).Clone(); got != nil {
		t.Errorf("Clone(nil) = %v, want nil", got)
	}
}

func TestKeys_Sorted(t *testing.T) {
	t.Parallel()
	s := State{"growth": 1, "novelty": 1, "coherence": 1}
	got := s.Keys()
	want := []string{"coherence", "growth", "novelty"}
	if !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
}

func TestAsMap_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	got := State{}.AsMap()
	if got != nil {
		t.Errorf("AsMap() on empty = %v, want nil", got)
	}
}

func TestAsMap_PreservesValues(t *testing.T) {
	t.Parallel()
	s := State{"novelty": 0.7}
	got := s.AsMap()
	if got["novelty"] != 0.7 {
		t.Errorf("AsMap()[novelty] = %v, want 0.7", got["novelty"])
	}
}

func TestValidate_RejectsEmptyKey(t *testing.T) {
	t.Parallel()
	err := State{"": 0.5}.Validate()
	if err == nil {
		t.Error("expected error for empty drive name")
	}
}

func TestValidate_RejectsNaN(t *testing.T) {
	t.Parallel()
	err := State{"x": math.NaN()}.Validate()
	if err == nil {
		t.Error("expected error for NaN weight")
	}
}

func TestValidate_AcceptsNegative(t *testing.T) {
	t.Parallel()
	err := State{"aversion": -0.5}.Validate()
	if err != nil {
		t.Errorf("negative weights should be allowed: %v", err)
	}
}

func TestFromAny_MixedNumericTypes(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"a": 0.5,
		"b": 1,
		"c": int64(2),
		"d": float32(0.25),
		"e": "ignored",
	}
	got := FromAny(in)
	want := State{"a": 0.5, "b": 1.0, "c": 2.0, "d": 0.25}
	if !maps.Equal(got, want) {
		t.Errorf("FromAny() = %v, want %v", got, want)
	}
}

func TestFromAny_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	if got := FromAny(nil); got != nil {
		t.Errorf("FromAny(nil) = %v, want nil", got)
	}
	if got := FromAny(map[string]any{}); got != nil {
		t.Errorf("FromAny(empty) = %v, want nil", got)
	}
	if got := FromAny(map[string]any{"x": "not a number"}); got != nil {
		t.Errorf("FromAny(non-numeric only) = %v, want nil", got)
	}
}

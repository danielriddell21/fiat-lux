// Package drives models the agent's intrinsic motivational state as
// a free-form map of named float weights (e.g. novelty, coherence,
// growth, equilibrium). Drives surface in the agent's Perception
// every tick so the brain can weight its decisions against them.
//
// Drive names are deliberately unconstrained - fiat-lux does not
// interpret them. The agent is free to honour, ignore, or redefine
// any drive; the framework's job is only to carry the numbers from
// configuration to perception.
package drives

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

// State is the agent's drive map. Keys are agent-meaningful labels;
// values are unbounded floats (typically 0..1, but the framework
// does not clamp). The zero value is a valid empty state.
type State map[string]float64

// Clone returns a deep copy. Returns nil when s is nil so callers
// can use the zero-value idiom.
func (s State) Clone() State {
	if s == nil {
		return nil
	}
	return maps.Clone(s)
}

// Keys returns the drive names sorted alphabetically. Stable output
// matters for prompt rendering so the brain sees the same shape each
// tick.
func (s State) Keys() []string {
	if len(s) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(s))
}

// AsMap returns the drives as a plain map[string]any suitable for
// stashing in an entity's Properties bag. Returns nil for an empty
// state so the property is omitted entirely.
func (s State) AsMap() map[string]any {
	if len(s) == 0 {
		return nil
	}
	out := make(map[string]any, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// Validate rejects empty keys and non-finite values. NaN and infinity
// would silently corrupt the prompt and JSON-marshal as null.
func (s State) Validate() error {
	for k, v := range s {
		if k == "" {
			return errors.New("drives: empty drive name")
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("drives: %q has non-finite weight", k)
		}
	}
	return nil
}

// FromAny decodes drives from a map[string]any (the shape YAML, JSON,
// and entity-property bags produce). Non-numeric values are ignored
// silently; callers wanting strict parsing should Validate() first.
// Returns nil when the input is empty.
func FromAny(in map[string]any) State {
	if len(in) == 0 {
		return nil
	}
	out := make(State, len(in))
	for k, v := range in {
		switch n := v.(type) {
		case float64:
			out[k] = n
		case float32:
			out[k] = float64(n)
		case int:
			out[k] = float64(n)
		case int64:
			out[k] = float64(n)
		case uint64:
			out[k] = float64(n)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

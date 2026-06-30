package drives

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

type State map[string]float64

func (s State) Clone() State {
	if s == nil {
		return nil
	}
	return maps.Clone(s)
}

func (s State) Keys() []string {
	if len(s) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(s))
}

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

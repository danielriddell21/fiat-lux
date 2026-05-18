package world

import (
	"encoding/json"
	"fmt"
)

// Properties is an entity's agent-authored property bag. The
// simulation never interprets the contents - meaning belongs to the
// agent. Values are JSON-shaped: strings, numbers (float64 after
// round-tripping), booleans, nil, []any, or nested map[string]any.
//
// reason: properties are arbitrary agent-authored JSON; their schema
// is by design not knowable in advance, so map[string]any is the
// right representation.
type Properties map[string]any

// Clone returns a deep copy of p via a JSON round-trip. Numeric
// values become float64 (standard JSON behaviour); since the
// simulation does not interpret values this is acceptable and
// guarantees structural isolation.
func (p Properties) Clone() Properties {
	if p == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		// Properties came from JSON or from an agent tool call that
		// passed validation; an unmarshalable map here would indicate
		// a programming bug, not user input.
		panic(fmt.Sprintf("world: properties not JSON-marshalable: %v", err))
	}
	var out Properties
	if err := json.Unmarshal(b, &out); err != nil {
		panic(fmt.Sprintf("world: properties round-trip failed: %v", err))
	}
	return out
}

// ApplyMergePatch applies an RFC 7396 JSON Merge Patch to p,
// returning the merged result. The original p is not modified.
//
// Semantics:
//   - if patch[k] is nil, k is deleted from the result
//   - if patch[k] and p[k] are both maps, they are merged recursively
//   - otherwise patch[k] replaces p[k] wholesale
func (p Properties) ApplyMergePatch(patch Properties) Properties {
	merged := p.Clone()
	if merged == nil {
		merged = Properties{}
	}
	mergeInto(merged, patch)
	return merged
}

func mergeInto(target, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(target, k)
			continue
		}
		if pm, ok := v.(map[string]any); ok {
			if tm, ok := target[k].(map[string]any); ok {
				mergeInto(tm, pm)
				continue
			}
			fresh := make(map[string]any, len(pm))
			mergeInto(fresh, pm)
			target[k] = fresh
			continue
		}
		target[k] = v
	}
}

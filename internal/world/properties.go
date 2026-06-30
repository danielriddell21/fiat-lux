package world

import (
	"encoding/json"
	"fmt"
)

type Properties map[string]any

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

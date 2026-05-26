package sim

import (
	"encoding/json"

	"github.com/danielriddell21/fiat-lux/internal/macros"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// MacrosFromEvents replays EventDefineTool entries out of the world's
// event log into a fresh macro Set. Validation is deferred to the
// receiving sim - this function only handles the JSON shape.
func MacrosFromEvents(w *world.World) *macros.Set {
	if w == nil {
		return nil
	}
	set := macros.NewSet()
	for _, ev := range w.Events() {
		if ev.Kind != world.EventDefineTool {
			continue
		}
		raw, ok := ev.Props["macro"]
		if !ok {
			continue
		}
		var blob []byte
		switch v := raw.(type) {
		case json.RawMessage:
			blob = []byte(v)
		case string:
			blob = []byte(v)
		case []byte:
			blob = v
		default:
			marshalled, err := json.Marshal(v)
			if err != nil {
				continue
			}
			blob = marshalled
		}
		var m macros.Macro
		if err := json.Unmarshal(blob, &m); err != nil {
			continue
		}
		_ = set.AddTrusted(m)
	}
	return set
}

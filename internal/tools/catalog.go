package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Default returns the full tool catalogue: Create, Modify,
// Destroy, Relate, Unrelate, Observe, Reflect, SpawnAgent, Speak,
// Wait. SpawnAgent and Speak are intercepted by the sim layer
// (which has access to per-world agent state); their Apply funcs
// are minimal stubs the sim never invokes.
func Default() *Registry {
	return NewRegistry(
		Create(),
		Modify(),
		Destroy(),
		Relate(),
		Unrelate(),
		Observe(),
		Reflect(),
		SpawnAgent(),
		Speak(),
		Wait(),
	)
}

// ---- Create -----------------------------------------------------------------

type createArgs struct {
	TypeLabel  string         `json:"type_label"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Create brings a new entity into being.
func Create() Tool {
	return Tool{
		Name:        "Create",
		Description: "Bring a new entity into being. The agent chooses both the type_label and the properties; the simulation does not interpret them.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type_label": map[string]any{"type": "string", "description": "agent-chosen kind of thing"},
				"properties": map[string]any{"type": "object", "description": "arbitrary agent-authored property bag"},
			},
			"required": []string{"type_label"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a createArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Create: bad args: %w", err)
			}
			id, err := w.Create(agent, a.TypeLabel, world.Properties(a.Properties))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("created %s #%d", a.TypeLabel, id), nil
		},
		RandomArgs: func(rng *rand.Rand, _ brain.Perception) (json.RawMessage, bool) {
			seeds := []struct {
				kind  string
				props map[string]any
			}{
				{"planet", map[string]any{"name": pick(rng, planetNames)}},
				{"ocean", map[string]any{"depth": rng.IntN(11000)}},
				{"continent", map[string]any{"name": pick(rng, continentNames)}},
				{"mountain", map[string]any{"elevation": rng.IntN(9000) + 500}},
				{"forest", map[string]any{"density": rng.Float64()}},
				{"star", map[string]any{"name": pick(rng, starNames)}},
				{"creature", map[string]any{"size": pick(rng, []string{"microscopic", "small", "large", "vast"})}},
				{"idea", map[string]any{"content": pick(rng, ideaPhrases)}},
			}
			s := seeds[rng.IntN(len(seeds))]
			b, err := json.Marshal(createArgs{TypeLabel: s.kind, Properties: s.props})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Modify -----------------------------------------------------------------

type modifyArgs struct {
	EntityID        uint64         `json:"entity_id"`
	PropertiesPatch map[string]any `json:"properties_patch"`
}

// Modify applies a JSON merge patch to an existing entity.
func Modify() Tool {
	return Tool{
		Name:        "Modify",
		Description: "Apply an RFC 7396 JSON merge patch to an entity's properties. Null values delete keys.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id":        map[string]any{"type": "integer", "description": "id of the entity to modify"},
				"properties_patch": map[string]any{"type": "object", "description": "RFC 7396 merge patch"},
			},
			"required": []string{"entity_id", "properties_patch"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a modifyArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Modify: bad args: %w", err)
			}
			if err := w.Modify(agent, world.EntityID(a.EntityID), world.Properties(a.PropertiesPatch)); err != nil {
				return "", err
			}
			return fmt.Sprintf("modified #%d", a.EntityID), nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			candidates := nonAgentEntities(p.AliveEntities)
			if len(candidates) == 0 {
				return nil, false
			}
			e := candidates[rng.IntN(len(candidates))]
			patch := map[string]any{
				pick(rng, []string{"colour", "mood", "age", "note"}): pick(rng, modifyValues),
			}
			b, err := json.Marshal(modifyArgs{EntityID: e.ID, PropertiesPatch: patch})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Destroy ----------------------------------------------------------------

type destroyArgs struct {
	EntityID uint64 `json:"entity_id"`
}

// Destroy soft-deletes an entity. Relationships referencing it
// cascade-soft-delete.
func Destroy() Tool {
	return Tool{
		Name:        "Destroy",
		Description: "Soft-delete an entity. It and its relationships are kept in history for replay.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id": map[string]any{"type": "integer"},
			},
			"required": []string{"entity_id"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a destroyArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Destroy: bad args: %w", err)
			}
			if err := w.Destroy(agent, world.EntityID(a.EntityID)); err != nil {
				return "", err
			}
			return fmt.Sprintf("destroyed #%d", a.EntityID), nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			candidates := nonAgentEntities(p.AliveEntities)
			if len(candidates) == 0 {
				return nil, false
			}
			e := candidates[rng.IntN(len(candidates))]
			b, err := json.Marshal(destroyArgs{EntityID: e.ID})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Relate -----------------------------------------------------------------

type relateArgs struct {
	From uint64 `json:"from_id"`
	To   uint64 `json:"to_id"`
	Kind string `json:"kind"`
}

// Relate declares a relationship between two live entities.
func Relate() Tool {
	return Tool{
		Name:        "Relate",
		Description: "Declare a relationship between two entities. The kind is a free-form string; the simulation does not interpret it.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_id": map[string]any{"type": "integer"},
				"to_id":   map[string]any{"type": "integer"},
				"kind":    map[string]any{"type": "string"},
			},
			"required": []string{"from_id", "to_id", "kind"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a relateArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Relate: bad args: %w", err)
			}
			id, err := w.Relate(agent, world.EntityID(a.From), world.EntityID(a.To), a.Kind)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("relation #%d: #%d -[%s]-> #%d", id, a.From, a.Kind, a.To), nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			if len(p.AliveEntities) < 2 {
				return nil, false
			}
			i := rng.IntN(len(p.AliveEntities))
			j := rng.IntN(len(p.AliveEntities) - 1)
			if j >= i {
				j++
			}
			b, err := json.Marshal(relateArgs{
				From: p.AliveEntities[i].ID,
				To:   p.AliveEntities[j].ID,
				Kind: pick(rng, []string{"part of", "borders", "child of", "knows", "made by"}),
			})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Unrelate ---------------------------------------------------------------

type unrelateArgs struct {
	RelID uint64 `json:"relationship_id"`
}

// Unrelate soft-deletes an existing relationship.
func Unrelate() Tool {
	return Tool{
		Name:        "Unrelate",
		Description: "Soft-delete an existing relationship.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"relationship_id": map[string]any{"type": "integer"},
			},
			"required": []string{"relationship_id"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a unrelateArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Unrelate: bad args: %w", err)
			}
			if err := w.Unrelate(agent, world.RelationshipID(a.RelID)); err != nil {
				return "", err
			}
			return fmt.Sprintf("unrelated #%d", a.RelID), nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			if len(p.AliveRelationships) == 0 {
				return nil, false
			}
			r := p.AliveRelationships[rng.IntN(len(p.AliveRelationships))]
			b, err := json.Marshal(unrelateArgs{RelID: r.ID})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Observe ----------------------------------------------------------------

type observeArgs struct {
	TypeLabel string `json:"type_label,omitempty"`
}

// Observe surveys the world. Returns a textual summary of matching
// entities. Supports an optional type_label filter; richer queries
// can be added later without changing the tool name.
func Observe() Tool {
	return Tool{
		Name:        "Observe",
		Description: "Survey the world. Optionally filter by type_label. Returns a short summary; does not mutate.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type_label": map[string]any{"type": "string", "description": "optional filter"},
			},
		},
		Apply: func(w *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a observeArgs
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("Observe: bad args: %w", err)
				}
			}
			ents := w.Entities()
			matched := 0
			for _, e := range ents {
				if a.TypeLabel == "" || e.TypeLabel == a.TypeLabel {
					matched++
				}
			}
			if a.TypeLabel == "" {
				return fmt.Sprintf("observed: %d live entities, %d live relationships", matched, w.RelationshipCount()), nil
			}
			return fmt.Sprintf("observed: %d live entities of type %q", matched, a.TypeLabel), nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			if rng.IntN(2) == 0 || len(p.AliveEntities) == 0 {
				return json.RawMessage(`{}`), true
			}
			e := p.AliveEntities[rng.IntN(len(p.AliveEntities))]
			b, err := json.Marshal(observeArgs{TypeLabel: e.TypeLabel})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- Reflect ----------------------------------------------------------------

type reflectArgs struct {
	Content string `json:"content"`
}

// Reflect writes a high-importance memory record. The world is not
// affected. Returns the content and routes it into the memory
// stream.
func Reflect() Tool {
	return Tool{
		Name:        "Reflect",
		Description: "Write a high-importance memory. Does not affect the world.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"content"},
		},
		Apply: func(_ *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a reflectArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Reflect: bad args: %w", err)
			}
			if a.Content == "" {
				return "", errors.New("Reflect: content must be non-empty")
			}
			return "reflected: " + a.Content, nil
		},
		RandomArgs: func(rng *rand.Rand, _ brain.Perception) (json.RawMessage, bool) {
			b, err := json.Marshal(reflectArgs{Content: pick(rng, reflections)})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- SpawnAgent -------------------------------------------------------------

// SpawnAgentArgs is the public arg struct so the sim adapter can
// decode the call before forwarding to its multi-agent runtime.
type SpawnAgentArgs struct {
	Name         string         `json:"name"`
	SystemPrompt string         `json:"system_prompt"`
	BrainConfig  map[string]any `json:"brain_config,omitempty"`
	GrantedTools []string       `json:"granted_tools,omitempty"`
}

// alias for backwards compatibility within this file.
type spawnAgentArgs = SpawnAgentArgs

// SpawnAgent creates an agent-entity and attaches a brain to it.
func SpawnAgent() Tool {
	return Tool{
		Name:        "SpawnAgent",
		Description: "Create an agent-entity. The sim layer attaches a brain when it intercepts the call; standalone Apply only creates the entity.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":          map[string]any{"type": "string"},
				"system_prompt": map[string]any{"type": "string"},
				"brain_config":  map[string]any{"type": "object"},
				"granted_tools": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"name", "system_prompt"},
		},
		Apply: func(w *world.World, agent world.AgentID, raw json.RawMessage) (string, error) {
			var a spawnAgentArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("SpawnAgent: bad args: %w", err)
			}
			props := world.Properties{
				"name":          a.Name,
				"system_prompt": a.SystemPrompt,
			}
			if len(a.BrainConfig) > 0 {
				props["brain_config"] = a.BrainConfig
			}
			if len(a.GrantedTools) > 0 {
				props["granted_tools"] = a.GrantedTools
			}
			id, err := w.Create(agent, "agent", props)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("spawned agent #%d (entity only - sim attaches the brain)", id), nil
		},
		// No RandomArgs: the stub brain shouldn't spawn agents.
		RandomArgs: nil,
	}
}

// ---- Speak ------------------------------------------------------------------

// SpeakArgs is the public arg struct so the sim adapter can decode
// the call before forwarding to the per-world inbox.
type SpeakArgs struct {
	Content string `json:"content"`
}

// Speak broadcasts a string to every other agent in the world. The
// sim layer intercepts this tool; its Apply func here is a no-op
// safety stub.
func Speak() Tool {
	return Tool{
		Name:        "Speak",
		Description: "Broadcast a string to every other agent in this world. They will hear it on their next tick.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string", "description": "what to say"},
			},
			"required": []string{"content"},
		},
		Apply: func(_ *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a SpeakArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Speak: bad args: %w", err)
			}
			if a.Content == "" {
				return "", errors.New("Speak: content must be non-empty")
			}
			// The sim normally intercepts Speak. If the tool is
			// invoked through the registry directly (e.g. tests), we
			// return the content; no broadcast happens.
			return "(spoke without sim broadcast) " + a.Content, nil
		},
		// No RandomArgs: the stub brain shouldn't spam Speak.
		RandomArgs: nil,
	}
}

// ---- Wait -------------------------------------------------------------------

type waitArgs struct {
	N uint64 `json:"n"`
}

// Wait skips n ticks without acting.
func Wait() Tool {
	return Tool{
		Name:        "Wait",
		Description: "Skip n ticks without acting. Useful for bundling time when there is nothing to do.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "integer", "minimum": 1},
			},
			"required": []string{"n"},
		},
		Apply: func(_ *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a waitArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Wait: bad args: %w", err)
			}
			if a.N == 0 {
				a.N = 1
			}
			return fmt.Sprintf("waited %d ticks", a.N), nil
		},
		RandomArgs: func(rng *rand.Rand, _ brain.Perception) (json.RawMessage, bool) {
			b, err := json.Marshal(waitArgs{N: uint64(rng.IntN(3) + 1)})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

// ---- shared fixtures for RandomArgs -----------------------------------------

func pick[T any](rng *rand.Rand, xs []T) T {
	return xs[rng.IntN(len(xs))]
}

// nonAgentEntities filters out entities whose type_label is "agent"
// so the stub brain never targets another agent (including its own
// entity) with Modify or Destroy. Real brains can decide for
// themselves.
func nonAgentEntities(in []brain.EntityView) []brain.EntityView {
	out := in[:0:0]
	for _, e := range in {
		if e.TypeLabel == "agent" {
			continue
		}
		out = append(out, e)
	}
	return out
}

var (
	planetNames    = []string{"Erith", "Pelor", "Vor", "Aezir", "Khel", "Solenne"}
	continentNames = []string{"Rho", "Vael", "Mira", "Sokun", "Nereth"}
	starNames      = []string{"Helios", "Anwen", "Caelum", "Lumin", "Vesper"}
	ideaPhrases    = []string{"there should be light", "warmth attracts life", "all things fall toward the centre", "kindness is heritable"}
	reflections    = []string{
		"the world is taking shape",
		"these things feel related but not yet named",
		"there is still nothing alive",
		"the surface is too smooth - it needs detail",
	}
	modifyValues = []any{"red", "calm", "ancient", "humming", float64(42), true}
)

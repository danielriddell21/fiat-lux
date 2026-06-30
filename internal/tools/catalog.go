package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

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
		DefineTool(),
		Die(),
		Wait(),
		FindByType(),
		FindByProperty(),
		FindRelated(),
		Zoom(),
		Unzoom(),
	)
}

type createArgs struct {
	TypeLabel  string         `json:"type_label"`
	Properties map[string]any `json:"properties,omitempty"`
}

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
				return "", fmt.Errorf("create entity: %w", err)
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

type modifyArgs struct {
	EntityID        uint64         `json:"entity_id"`
	PropertiesPatch map[string]any `json:"properties_patch"`
}

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
				return "", fmt.Errorf("modify entity: %w", err)
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

type destroyArgs struct {
	EntityID uint64 `json:"entity_id"`
}

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
				return "", fmt.Errorf("destroy entity: %w", err)
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

type relateArgs struct {
	From uint64 `json:"from_id"`
	To   uint64 `json:"to_id"`
	Kind string `json:"kind"`
}

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
				return "", fmt.Errorf("relate entities: %w", err)
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

type unrelateArgs struct {
	RelID uint64 `json:"relationship_id"`
}

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
				return "", fmt.Errorf("unrelate: %w", err)
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

type observeArgs struct {
	TypeLabel string `json:"type_label,omitempty"`
}

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

type reflectArgs struct {
	Content string `json:"content"`
}

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

type SpawnAgentArgs struct {
	Name         string             `json:"name"`
	SystemPrompt string             `json:"system_prompt"`
	BrainConfig  map[string]any     `json:"brain_config,omitempty"`
	GrantedTools []string           `json:"granted_tools,omitempty"`
	Drives       map[string]float64 `json:"drives,omitempty"`

	InheritMacros *bool `json:"inherit_macros,omitempty"`
}

type spawnAgentArgs = SpawnAgentArgs

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
				"drives": map[string]any{
					"type":                 "object",
					"description":          "intrinsic motivational state - named float weights; if omitted, the child inherits the parent's drives",
					"additionalProperties": map[string]any{"type": "number"},
				},
				"inherit_macros": map[string]any{
					"type":        "boolean",
					"description": "when true (the default), the child inherits every macro the parent has defined",
				},
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
				return "", fmt.Errorf("spawn agent: %w", err)
			}
			return fmt.Sprintf("spawned agent #%d (entity only - sim attaches the brain)", id), nil
		},
		// No RandomArgs: the stub brain shouldn't spawn agents.
		RandomArgs: nil,
	}
}

type SpeakArgs struct {
	Content string `json:"content"`
}

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

type DefineToolArgs struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Params      []string         `json:"params,omitempty"`
	Steps       []DefineToolStep `json:"steps"`
}

type DefineToolStep struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

func DefineTool() Tool {
	return Tool{
		Name: "DefineTool",
		Description: "Author a named macro - a sequence of existing tool calls with " +
			"{{param}} substitution. The macro appears as a new tool in subsequent ticks.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"params":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"tool": map[string]any{"type": "string"},
							"args": map[string]any{"type": "object"},
						},
						"required": []string{"tool"},
					},
				},
			},
			"required": []string{"name", "steps"},
		},
		Apply: func(_ *world.World, _ world.AgentID, _ json.RawMessage) (string, error) {
			return "", errors.New("DefineTool: must be intercepted by sim layer")
		},
		RandomArgs: nil,
	}
}

type DieArgs struct {
	Final string `json:"final,omitempty"`
}

func Die() Tool {
	return Tool{
		Name:        "Die",
		Description: "End your existence. Direct children inherit a digest of your memories. Use sparingly; the world does not bring you back.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"final": map[string]any{"type": "string", "description": "optional last words recorded with the death event"},
			},
		},
		Apply: func(_ *world.World, _ world.AgentID, _ json.RawMessage) (string, error) {
			return "", errors.New("Die: must be intercepted by sim layer")
		},
		RandomArgs: nil,
	}
}

type waitArgs struct {
	N uint64 `json:"n"`
}

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

type ZoomArgs struct {
	EntityID uint64 `json:"entity_id"`
	Turns    int    `json:"turns,omitempty"`
}

func Zoom() Tool {
	return Tool{
		Name:        "Zoom",
		Description: "Pin focus on an entity for several turns. Perception will then include that entity's contains sub-tree so you can drill into it. The agent's global view is preserved.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id": map[string]any{"type": "integer", "description": "entity to focus on"},
				"turns":     map[string]any{"type": "integer", "description": "turns to keep focus pinned; default 5, max 20"},
			},
			"required": []string{"entity_id"},
		},
		Apply: func(_ *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a ZoomArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("Zoom: bad args: %w", err)
			}
			if a.EntityID == 0 {
				return "", errors.New("Zoom: entity_id must be > 0")
			}
			return "(zoom requires the sim layer)", nil
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			containers := containerEntities(p.AliveEntities, p.AliveRelationships)
			if len(containers) == 0 {
				return nil, false
			}
			id := containers[rng.IntN(len(containers))]
			b, err := json.Marshal(ZoomArgs{EntityID: id, Turns: rng.IntN(5) + 3})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

func Unzoom() Tool {
	return Tool{
		Name:        "Unzoom",
		Description: "Release the agent's current focus before its turns expire. No-op if no focus is set.",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Apply: func(_ *world.World, _ world.AgentID, _ json.RawMessage) (string, error) {
			return "(unzoom requires the sim layer)", nil
		},
		RandomArgs: nil,
	}
}

type findByTypeArgs struct {
	TypeLabel string `json:"type_label"`
}

type findHit struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

func FindByType() Tool {
	return Tool{
		Name:        "FindByType",
		Description: "Read-only. List every live entity whose type matches. Returns JSON: {\"matches\":[{id,type,name}, ...],\"count\":N}.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type_label": map[string]any{"type": "string", "description": "exact type label to match"},
			},
			"required": []string{"type_label"},
		},
		Apply: func(w *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a findByTypeArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("FindByType: bad args: %w", err)
			}
			if a.TypeLabel == "" {
				return "", errors.New("FindByType: type_label must be non-empty")
			}
			hits := []findHit{}
			for _, e := range w.Entities() {
				if e.TypeLabel != a.TypeLabel {
					continue
				}
				hits = append(hits, findHit{ID: uint64(e.ID), Type: e.TypeLabel, Name: stringProp(e.Properties, "name")})
			}
			return encodeMatches(hits)
		},
		RandomArgs: func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
			if len(p.AliveEntities) == 0 {
				return nil, false
			}
			e := p.AliveEntities[rng.IntN(len(p.AliveEntities))]
			b, err := json.Marshal(findByTypeArgs{TypeLabel: e.TypeLabel})
			if err != nil {
				return nil, false
			}
			return b, true
		},
	}
}

type findByPropertyArgs struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func FindByProperty() Tool {
	return Tool{
		Name:        "FindByProperty",
		Description: "Read-only. List every live entity whose property matches a key/value pair. Returns JSON: {\"matches\":[{id,type,name}, ...],\"count\":N}.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":   map[string]any{"type": "string", "description": "property key to compare"},
				"value": map[string]any{"description": "value to match; strings, numbers, and booleans supported"},
			},
			"required": []string{"key", "value"},
		},
		Apply: func(w *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a findByPropertyArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("FindByProperty: bad args: %w", err)
			}
			return findByProperty(w, a)
		},
		RandomArgs: randomFindByPropertyArgs,
	}
}

func findByProperty(w *world.World, a findByPropertyArgs) (string, error) {
	if a.Key == "" {
		return "", errors.New("FindByProperty: key must be non-empty")
	}
	hits := []findHit{}
	for _, e := range w.Entities() {
		got, ok := e.Properties[a.Key]
		if !ok || !propsEqual(got, a.Value) {
			continue
		}
		hits = append(hits, findHit{ID: uint64(e.ID), Type: e.TypeLabel, Name: stringProp(e.Properties, "name")})
	}
	return encodeMatches(hits)
}

func randomFindByPropertyArgs(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
	for tries := 0; tries < 4 && len(p.AliveEntities) > 0; tries++ {
		e := p.AliveEntities[rng.IntN(len(p.AliveEntities))]
		if len(e.Properties) == 0 {
			continue
		}
		keys := make([]string, 0, len(e.Properties))
		for k := range e.Properties {
			keys = append(keys, k)
		}
		k := keys[rng.IntN(len(keys))]
		b, err := json.Marshal(findByPropertyArgs{Key: k, Value: e.Properties[k]})
		if err == nil {
			return b, true
		}
	}
	return nil, false
}

type findRelatedArgs struct {
	EntityID uint64 `json:"entity_id"`
	Kind     string `json:"kind,omitempty"`
}

type relatedHit struct {
	ID        uint64 `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind"`
	Direction string `json:"direction"`
}

func FindRelated() Tool {
	return Tool{
		Name:        "FindRelated",
		Description: "Read-only. List entities related to entity_id. Optional kind filter. Returns JSON: {\"matches\":[{id,type,name,kind,direction}, ...],\"count\":N}.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id": map[string]any{"type": "integer", "description": "id of the entity whose relationships to inspect"},
				"kind":      map[string]any{"type": "string", "description": "optional relationship-kind filter"},
			},
			"required": []string{"entity_id"},
		},
		Apply: func(w *world.World, _ world.AgentID, raw json.RawMessage) (string, error) {
			var a findRelatedArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("FindRelated: bad args: %w", err)
			}
			return findRelated(w, a)
		},
		RandomArgs: randomFindRelatedArgs,
	}
}

func findRelated(w *world.World, a findRelatedArgs) (string, error) {
	if a.EntityID == 0 {
		return "", errors.New("FindRelated: entity_id must be > 0")
	}
	target := world.EntityID(a.EntityID)
	byID := make(map[world.EntityID]world.Entity)
	for _, e := range w.Entities() {
		byID[e.ID] = e
	}
	hits := []relatedHit{}
	for _, r := range w.Relationships() {
		if hit, ok := relatedHitFor(r, target, a.Kind, byID); ok {
			hits = append(hits, hit)
		}
	}
	out := struct {
		Matches []relatedHit `json:"matches"`
		Count   int          `json:"count"`
	}{Matches: hits, Count: len(hits)}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("FindRelated: encode result: %w", err)
	}
	return string(b), nil
}

func relatedHitFor(r world.Relationship, target world.EntityID, kind string, byID map[world.EntityID]world.Entity) (relatedHit, bool) {
	var otherID world.EntityID
	var direction string
	switch {
	case r.From == target:
		otherID, direction = r.To, "outgoing"
	case r.To == target:
		otherID, direction = r.From, "incoming"
	default:
		return relatedHit{}, false
	}
	if kind != "" && r.Kind != kind {
		return relatedHit{}, false
	}
	other, ok := byID[otherID]
	if !ok {
		return relatedHit{}, false
	}
	return relatedHit{
		ID:        uint64(other.ID),
		Type:      other.TypeLabel,
		Name:      stringProp(other.Properties, "name"),
		Kind:      r.Kind,
		Direction: direction,
	}, true
}

func randomFindRelatedArgs(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool) {
	if len(p.AliveEntities) == 0 {
		return nil, false
	}
	e := p.AliveEntities[rng.IntN(len(p.AliveEntities))]
	args := findRelatedArgs{EntityID: e.ID}
	if len(p.AliveRelationships) > 0 && rng.IntN(2) == 0 {
		args.Kind = p.AliveRelationships[rng.IntN(len(p.AliveRelationships))].Kind
	}
	b, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return b, true
}

func encodeMatches(hits []findHit) (string, error) {
	out := struct {
		Matches []findHit `json:"matches"`
		Count   int       `json:"count"`
	}{Matches: hits, Count: len(hits)}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshal matches: %w", err)
	}
	return string(b), nil
}

func stringProp(p world.Properties, key string) string {
	if p == nil {
		return ""
	}
	if s, ok := p[key].(string); ok {
		return s
	}
	return ""
}

func propsEqual(got, want any) bool {
	wb, err := json.Marshal(want)
	if err != nil {
		return false
	}
	gb, err := json.Marshal(got)
	if err != nil {
		return false
	}
	return string(wb) == string(gb)
}

func pick[T any](rng *rand.Rand, xs []T) T {
	return xs[rng.IntN(len(xs))]
}

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

func containerEntities(ents []brain.EntityView, rels []brain.RelationshipView) []uint64 {
	hasChild := make(map[uint64]bool, len(ents))
	for _, r := range rels {
		if !strings.EqualFold(r.Kind, "contains") {
			continue
		}
		hasChild[r.From] = true
	}
	out := make([]uint64, 0, len(hasChild))
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			continue
		}
		if hasChild[e.ID] {
			out = append(out, e.ID)
		}
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

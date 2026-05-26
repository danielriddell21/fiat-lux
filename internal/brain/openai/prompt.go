package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

// RenderPerception turns the agent's Perception into a compact JSON
// payload the model can read. JSON is more reliable than prose for
// our use - the model is asked to make a single tool call, and the
// keys are stable and machine-readable.
//
// We deliberately keep the structure terse: no narrative scaffolding,
// no examples. The system prompt instructs the model to issue at
// most one tool call with a brief justification; the user message
// is purely current state.
func RenderPerception(p brain.Perception) string {
	type rendered struct {
		Tick          uint64              `json:"tick"`
		EntityCount   int                 `json:"entity_count"`
		Drives        map[string]float64  `json:"drives,omitempty"`
		Entities      []entitySummary     `json:"entities,omitempty"`
		Relationships []relSummary        `json:"relationships,omitempty"`
		RecentEvents  []eventSummary      `json:"recent_events,omitempty"`
		Memories      []memorySummary     `json:"memories,omitempty"`
		Focus         *focusSummary       `json:"focus,omitempty"`
		Frontier      *frontierSummary    `json:"frontier,omitempty"`
		Suggestions   []suggestionSummary `json:"spawn_suggestions,omitempty"`
	}
	r := rendered{
		Tick:        p.Tick,
		EntityCount: p.EntityCount,
		Drives:      p.Drives,
	}
	for _, e := range p.AliveEntities {
		r.Entities = append(r.Entities, entitySummary{
			ID: e.ID, Type: e.TypeLabel, Props: e.Properties,
		})
	}
	for _, rel := range p.AliveRelationships {
		r.Relationships = append(r.Relationships, relSummary{
			ID: rel.ID, From: rel.From, To: rel.To, Kind: rel.Kind,
		})
	}
	for _, ev := range p.RecentEvents {
		s := ev.Summary
		if s == "" {
			s = ev.Kind
		}
		r.RecentEvents = append(r.RecentEvents, eventSummary{
			Tick: ev.Tick, Summary: s,
		})
	}
	for _, m := range p.Memories {
		r.Memories = append(r.Memories, memorySummary{
			Tick: m.Tick, Content: m.Content,
		})
	}
	r.Focus = renderFocus(p.Focus)
	r.Frontier = renderFrontier(p.Frontier)
	for _, s := range p.Suggestions {
		r.Suggestions = append(r.Suggestions, suggestionSummary{
			EntityID: s.EntityID, Type: s.TypeLabel,
			ChildCount: s.ChildCount, Reason: s.Reason,
		})
	}

	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("(perception render failed: %v)", err)
	}
	return string(b)
}

func renderFocus(f *brain.FocusView) *focusSummary {
	if f == nil {
		return nil
	}
	out := &focusSummary{
		EntityID:   f.EntityID,
		Type:       f.TypeLabel,
		TurnsLeft:  f.TurnsLeft,
		DepthBelow: f.DepthBelow,
	}
	for _, e := range f.Subtree {
		out.Subtree = append(out.Subtree, entitySummary{
			ID: e.ID, Type: e.TypeLabel, Props: e.Properties,
		})
	}
	for _, r := range f.SubRels {
		out.SubRels = append(out.SubRels, relSummary{
			ID: r.ID, From: r.From, To: r.To, Kind: r.Kind,
		})
	}
	return out
}

func renderFrontier(f brain.FrontierView) *frontierSummary {
	if len(f.Leaves) == 0 && len(f.DeepestPath) == 0 {
		return nil
	}
	out := &frontierSummary{}
	for _, l := range f.Leaves {
		out.Leaves = append(out.Leaves, leafSummary{ID: l.EntityID, Type: l.TypeLabel, Depth: l.Depth})
	}
	for _, s := range f.DeepestPath {
		out.DeepestPath = append(out.DeepestPath, stepSummary{ID: s.EntityID, Type: s.TypeLabel})
	}
	return out
}

type entitySummary struct {
	ID    uint64         `json:"id"`
	Type  string         `json:"type"`
	Props map[string]any `json:"properties,omitempty"`
}

type relSummary struct {
	ID   uint64 `json:"id"`
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
	Kind string `json:"kind"`
}

type eventSummary struct {
	Tick    uint64 `json:"tick"`
	Summary string `json:"event"`
}

type memorySummary struct {
	Tick    uint64 `json:"tick"`
	Content string `json:"content"`
}

type focusSummary struct {
	EntityID   uint64          `json:"entity_id"`
	Type       string          `json:"type"`
	TurnsLeft  int             `json:"turns_left"`
	DepthBelow int             `json:"depth_below"`
	Subtree    []entitySummary `json:"subtree,omitempty"`
	SubRels    []relSummary    `json:"subtree_relationships,omitempty"`
}

type frontierSummary struct {
	Leaves      []leafSummary `json:"leaves,omitempty"`
	DeepestPath []stepSummary `json:"deepest_path,omitempty"`
}

type leafSummary struct {
	ID    uint64 `json:"id"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
}

type stepSummary struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`
}

type suggestionSummary struct {
	EntityID   uint64 `json:"entity_id"`
	Type       string `json:"type"`
	ChildCount int    `json:"child_count"`
	Reason     string `json:"reason"`
}

// parseJSONFallback handles models that emit a tool call as plain
// text (older Ollama / llama.cpp / vLLM variants without reliable
// native tool calling). Expected shape:
//
//	{"tool":"<name>","args":{...}}  or
//	{"name":"<name>","arguments":{...}}
//
// Returns nil, false if the content is not a valid fallback object.
func parseJSONFallback(content string) (*brain.ToolCall, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || trimmed[0] != '{' {
		return nil, false
	}
	var fall struct {
		Tool      string          `json:"tool"`
		Name      string          `json:"name"`
		Args      json.RawMessage `json:"args"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(trimmed), &fall); err != nil {
		return nil, false
	}
	name := fall.Tool
	if name == "" {
		name = fall.Name
	}
	if name == "" {
		return nil, false
	}
	args := fall.Args
	if len(args) == 0 {
		args = fall.Arguments
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return &brain.ToolCall{Name: name, Args: args}, true
}

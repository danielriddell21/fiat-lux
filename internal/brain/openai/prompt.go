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
		Tick          uint64          `json:"tick"`
		EntityCount   int             `json:"entity_count"`
		Entities      []entitySummary `json:"entities,omitempty"`
		Relationships []relSummary    `json:"relationships,omitempty"`
		RecentEvents  []eventSummary  `json:"recent_events,omitempty"`
		Memories      []memorySummary `json:"memories,omitempty"`
	}
	r := rendered{
		Tick:        p.Tick,
		EntityCount: p.EntityCount,
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

	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("(perception render failed: %v)", err)
	}
	return string(b)
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

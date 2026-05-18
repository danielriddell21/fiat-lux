package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// ErrUnknownTool is returned by a Registry when a tool name is not
// registered.
var ErrUnknownTool = errors.New("tools: unknown tool")

// Apply is the runtime side of a Tool. It applies the JSON-encoded
// args to the given world on behalf of the named agent and returns a
// short human-readable result string for the agent's memory stream.
type Apply func(w *world.World, agent world.AgentID, args json.RawMessage) (string, error)

// RandomArgs generates plausible random args for stub-brain use,
// driven entirely from the agent's perception. The bool result is
// false when no valid args can be produced (e.g. Modify with no
// live entities); the stub falls back to another tool in that case.
type RandomArgs func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool)

// Tool is one entry in the agent's creation vocabulary. The schema
// is a JSON Schema object in Anthropic tool-use shape; each Brain
// implementation translates it to its provider's wire format.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Apply       Apply
	RandomArgs  RandomArgs
}

// Def returns the brain-facing ToolDef view of this tool.
func (t Tool) Def() brain.ToolDef {
	return brain.ToolDef{
		Name:        t.Name,
		Description: t.Description,
		Schema:      t.Schema,
	}
}

// Registry is a name-indexed collection of tools. The zero value is
// usable but empty; Default() returns the full catalogue.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry constructs a Registry containing the given tools. A
// duplicate name panics so the misconfiguration is caught at startup.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if _, exists := r.tools[t.Name]; exists {
			panic(fmt.Sprintf("tools: duplicate registration %q", t.Name))
		}
		r.tools[t.Name] = t
	}
	return r
}

// Get returns the tool with the given name. ok is false if no such
// tool is registered.
func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return Tool{}, false
	}
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool, sorted by name ascending. The
// returned slice is a copy.
func (r *Registry) All() []Tool {
	if r == nil {
		return nil
	}
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Defs returns the brain-facing tool definitions for every tool in
// the registry.
func (r *Registry) Defs() []brain.ToolDef {
	all := r.All()
	out := make([]brain.ToolDef, len(all))
	for i, t := range all {
		out[i] = t.Def()
	}
	return out
}

// Filter returns a new Registry containing only the named tools. If
// names is empty, the original registry's tools are all returned
// (a creator that grants no explicit subset gets the full vocabulary).
// Unknown names yield ErrUnknownTool.
func (r *Registry) Filter(names []string) (*Registry, error) {
	if r == nil {
		return nil, errors.New("tools: nil registry")
	}
	if len(names) == 0 {
		out := make([]Tool, 0, len(r.tools))
		for _, t := range r.tools {
			out = append(out, t)
		}
		return NewRegistry(out...), nil
	}
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		t, ok := r.tools[n]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTool, n)
		}
		out = append(out, t)
	}
	return NewRegistry(out...), nil
}

// Invoke parses and applies a single ToolCall.
func (r *Registry) Invoke(call brain.ToolCall, w *world.World, agent world.AgentID) (string, error) {
	t, ok := r.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownTool, call.Name)
	}
	if t.Apply == nil {
		return "", fmt.Errorf("tools: %q has no Apply func", call.Name)
	}
	return t.Apply(w, agent, call.Args)
}

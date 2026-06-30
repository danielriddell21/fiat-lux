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

var ErrUnknownTool = errors.New("tools: unknown tool")

type Apply func(w *world.World, agent world.AgentID, args json.RawMessage) (string, error)

type RandomArgs func(rng *rand.Rand, p brain.Perception) (json.RawMessage, bool)

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Apply       Apply
	RandomArgs  RandomArgs
}

func (t Tool) Def() brain.ToolDef {
	return brain.ToolDef{
		Name:        t.Name,
		Description: t.Description,
		Schema:      t.Schema,
	}
}

type Registry struct {
	tools map[string]Tool
}

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

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return Tool{}, false
	}
	t, ok := r.tools[name]
	return t, ok
}

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

func (r *Registry) Defs() []brain.ToolDef {
	all := r.All()
	out := make([]brain.ToolDef, len(all))
	for i, t := range all {
		out[i] = t.Def()
	}
	return out
}

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

package macros

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

const (
	MaxMacrosPerAgent = 16

	MaxStepsPerMacro = 8

	MaxExpansionDepth = 3
)

var forbiddenSteps = []string{"SpawnAgent", "DefineTool"}

type Step struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

type Macro struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Params      []string `json:"params,omitempty"`
	Steps       []Step   `json:"steps"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (m Macro) Validate(existingTools map[string]struct{}) error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("macros: invalid name %q (expected /[A-Za-z_][A-Za-z0-9_]*/)", m.Name)
	}
	for _, t := range forbiddenSteps {
		if m.Name == t {
			return fmt.Errorf("macros: name %q collides with reserved tool", m.Name)
		}
	}
	if len(m.Steps) == 0 {
		return errors.New("macros: at least one step is required")
	}
	if len(m.Steps) > MaxStepsPerMacro {
		return fmt.Errorf("macros: too many steps (%d > %d)", len(m.Steps), MaxStepsPerMacro)
	}
	for _, p := range m.Params {
		if !nameRe.MatchString(p) {
			return fmt.Errorf("macros: invalid param %q", p)
		}
	}
	for i, s := range m.Steps {
		if err := validateStep(i, s, existingTools); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(i int, s Step, existingTools map[string]struct{}) error {
	if s.Tool == "" {
		return fmt.Errorf("macros: step %d has empty tool", i)
	}
	if slices.Contains(forbiddenSteps, s.Tool) {
		return fmt.Errorf("macros: step %d uses forbidden tool %q", i, s.Tool)
	}
	if existingTools != nil {
		if _, ok := existingTools[s.Tool]; !ok {
			return fmt.Errorf("macros: step %d uses unknown tool %q", i, s.Tool)
		}
	}
	if len(s.Args) > 0 && !json.Valid(s.Args) {
		return fmt.Errorf("macros: step %d args are not valid JSON", i)
	}
	return nil
}

func (m Macro) Def() brain.ToolDef {
	props := make(map[string]any, len(m.Params))
	for _, p := range m.Params {
		props[p] = map[string]any{"type": "string"}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(m.Params) > 0 {
		req := make([]string, len(m.Params))
		copy(req, m.Params)
		schema["required"] = req
	}
	desc := m.Description
	if desc == "" {
		desc = fmt.Sprintf("agent-defined macro: %d step(s)", len(m.Steps))
	}
	return brain.ToolDef{Name: m.Name, Description: desc, Schema: schema}
}

func (m Macro) Expand(call brain.ToolCall, registry map[string]Macro, depth int) ([]brain.ToolCall, error) {
	if depth > MaxExpansionDepth {
		return nil, fmt.Errorf("macros: expansion depth %d exceeds limit %d", depth, MaxExpansionDepth)
	}
	if call.Name != m.Name {
		return nil, fmt.Errorf("macros: call %q does not match macro %q", call.Name, m.Name)
	}

	bindings := make(map[string]string, len(m.Params))
	if len(call.Args) > 0 && len(m.Params) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(call.Args, &raw); err != nil {
			return nil, fmt.Errorf("macros: %s: args not a JSON object: %w", m.Name, err)
		}
		for _, p := range m.Params {
			v, ok := raw[p]
			if !ok {
				return nil, fmt.Errorf("macros: %s: missing required param %q", m.Name, p)
			}
			bindings[p] = stringify(v)
		}
	} else if len(m.Params) > 0 {
		return nil, fmt.Errorf("macros: %s: missing required params %v", m.Name, m.Params)
	}

	out := make([]brain.ToolCall, 0, len(m.Steps))
	for i, step := range m.Steps {
		substituted := substitute(step.Args, bindings)
		sub := brain.ToolCall{Name: step.Tool, Args: substituted}
		// If this step names another macro, recurse.
		if nested, ok := registry[step.Tool]; ok && step.Tool != m.Name {
			expanded, err := nested.Expand(sub, registry, depth+1)
			if err != nil {
				return nil, fmt.Errorf("macros: %s step %d: %w", m.Name, i, err)
			}
			out = append(out, expanded...)
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

func substitute(template json.RawMessage, bindings map[string]string) json.RawMessage {
	if len(template) == 0 {
		return json.RawMessage(`{}`)
	}
	if len(bindings) == 0 {
		return slices.Clone(template)
	}
	s := string(template)
	for k, v := range bindings {
		quoted := `"{{` + k + `}}"`
		s = strings.ReplaceAll(s, quoted, v)
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return json.RawMessage(s)
}

func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

type Set struct {
	byName map[string]Macro
}

func NewSet() *Set { return &Set{byName: make(map[string]Macro)} }

func (s *Set) Clone() *Set {
	if s == nil {
		return nil
	}
	out := NewSet()
	out.byName = maps.Clone(s.byName)
	return out
}

func (s *Set) Add(m Macro, existingTools map[string]struct{}) error {
	if s == nil {
		return errors.New("macros: nil set")
	}
	if _, dup := s.byName[m.Name]; !dup && len(s.byName) >= MaxMacrosPerAgent {
		return fmt.Errorf("macros: cap reached (%d)", MaxMacrosPerAgent)
	}
	// Allow a macro to reference earlier macros it owns by name.
	tools := maps.Clone(existingTools)
	if tools == nil {
		tools = make(map[string]struct{}, len(s.byName))
	}
	for name := range s.byName {
		tools[name] = struct{}{}
	}
	if err := m.Validate(tools); err != nil {
		return err
	}
	s.byName[m.Name] = m
	return nil
}

func (s *Set) AddTrusted(m Macro) error {
	if s == nil {
		return errors.New("macros: nil set")
	}
	if _, dup := s.byName[m.Name]; !dup && len(s.byName) >= MaxMacrosPerAgent {
		return fmt.Errorf("macros: cap reached (%d)", MaxMacrosPerAgent)
	}
	s.byName[m.Name] = m
	return nil
}

func (s *Set) Get(name string) (Macro, bool) {
	if s == nil {
		return Macro{}, false
	}
	m, ok := s.byName[name]
	return m, ok
}

func (s *Set) All() []Macro {
	if s == nil || len(s.byName) == 0 {
		return nil
	}
	names := slices.Sorted(maps.Keys(s.byName))
	out := make([]Macro, len(names))
	for i, n := range names {
		out[i] = s.byName[n]
	}
	return out
}

func (s *Set) AsRegistry() map[string]Macro {
	if s == nil {
		return nil
	}
	return maps.Clone(s.byName)
}

func (s *Set) Defs() []brain.ToolDef {
	all := s.All()
	out := make([]brain.ToolDef, len(all))
	for i, m := range all {
		out[i] = m.Def()
	}
	return out
}

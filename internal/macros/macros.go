// Package macros implements runtime-defined tool macros: named
// sequences of existing primitive tool calls that the agent can
// author with the DefineTool tool. Macros are NOT a DSL - they
// expand to a list of ordinary brain.ToolCalls with {{param}}
// substitution into each step's args. This keeps the surface area
// small enough to validate, persist in the world event log, and
// inspect.
//
// Children inherit parent macros by default (set InheritMacros=false
// on SpawnAgent to opt out). Macros may not include SpawnAgent for
// v1 - the recursion footgun is too easy to misfire.
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

// Constraints. Tuned to keep macros simple and bound resource use.
const (
	// MaxMacrosPerAgent caps how many macros one agent may define.
	MaxMacrosPerAgent = 16

	// MaxStepsPerMacro caps the call sequence length of a single
	// macro.
	MaxStepsPerMacro = 8

	// MaxExpansionDepth caps recursive macro expansion: a macro
	// step that names another macro counts as one level of depth.
	MaxExpansionDepth = 3
)

// ForbiddenSteps are tool names that may not appear in a macro step.
// SpawnAgent is excluded because nested spawning multiplies state
// changes in ways the user expects to gate explicitly. The macro
// owner can still call SpawnAgent directly.
var forbiddenSteps = []string{"SpawnAgent", "DefineTool"}

// Step is one entry in a macro's call sequence. Args is a JSON object
// literal that may contain "{{param}}" tokens which are substituted
// from the macro invocation's args at expansion time.
type Step struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Macro is one runtime-defined tool the agent has authored. Name is
// the call name the brain will see in subsequent ticks; Description
// is surfaced verbatim to the brain. Params are the named placeholders
// {{name}} that callers must supply.
type Macro struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Params      []string `json:"params,omitempty"`
	Steps       []Step   `json:"steps"`
}

// nameRe restricts macro and param identifiers to a reasonable shape
// so the JSON schema we synthesise stays clean and substitution
// cannot collide with valid JSON syntax.
var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate checks the macro's shape independent of any registry.
// existingTools is the set of valid primitive (or already-defined
// macro) tool names; nil disables that check.
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

// validateStep checks one macro step's tool name (non-empty, not reserved, and
// known when existingTools is set) and that any args are valid JSON.
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

// Def returns the brain-facing definition (name, description, JSON
// schema) for this macro so it appears alongside primitive tools in
// the brain's tool list each tick.
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

// Expand turns one ToolCall against this macro into a sequence of
// primitive ToolCalls with {{param}} substituted from the invocation
// args. registry, when non-nil, allows recursive expansion of macros
// referenced by Steps. depth tracks recursion against MaxExpansionDepth.
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

// substitute replaces "{{param}}" tokens in a raw JSON args template
// with the corresponding binding values. Each binding is the JSON
// encoding of the value the caller passed (a string keeps its quotes;
// a number or object is its literal form). The quoted form
// "\"{{name}}\"" splices the literal directly so the result is still
// valid JSON; the bare form {{name}} does the same and is allowed
// anywhere a JSON value is expected. Returns "{}" when the template
// is empty.
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

// stringify renders an arbitrary JSON-decoded value as a JSON literal
// suitable for splicing into the macro's args template. Strings keep
// their quotes; numbers/bools/objects/arrays are marshalled.
func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// Set is a name-indexed bundle of macros owned by one agent. Safe
// for single-goroutine use; the sim layer holds the sync.
type Set struct {
	byName map[string]Macro
}

// NewSet returns an empty Set.
func NewSet() *Set { return &Set{byName: make(map[string]Macro)} }

// Clone returns a deep copy of the set. Nil-safe.
func (s *Set) Clone() *Set {
	if s == nil {
		return nil
	}
	out := NewSet()
	out.byName = maps.Clone(s.byName)
	return out
}

// Add registers a macro. Returns an error if the cap is exceeded or
// the macro fails validation against the existing tool names.
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

// AddTrusted inserts a macro without re-validating it. Use only for
// rehydration from a previously-validated source (e.g. replaying the
// world event log). Returns an error only if the cap is exceeded.
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

// Get returns the named macro and whether it was found.
func (s *Set) Get(name string) (Macro, bool) {
	if s == nil {
		return Macro{}, false
	}
	m, ok := s.byName[name]
	return m, ok
}

// All returns every macro sorted by name. Useful for prompt rendering
// and tests.
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

// AsRegistry returns the macros as a name->Macro map suitable for
// passing to Macro.Expand for recursive expansion.
func (s *Set) AsRegistry() map[string]Macro {
	if s == nil {
		return nil
	}
	return maps.Clone(s.byName)
}

// Defs returns brain-facing tool definitions for every macro.
func (s *Set) Defs() []brain.ToolDef {
	all := s.All()
	out := make([]brain.ToolDef, len(all))
	for i, m := range all {
		out[i] = m.Def()
	}
	return out
}

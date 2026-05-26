package macros

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

func validTools(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

func TestValidate_RejectsBadName(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "1bad", Steps: []Step{{Tool: "Create", Args: json.RawMessage(`{}`)}}}
	if err := m.Validate(validTools("Create")); err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestValidate_RejectsReserved(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "SpawnAgent", Steps: []Step{{Tool: "Create"}}}
	if err := m.Validate(validTools("Create", "SpawnAgent")); err == nil {
		t.Error("expected error for reserved name")
	}
}

func TestValidate_RejectsForbiddenStep(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "Bad", Steps: []Step{{Tool: "SpawnAgent", Args: json.RawMessage(`{}`)}}}
	if err := m.Validate(validTools("SpawnAgent")); err == nil {
		t.Error("expected error for forbidden step")
	}
}

func TestValidate_RejectsUnknownTool(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "X", Steps: []Step{{Tool: "Nope"}}}
	if err := m.Validate(validTools("Create")); err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestValidate_RejectsEmptySteps(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "X"}
	if err := m.Validate(validTools("Create")); err == nil {
		t.Error("expected error for empty steps")
	}
}

func TestExpand_SubstitutesParams(t *testing.T) {
	t.Parallel()
	m := Macro{
		Name:   "Twin",
		Params: []string{"label"},
		Steps: []Step{
			{Tool: "Create", Args: json.RawMessage(`{"type_label":"{{label}}","properties":{"twin":true}}`)},
			{Tool: "Create", Args: json.RawMessage(`{"type_label":"{{label}}","properties":{"twin":true}}`)},
		},
	}
	if err := m.Validate(validTools("Create")); err != nil {
		t.Fatal(err)
	}
	calls, err := m.Expand(brain.ToolCall{Name: "Twin", Args: json.RawMessage(`{"label":"star"}`)}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("expansions = %d, want 2", len(calls))
	}
	for i, c := range calls {
		if !strings.Contains(string(c.Args), `"type_label":"star"`) {
			t.Errorf("step %d args missing substituted value: %s", i, c.Args)
		}
	}
}

func TestExpand_MissingParamError(t *testing.T) {
	t.Parallel()
	m := Macro{
		Name: "X", Params: []string{"label"},
		Steps: []Step{{Tool: "Create", Args: json.RawMessage(`{"type_label":"{{label}}"}`)}},
	}
	_, err := m.Expand(brain.ToolCall{Name: "X", Args: json.RawMessage(`{}`)}, nil, 0)
	if err == nil {
		t.Error("expected missing-param error")
	}
}

func TestExpand_RespectsDepthLimit(t *testing.T) {
	t.Parallel()
	// Two macros that reference each other - should hit the limit.
	a := Macro{Name: "A", Steps: []Step{{Tool: "B", Args: json.RawMessage(`{}`)}}}
	b := Macro{Name: "B", Steps: []Step{{Tool: "A", Args: json.RawMessage(`{}`)}}}
	reg := map[string]Macro{"A": a, "B": b}
	_, err := a.Expand(brain.ToolCall{Name: "A", Args: json.RawMessage(`{}`)}, reg, 0)
	if err == nil {
		t.Error("expected depth-limit error")
	}
}

func TestExpand_RecursivelyExpandsNestedMacro(t *testing.T) {
	t.Parallel()
	leaf := Macro{
		Name: "Leaf",
		Steps: []Step{
			{Tool: "Create", Args: json.RawMessage(`{"type_label":"leaf"}`)},
		},
	}
	parent := Macro{
		Name:  "Parent",
		Steps: []Step{{Tool: "Leaf", Args: json.RawMessage(`{}`)}},
	}
	reg := map[string]Macro{"Leaf": leaf, "Parent": parent}
	calls, err := parent.Expand(brain.ToolCall{Name: "Parent", Args: json.RawMessage(`{}`)}, reg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "Create" {
		t.Errorf("expanded calls = %+v, want [Create]", calls)
	}
}

func TestSet_AddRespectsCap(t *testing.T) {
	t.Parallel()
	s := NewSet()
	for i := range MaxMacrosPerAgent {
		m := Macro{Name: nameForIndex(i), Steps: []Step{{Tool: "Create"}}}
		if err := s.Add(m, validTools("Create")); err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
	}
	overflow := Macro{Name: "Z_overflow", Steps: []Step{{Tool: "Create"}}}
	if err := s.Add(overflow, validTools("Create")); err == nil {
		t.Error("expected cap-exceeded error")
	}
}

func TestSet_AddIsIdempotentOnUpdate(t *testing.T) {
	t.Parallel()
	s := NewSet()
	m := Macro{Name: "X", Steps: []Step{{Tool: "Create"}}}
	if err := s.Add(m, validTools("Create")); err != nil {
		t.Fatal(err)
	}
	// Same name should replace, not bump the count.
	m2 := Macro{Name: "X", Description: "updated", Steps: []Step{{Tool: "Modify"}}}
	if err := s.Add(m2, validTools("Create", "Modify")); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("X")
	if got.Description != "updated" {
		t.Errorf("Description = %q, want updated", got.Description)
	}
}

func TestSet_AddAllowsReferencingPriorMacro(t *testing.T) {
	t.Parallel()
	s := NewSet()
	if err := s.Add(Macro{Name: "Leaf", Steps: []Step{{Tool: "Create"}}}, validTools("Create")); err != nil {
		t.Fatal(err)
	}
	parent := Macro{Name: "Parent", Steps: []Step{{Tool: "Leaf"}}}
	if err := s.Add(parent, validTools("Create")); err != nil {
		t.Errorf("expected parent to reference Leaf: %v", err)
	}
}

func TestSet_DefsSortedByName(t *testing.T) {
	t.Parallel()
	s := NewSet()
	tools := validTools("Create")
	_ = s.Add(Macro{Name: "Zeta", Steps: []Step{{Tool: "Create"}}}, tools)
	_ = s.Add(Macro{Name: "Alpha", Steps: []Step{{Tool: "Create"}}}, tools)
	defs := s.Defs()
	if len(defs) != 2 || defs[0].Name != "Alpha" || defs[1].Name != "Zeta" {
		t.Errorf("defs = %+v, want sorted [Alpha, Zeta]", defs)
	}
}

func nameForIndex(i int) string {
	letters := []byte("abcdefghijklmnopqrstuvwxyz")
	return "Macro_" + string(letters[i%len(letters)]) + string(letters[(i/len(letters))%len(letters)])
}

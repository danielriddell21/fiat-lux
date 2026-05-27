package sim

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func makeSim(t *testing.T, name string, br brain.Brain) *Sim {
	t.Helper()
	w, err := world.New(name)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{World: w, Brain: br})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUniverse_StepRotatesAcrossWorlds(t *testing.T) {
	t.Parallel()
	// Each brain returns a Create so the sim never enters its
	// skip-tick path; we want to verify rotation, not skip logic.
	createDec := brain.Decision{
		ToolCall: &brain.ToolCall{Name: "Create", Args: jsonRaw(`{"type_label":"thing"}`)},
	}
	bA := &scriptedBrain{decisions: []brain.Decision{createDec}}
	bB := &scriptedBrain{decisions: []brain.Decision{createDec}}
	sA := makeSim(t, "kosmos", bA)
	sB := makeSim(t, "anima", bB)

	u, err := NewUniverse([]*Sim{sA, sB})
	if err != nil {
		t.Fatal(err)
	}
	if u.FocusedIdx() != 0 {
		t.Errorf("initial focus = %d, want 0", u.FocusedIdx())
	}

	// Three Steps in round-robin: A, B, A.
	if _, err := u.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bA.idx != 2 {
		t.Errorf("bA invoked %d times, want 2", bA.idx)
	}
	if bB.idx != 1 {
		t.Errorf("bB invoked %d times, want 1", bB.idx)
	}
}

func TestUniverse_FocusCycling(t *testing.T) {
	t.Parallel()
	sA := makeSim(t, "a", &scriptedBrain{decisions: []brain.Decision{{Thought: "x"}}})
	sB := makeSim(t, "b", &scriptedBrain{decisions: []brain.Decision{{Thought: "x"}}})
	sC := makeSim(t, "c", &scriptedBrain{decisions: []brain.Decision{{Thought: "x"}}})
	u, _ := NewUniverse([]*Sim{sA, sB, sC})

	u.CycleFocus(+1)
	if u.FocusedIdx() != 1 {
		t.Errorf("after +1: focused = %d, want 1", u.FocusedIdx())
	}
	u.CycleFocus(+1)
	if u.FocusedIdx() != 2 {
		t.Errorf("after +2: focused = %d, want 2", u.FocusedIdx())
	}
	u.CycleFocus(+1)
	if u.FocusedIdx() != 0 {
		t.Errorf("after wrap: focused = %d, want 0", u.FocusedIdx())
	}
	u.CycleFocus(-1)
	if u.FocusedIdx() != 2 {
		t.Errorf("backward wrap: focused = %d, want 2", u.FocusedIdx())
	}
}

func TestUniverse_AgentsCannotPerceiveAcrossWorlds(t *testing.T) {
	t.Parallel()
	bA := &scriptedBrain{decisions: []brain.Decision{{Thought: "noop"}}}
	bB := &scriptedBrain{decisions: []brain.Decision{{Thought: "noop"}}}
	sA := makeSim(t, "kosmos", bA)
	sB := makeSim(t, "anima", bB)

	// Create an entity in A.
	if _, err := sA.World.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"}); err != nil {
		t.Fatal(err)
	}

	u, _ := NewUniverse([]*Sim{sA, sB})
	// Tick A: bA should see the planet in its perception.
	if _, err := u.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Tick B: bB's perception must NOT contain the planet.
	if _, err := u.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	foundPlanetInB := false
	for _, e := range bB.seen.AliveEntities {
		if e.TypeLabel == "planet" {
			foundPlanetInB = true
		}
	}
	if foundPlanetInB {
		t.Errorf("world B's agent perceived world A's planet; worlds must be isolated")
	}
}

func TestLoadYAML_ValidConfig(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      name: the creator
      system_prompt: be brief
      brain:
        provider: stub
  - name: anima
    agent:
      brain:
        spec: stub
reflect_interval: 25
max_agents: 5
max_spawn_depth: 2
`
	cfg, err := LoadYAML(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Worlds) != 2 {
		t.Errorf("len(Worlds) = %d, want 2", len(cfg.Worlds))
	}
	if cfg.Worlds[0].Agent.Brain.resolveSpec() != "stub" {
		t.Errorf("provider-only spec did not resolve to stub")
	}
	if cfg.Worlds[1].Agent.Brain.resolveSpec() != "stub" {
		t.Errorf("explicit spec did not resolve")
	}
}

func TestLoadYAML_RejectsEmptyWorlds(t *testing.T) {
	t.Parallel()
	_, err := LoadYAML(strings.NewReader(`reflect_interval: 1`))
	if err == nil {
		t.Errorf("expected error for missing worlds")
	}
}

func TestMultimodalConfig_Spec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  MultimodalConfig
		want string
	}{
		{"disabled", MultimodalConfig{}, ""},
		{"openai bare", MultimodalConfig{Provider: "openai"}, "openai"},
		{"openai with model", MultimodalConfig{Provider: "openai", Model: "dall-e-3"}, "openai:dall-e-3"},
		{
			"openaicompat full",
			MultimodalConfig{Provider: "openaicompat", BaseURL: "http://localhost:1234/v1", Model: "sdxl"},
			"openaicompat:http://localhost:1234/v1::sdxl",
		},
		{"openaicompat missing url", MultimodalConfig{Provider: "openaicompat", Model: "sdxl"}, ""},
		{"openaicompat missing model", MultimodalConfig{Provider: "openaicompat", BaseURL: "u"}, ""},
		{"unknown provider", MultimodalConfig{Provider: "bogus"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.Spec(); got != c.want {
				t.Errorf("Spec() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLoadYAML_ParsesMultimodal(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      brain:
        provider: stub
multimodal:
  provider: openaicompat
  base_url: http://127.0.0.1:1234/v1
  model: sdxl
  cache_dir: /tmp/fl-images
  min_props_count: 2
`
	cfg, err := LoadYAML(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Multimodal.Spec(); got != "openaicompat:http://127.0.0.1:1234/v1::sdxl" {
		t.Errorf("Spec = %q", got)
	}
	if cfg.Multimodal.CacheDir != "/tmp/fl-images" {
		t.Errorf("CacheDir = %q", cfg.Multimodal.CacheDir)
	}
	if cfg.Multimodal.MinPropsCount != 2 {
		t.Errorf("MinPropsCount = %d", cfg.Multimodal.MinPropsCount)
	}
}

func TestLoadYAML_RejectsMultimodalMissingBaseURL(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      brain:
        provider: stub
multimodal:
  provider: openaicompat
  model: sdxl
`
	_, err := LoadYAML(strings.NewReader(src))
	if err == nil {
		t.Error("expected error for openaicompat without base_url")
	}
}

func TestLoadYAML_RejectsUnknownMultimodalProvider(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      brain:
        provider: stub
multimodal:
  provider: bogus
`
	_, err := LoadYAML(strings.NewReader(src))
	if err == nil {
		t.Error("expected error for unknown multimodal.provider")
	}
}

func TestLoadYAML_RejectsDuplicateWorldNames(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      brain:
        provider: stub
  - name: kosmos
    agent:
      brain:
        provider: stub
`
	_, err := LoadYAML(strings.NewReader(src))
	if err == nil {
		t.Errorf("expected duplicate-name error")
	}
}

func TestConfig_BuildUniverse(t *testing.T) {
	t.Parallel()
	src := `
worlds:
  - name: kosmos
    agent:
      brain:
        provider: stub
  - name: anima
    agent:
      brain:
        provider: stub
`
	cfg, err := LoadYAML(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	factory := func(_ context.Context, _ string) (brain.Brain, error) {
		return &scriptedBrain{decisions: []brain.Decision{{}}}, nil
	}
	u, err := cfg.BuildUniverse(context.Background(), factory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("nil universe")
	}
	if got := len(u.Sims()); got != 2 {
		t.Errorf("sims = %d, want 2", got)
	}
	if got := u.Worlds()[0].Name; got != "kosmos" {
		t.Errorf("first world name = %q", got)
	}
}

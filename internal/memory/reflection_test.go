package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type fakeBrain struct {
	seen    brain.Perception
	thought string
	err     error
	usage   brain.TokenUsage
	calls   int
}

func (f *fakeBrain) Decide(_ context.Context, p brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	f.calls++
	f.seen = p
	if f.err != nil {
		return brain.Decision{}, f.err
	}
	return brain.Decision{Thought: f.thought, Usage: f.usage}, nil
}
func (f *fakeBrain) Model() string    { return "fake" }
func (f *fakeBrain) Provider() string { return "fake" }
func (f *fakeBrain) Close() error     { return nil }

func TestShouldReflect(t *testing.T) {
	t.Parallel()
	r := &Reflector{Interval: 10}
	cases := []struct {
		tick world.Tick
		want bool
	}{
		{0, false},
		{5, false},
		{10, true},
		{15, false},
		{20, true},
	}
	for _, c := range cases {
		if got := r.ShouldReflect(c.tick); got != c.want {
			t.Errorf("ShouldReflect(%d) = %v, want %v", c.tick, got, c.want)
		}
	}
}

func TestShouldReflect_ZeroIntervalDisables(t *testing.T) {
	t.Parallel()
	r := &Reflector{Interval: 0}
	for _, tick := range []world.Tick{0, 10, 100, 1000} {
		if r.ShouldReflect(tick) {
			t.Errorf("zero-interval Reflector should never fire; got true at tick %d", tick)
		}
	}
}

func TestReflect_BelowMinRawNoOp(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	// Only 2 raw records < default min of 5.
	_, _ = s.Add(ctx, 1, KindAction, "a", 0, AddOptions{})
	_, _ = s.Add(ctx, 1, KindAction, "b", 1, AddOptions{})

	fb := &fakeBrain{thought: "- nothing"}
	r := &Reflector{Brain: fb, Interval: 10, MinRawMemories: 5}
	out, _, err := r.Reflect(ctx, s, 1, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected no reflections; got %d", len(out))
	}
	if fb.calls != 0 {
		t.Errorf("brain called %d times despite below-minimum raw memories", fb.calls)
	}
}

func TestReflect_SynthesisesAndStoresInsights(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := s.Add(ctx, 1, KindAction, "thing", world.Tick(i), AddOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	fb := &fakeBrain{thought: `- the world is filling up
- there are many things now
- it feels emergent`}
	r := &Reflector{Brain: fb, Interval: 10}

	out, usage, err := r.Reflect(ctx, s, 1, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d reflections, want 3; thoughts=%q", len(out), fb.thought)
	}
	for _, r := range out {
		if r.Kind != KindReflection {
			t.Errorf("reflection kind = %q, want %q", r.Kind, KindReflection)
		}
		if r.Importance < 7.0 {
			t.Errorf("reflection importance = %v, want >= 7", r.Importance)
		}
	}
	if usage != fb.usage {
		t.Errorf("returned usage differs from brain usage")
	}
	if s.Len() != 6+3 {
		t.Errorf("stream len = %d, want 9 (6 raw + 3 reflections)", s.Len())
	}
}

func TestReflect_LookbackExcludesPriorReflections(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	// Pre-seed a reflection from a previous pass.
	_, _ = s.Add(ctx, 1, KindReflection, "old insight", 0, AddOptions{Importance: 7.5})
	for i := 0; i < 6; i++ {
		if _, err := s.Add(ctx, 1, KindAction, "thing", world.Tick(i+1), AddOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	fb := &fakeBrain{thought: "- emergent\n- shapeful"}
	r := &Reflector{Brain: fb, Interval: 10, Lookback: 5}
	_, _, err := r.Reflect(ctx, s, 1, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The brain should have received the 5 most recent ACTIONS, not the old reflection.
	if got := len(fb.seen.Memories); got != 5 {
		t.Errorf("brain saw %d memories, want 5", got)
	}
	for _, m := range fb.seen.Memories {
		if m.Content == "old insight" {
			t.Errorf("reflection brain received prior reflection content")
		}
	}
}

func TestReflect_BrainErrorPropagates(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		_, _ = s.Add(ctx, 1, KindAction, "x", world.Tick(i), AddOptions{})
	}
	fb := &fakeBrain{err: errors.New("rate limited")}
	r := &Reflector{Brain: fb, Interval: 10}
	_, _, err := r.Reflect(ctx, s, 1, 10, nil)
	if err == nil {
		t.Errorf("expected error to propagate")
	}
}

func TestParseInsights_VariousBulletStyles(t *testing.T) {
	t.Parallel()
	in := `- alpha
* beta
• gamma
1. delta
2) epsilon
zeta
`
	got := parseInsights(in, 10)
	want := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("parsed %d insights, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("insight %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestParseInsights_RespectsMaxN(t *testing.T) {
	t.Parallel()
	got := parseInsights("- a\n- b\n- c\n- d", 2)
	if len(got) != 2 {
		t.Errorf("got %d, want 2", len(got))
	}
}

func TestParseInsights_DropsEmpty(t *testing.T) {
	t.Parallel()
	got := parseInsights("\n\n- alpha\n\n", 5)
	if len(got) != 1 || got[0] != "alpha" {
		t.Errorf("got %v, want [alpha]", got)
	}
}

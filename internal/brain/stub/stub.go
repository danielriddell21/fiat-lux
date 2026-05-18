package stub

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/tools"
)

// Brain is a Brain implementation that picks a random valid tool
// with sensible random args. Selection is weighted to favour Create
// so the TUI fills with content quickly.
type Brain struct {
	rng     *rand.Rand
	tools   []tools.Tool
	weights map[string]int
}

// New constructs a stub Brain. seed1 and seed2 seed math/rand/v2's
// pcg source; pass zero for both to get a default deterministic
// sequence useful in tests.
func New(seed1, seed2 uint64, reg *tools.Registry) *Brain {
	src := rand.NewPCG(seed1, seed2)
	return &Brain{
		rng:   rand.New(src),
		tools: reg.All(),
		weights: map[string]int{
			// Heavily Create-biased so the stub fills the world with
			// content quickly - this provides the visible-output
			// guarantee. Destroy / Unrelate are zero because a random
			// destroy of the root agent leaves the world inert; later
			// iterations attach more deliberation.
			"Create":     20,
			"Modify":     1,
			"Destroy":    0,
			"Relate":     4,
			"Unrelate":   0,
			"Observe":    1,
			"Reflect":    1,
			"Wait":       1,
			"SpawnAgent": 0, // disabled
		},
	}
}

// Decide picks a random tool that can produce valid args for the
// current perception and returns it as a ToolCall. If no tool can
// produce args (an extremely empty perception), Decide returns a
// nil ToolCall meaning "skip this tick".
func (b *Brain) Decide(_ context.Context, p brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	candidates := b.weightedOrder()
	for _, t := range candidates {
		if t.RandomArgs == nil {
			continue
		}
		args, ok := t.RandomArgs(b.rng, p)
		if !ok {
			continue
		}
		return brain.Decision{
			Thought:  fmt.Sprintf("(stub) chose %s with %d entities and %d relationships in view", t.Name, len(p.AliveEntities), len(p.AliveRelationships)),
			ToolCall: &brain.ToolCall{Name: t.Name, Args: args},
		}, nil
	}
	// Nothing produced args (e.g. zero-tool registry). Skip the tick.
	return brain.Decision{Thought: "(stub) no tool could produce valid args; skipping tick"}, nil
}

// Close is a no-op for the stub.
func (b *Brain) Close() error { return nil }

// Model returns the stub model identifier. Used by the budget
// tracker, which looks the name up in its price table; the stub is
// not in the table so it always costs zero.
func (b *Brain) Model() string { return "stub" }

// Provider returns "stub".
func (b *Brain) Provider() string { return "stub" }

// weightedOrder returns the enabled tools (weight > 0) in a random
// order weighted by b.weights: heavier weight is more likely to come
// first. Tools whose weight is zero - or which are absent from the
// weights map - are filtered out entirely.
//
// The implementation draws Float64()/weight per candidate and sorts
// ascending. Smaller score = picked sooner; larger weight skews
// scores toward zero.
func (b *Brain) weightedOrder() []tools.Tool {
	type scored struct {
		t     tools.Tool
		score float64
	}
	xs := make([]scored, 0, len(b.tools))
	for _, t := range b.tools {
		w := b.weights[t.Name]
		if w <= 0 {
			continue
		}
		xs = append(xs, scored{t: t, score: b.rng.Float64() / float64(w)})
	}
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1].score > xs[j].score; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
	out := make([]tools.Tool, len(xs))
	for i, s := range xs {
		out[i] = s.t
	}
	return out
}

// Compile-time assertion that *Brain satisfies brain.Brain.
var _ brain.Brain = (*Brain)(nil)

// MustMarshal is a helper for tests that need to construct a
// ToolCall with literal args.
func MustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

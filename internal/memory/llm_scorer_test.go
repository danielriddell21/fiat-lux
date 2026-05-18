package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

type scriptedBrain struct {
	thought string
	err     error
}

func (s *scriptedBrain) Decide(_ context.Context, _ brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	if s.err != nil {
		return brain.Decision{}, s.err
	}
	return brain.Decision{Thought: s.thought}, nil
}
func (s *scriptedBrain) Model() string    { return "test" }
func (s *scriptedBrain) Provider() string { return "test" }
func (s *scriptedBrain) Close() error     { return nil }

func TestLLMScorer_ParsesNumber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want float64
	}{
		{"7", 7.0},
		{"7.5", 7.5},
		{"importance: 8", 8.0},
		{"On a scale of 0 to 10, I'd say this is a 4.", 4.0},
	} {
		l := LLMScorer{Brain: &scriptedBrain{thought: tc.text}}
		got, err := l.Score(context.Background(), KindAction, "hello")
		if err != nil {
			t.Errorf("Score(%q): %v", tc.text, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Score(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestLLMScorer_ClampsAboveTen(t *testing.T) {
	t.Parallel()
	l := LLMScorer{Brain: &scriptedBrain{thought: "99"}}
	got, err := l.Score(context.Background(), KindAction, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Errorf("got %v, want 10 (clamped)", got)
	}
}

func TestLLMScorer_FallsBackOnError(t *testing.T) {
	t.Parallel()
	l := LLMScorer{Brain: &scriptedBrain{err: errors.New("nope")}}
	got, err := l.Score(context.Background(), KindReflection, "the world is now strange")
	if err != nil {
		t.Fatal(err)
	}
	// Heuristic for KindReflection is 8.0 base + length bumps; never zero.
	if got <= 0 {
		t.Errorf("fallback gave non-positive score: %v", got)
	}
}

func TestLLMScorer_FallsBackOnUnparseable(t *testing.T) {
	t.Parallel()
	l := LLMScorer{Brain: &scriptedBrain{thought: "I am thinking deeply"}}
	got, err := l.Score(context.Background(), KindAction, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 {
		t.Errorf("fallback gave non-positive score: %v", got)
	}
}

func TestLLMScorer_NilBrainFallsBack(t *testing.T) {
	t.Parallel()
	l := LLMScorer{}
	got, err := l.Score(context.Background(), KindAction, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 {
		t.Errorf("nil-brain fallback gave %v", got)
	}
}

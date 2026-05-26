package sim

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/narrator"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type fakeNarratorBrain struct {
	thought string
	called  int
}

func (f *fakeNarratorBrain) Decide(_ context.Context, _ brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	f.called++
	return brain.Decision{Thought: f.thought}, nil
}
func (f *fakeNarratorBrain) Model() string    { return "fake" }
func (f *fakeNarratorBrain) Provider() string { return "fake" }
func (f *fakeNarratorBrain) Close() error     { return nil }

func TestNarrator_WritesChapterOnceThresholdMet(t *testing.T) {
	t.Parallel()
	nb := &fakeNarratorBrain{thought: "in the beginning, things were made."}
	n, err := narrator.New(narrator.Options{
		Brain:         nb,
		IntervalTicks: 2,
		MinEvents:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := world.New("kosmos")
	creator := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"x"}`)}},
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"y"}`)}},
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"z"}`)}},
	}}
	s, err := New(Options{World: w, Brain: creator, Narrator: n})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for range 5 {
		if _, err := s.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	chapters := s.Annals()
	if len(chapters) == 0 {
		t.Errorf("expected at least one chapter; brain called %d times", nb.called)
	}
	for _, c := range chapters {
		if c.Content == "" {
			t.Errorf("empty chapter content")
		}
	}
}

func TestNarrator_DisabledWhenNil(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	creator := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{"type_label":"x"}`)}},
	}}
	s, _ := New(Options{World: w, Brain: creator})
	defer s.Close()
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Annals()) != 0 {
		t.Errorf("expected no chapters when narrator nil")
	}
}

package sim

import (
	"context"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/brain/stub"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func TestIntervene_CreateStampsIntervenerSentinel(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	s, err := New(Options{World: w, Brain: stub.New(0, 0, tools.Default())})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Intervene(context.Background(), Intervention{
		Op:         InterveneCreate,
		TypeLabel:  "miracle",
		Properties: map[string]any{"shimmer": true},
	}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range w.Events() {
		if ev.Kind != world.EventCreate {
			continue
		}
		if ev.TypeLabel != "miracle" {
			continue
		}
		found = true
		if ev.Agent != world.AgentIntervener {
			t.Errorf("event.Agent = %d, want AgentIntervener (%d)", ev.Agent, world.AgentIntervener)
		}
	}
	if !found {
		t.Errorf("no miracle event in log: %v", w.Events())
	}
}

func TestIntervene_BroadcastsToAllAgents(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &scriptedBrain{decisions: []brain.Decision{{Thought: "ok"}}}
	s, _ := New(Options{World: w, Brain: br})
	if err := s.Intervene(context.Background(), Intervention{
		Op:      InterveneSpeak,
		Content: "the void stirs",
	}); err != nil {
		t.Fatal(err)
	}
	ag := s.Agent()
	if ag.InboxLen() == 0 {
		t.Errorf("expected the intervention to land in the agent's inbox")
	}
	heard := ag.DrainHeard()
	if len(heard) != 1 {
		t.Fatalf("inbox=%d, want 1", len(heard))
	}
	if heard[0].SpeakerName != "the void" {
		t.Errorf("speaker = %q, want 'the void'", heard[0].SpeakerName)
	}
}

func TestIntervene_DestroyRejectsZeroID(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	s, _ := New(Options{World: w, Brain: stub.New(0, 0, tools.Default())})
	err := s.Intervene(context.Background(), Intervention{Op: InterveneDestroy})
	if err == nil {
		t.Error("expected error for missing entity_id")
	}
}

func TestIntervene_UnknownOpRejected(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	s, _ := New(Options{World: w, Brain: stub.New(0, 0, tools.Default())})
	err := s.Intervene(context.Background(), Intervention{Op: "wat"})
	if err == nil {
		t.Error("expected error for unknown op")
	}
}

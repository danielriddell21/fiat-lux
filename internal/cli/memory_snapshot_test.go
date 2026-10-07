package cli

import (
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain/stub"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/sim"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func newTestAgent(t *testing.T, id world.EntityID, name string, records ...memory.Record) *agent.Agent {
	t.Helper()
	ag, err := agent.New(id, name, "", stub.New(1, 2, tools.Default()), tools.Default())
	if err != nil {
		t.Fatal(err)
	}
	if records != nil {
		ag.Memory = memory.New(memory.ScoreWeights{}, 1)
		ag.Memory.Restore(records)
	}
	return ag
}

func TestMemorySnapshotOfAnEmptyRoster(t *testing.T) {
	if got := memorySnapshot(nil, 7); got.AgentName != "" || len(got.Records) != 0 {
		t.Errorf("memorySnapshot(nil) = %+v, want empty", got)
	}
}

func TestMemorySnapshotFollowsTheFocus(t *testing.T) {
	first := newTestAgent(t, 1, "first")
	second := newTestAgent(t, 2, "second", memory.Record{
		ID: 9, Kind: memory.Kind("observation"), Content: "the void hums", CreatedAt: 4, Importance: 0.5,
	})
	roster := []*agent.Agent{first, second}

	got := memorySnapshot(roster, 2)
	if got.AgentName != "second" || len(got.Records) != 1 {
		t.Fatalf("focused on 2: %+v", got)
	}
	r := got.Records[0]
	if r.ID != 9 || r.Kind != "observation" || r.Content != "the void hums" || r.Tick != 4 || r.Importance != 0.5 {
		t.Errorf("record = %+v", r)
	}

	// A focus that has left the roster falls back to the first agent, whose
	// memory is nil.
	got = memorySnapshot(roster, 42)
	if got.AgentName != "first" || got.Records != nil {
		t.Errorf("focus gone: %+v, want the first agent with no records", got)
	}
}

func newTestSim(t *testing.T) *sim.Sim {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	s, err := sim.New(sim.Options{World: w, Brain: stub.New(1, 2, tools.Default())})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdaptersSnapshotTheRootAgent(t *testing.T) {
	s := newTestSim(t)
	root := s.Agent()
	if got := newSimAdapter(s).MemorySnapshot(); got.AgentName != root.Name {
		t.Errorf("sim adapter snapshot is of %q, want the root %q", got.AgentName, root.Name)
	}

	u, err := sim.NewUniverse([]*sim.Sim{newTestSim(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := newUniverseAdapter(u).MemorySnapshot(); got.AgentName == "" {
		t.Error("universe adapter snapshot has no agent")
	}
}

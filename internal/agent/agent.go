package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Agent is the runtime brain attached to an agent-entity. It owns a
// pointer to its Brain, the tool subset its creator granted it, and
// its private memory stream.
type Agent struct {
	// EntityID is the agent's own entry in the world registry.
	EntityID world.EntityID

	// Name is the agent's display name; pulled from its entity's
	// properties at spawn time.
	Name string

	// SystemPrompt is the static instruction prepended to every
	// brain call.
	SystemPrompt string

	// Brain is the LLM-shaped decision-maker.
	Brain brain.Brain

	// Tools is the registry the agent may act through. May be a
	// subset of the global catalogue per the creator's grant.
	Tools *tools.Registry

	// Memory is the Smallville-style memory stream. Records are
	// appended after every step; top-K are retrieved into the
	// next perception.
	Memory *memory.Stream

	// Embedder hashes text into embedding vectors for relevance
	// retrieval. May be nil (zero-vector fallback).
	Embedder memory.Embedder

	// Importance scorer overrides the heuristic default. May be
	// nil; memory.Add falls back to memory.HeuristicScorer.
	Importance memory.Importance

	// RetrieveK is how many records are pulled into each
	// perception. Defaults to 6 when zero.
	RetrieveK int

	// SeenEventID tracks the highest event ID this agent has
	// perceived. The next perception's RecentEvents include only
	// events with ID > SeenEventID. Updated by the sim after each
	// step.
	SeenEventID world.EventID

	// SpawnDepth is the number of ancestors between this agent and
	// the root creator. Zero for the root agent; one for its direct
	// children; etc. Used to enforce MaxSpawnDepth.
	SpawnDepth int

	// inbox holds Heard events delivered by Speak from other
	// agents in the same world. Drained into Perception.Heard by
	// the sim on each step.
	inbox   []brain.HeardEvent
	inboxMu sync.Mutex
}

// New constructs an Agent. brain and tools must be non-nil.
func New(entityID world.EntityID, name, systemPrompt string, br brain.Brain, reg *tools.Registry) (*Agent, error) {
	if br == nil {
		return nil, fmt.Errorf("agent: brain is required")
	}
	if reg == nil {
		return nil, fmt.Errorf("agent: tools registry is required")
	}
	return &Agent{
		EntityID:     entityID,
		Name:         name,
		SystemPrompt: systemPrompt,
		Brain:        br,
		Tools:        reg,
		RetrieveK:    6,
	}, nil
}

// RememberAction records a memory pair for the just-completed step:
// the agent's tool choice and the tool's result. Either may be
// empty (e.g. the brain returned no tool call). Returns the IDs
// inserted; errors are logged via the returned error so the sim
// can decide whether to surface them.
func (a *Agent) RememberAction(ctx context.Context, tick world.Tick, thought, toolName, toolResult string) error {
	if a.Memory == nil {
		return nil
	}
	opts := memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance}
	if thought != "" {
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindThought, thought, tick, opts); err != nil {
			return fmt.Errorf("agent: remember thought: %w", err)
		}
	}
	if toolName != "" {
		actionContent := "I chose " + toolName
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindAction, actionContent, tick, opts); err != nil {
			return fmt.Errorf("agent: remember action: %w", err)
		}
	}
	if toolResult != "" {
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindOutcome, toolResult, tick, opts); err != nil {
			return fmt.Errorf("agent: remember outcome: %w", err)
		}
	}
	return nil
}

// RememberObservation records an observation of an external event.
func (a *Agent) RememberObservation(ctx context.Context, tick world.Tick, content string) error {
	if a.Memory == nil || content == "" {
		return nil
	}
	_, err := a.Memory.Add(ctx, a.EntityID, memory.KindObservation, content, tick,
		memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance})
	if err != nil {
		return fmt.Errorf("agent: remember observation: %w", err)
	}
	return nil
}

// DeliverHeard enqueues a HeardEvent into this agent's inbox. The
// sim's broadcast machinery calls this on every other agent in the
// world when one of them issues Speak. Thread-safe.
func (a *Agent) DeliverHeard(ev brain.HeardEvent) {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	a.inbox = append(a.inbox, ev)
}

// DrainHeard returns and clears the inbox. Called by the sim just
// before BuildPerception so the agent perceives broadcasts on the
// tick they arrived. Thread-safe.
func (a *Agent) DrainHeard() []brain.HeardEvent {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	if len(a.inbox) == 0 {
		return nil
	}
	out := a.inbox
	a.inbox = nil
	return out
}

// InboxLen returns the number of pending Heard events without
// draining. Used by the sim's skip-tick decision: an agent that
// has been spoken to since its last turn must wake up.
func (a *Agent) InboxLen() int {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	return len(a.inbox)
}

// RememberHeard records a heard broadcast as an observation in the
// agent's memory stream so it persists past the next tick.
func (a *Agent) RememberHeard(ctx context.Context, tick world.Tick, ev brain.HeardEvent) error {
	if a.Memory == nil {
		return nil
	}
	content := fmt.Sprintf("I heard %s say: %s", ev.SpeakerName, ev.Content)
	_, err := a.Memory.Add(ctx, a.EntityID, memory.KindObservation, content, tick,
		memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance})
	if err != nil {
		return fmt.Errorf("agent: remember heard: %w", err)
	}
	return nil
}

// RetrieveMemories returns the top-K records relevant to the
// current perception query.
func (a *Agent) RetrieveMemories(ctx context.Context, query string, tick world.Tick) ([]memory.Record, error) {
	if a.Memory == nil {
		return nil, nil
	}
	k := a.RetrieveK
	if k <= 0 {
		k = 6
	}
	return a.Memory.Retrieve(ctx, query, tick, k, a.Embedder)
}

// BuildPerception assembles a Perception snapshot from the world.
// memories are the retrieved records to inject; may be nil.
// heard contains broadcasts drained from this agent's inbox; may
// be nil.
func (a *Agent) BuildPerception(w *world.World, memories []memory.Record, heard []brain.HeardEvent) brain.Perception {
	ents := w.Entities()
	rels := w.Relationships()
	allEvents := w.Events()

	pe := make([]brain.EntityView, len(ents))
	for i, e := range ents {
		pe[i] = brain.EntityView{
			ID:         uint64(e.ID),
			TypeLabel:  e.TypeLabel,
			Properties: e.Properties,
		}
	}
	pr := make([]brain.RelationshipView, len(rels))
	for i, r := range rels {
		pr[i] = brain.RelationshipView{
			ID:   uint64(r.ID),
			From: uint64(r.From),
			To:   uint64(r.To),
			Kind: r.Kind,
		}
	}
	// RecentEvents: events strictly newer than the agent's high-water
	// mark, in order.
	var recent []brain.EventView
	for _, e := range allEvents {
		if e.ID <= a.SeenEventID {
			continue
		}
		recent = append(recent, brain.EventView{
			ID:       uint64(e.ID),
			Tick:     uint64(e.Tick),
			Kind:     string(e.Kind),
			Agent:    uint64(e.Agent),
			EntityID: uint64(e.EntityID),
			RelID:    uint64(e.RelID),
			Summary:  summariseEvent(e),
		})
	}
	return brain.Perception{
		Tick:               uint64(w.Tick()),
		EntityCount:        w.EntityCount(),
		AliveEntities:      pe,
		AliveRelationships: pr,
		RecentEvents:       recent,
		Memories:           memory.PerceptionViews(memories),
		Heard:              heard,
	}
}

// MarkSeen advances the agent's SeenEventID to the highest event ID
// currently in the world. Called by the sim after each step so the
// next perception only carries the deltas.
func (a *Agent) MarkSeen(w *world.World) {
	events := w.Events()
	if len(events) == 0 {
		return
	}
	high := events[len(events)-1].ID
	if high > a.SeenEventID {
		a.SeenEventID = high
	}
}

func summariseEvent(e world.Event) string {
	switch e.Kind {
	case world.EventCreate:
		if e.TypeLabel != "" {
			return fmt.Sprintf("create %s #%d", e.TypeLabel, e.EntityID)
		}
		return fmt.Sprintf("create #%d", e.EntityID)
	case world.EventModify:
		return fmt.Sprintf("modify #%d", e.EntityID)
	case world.EventDestroy:
		return fmt.Sprintf("destroy #%d", e.EntityID)
	case world.EventRelate:
		return fmt.Sprintf("relate #%d -[%s]-> #%d", e.From, e.RelKind, e.To)
	case world.EventUnrelate:
		if e.Cascade {
			return fmt.Sprintf("unrelate #%d (cascade)", e.RelID)
		}
		return fmt.Sprintf("unrelate #%d", e.RelID)
	case world.EventTickStart:
		return ""
	default:
		return string(e.Kind)
	}
}

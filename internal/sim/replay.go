package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// ErrReplayDone is returned by Replay.Step when the event log has
// been fully replayed.
var ErrReplayDone = errors.New("sim: replay complete")

// Replay walks a saved event log tick by tick, applying each event
// to a fresh world. Since brain responses are recorded as part of
// the resulting world events, replays are exact: the visible world
// state evolves the same way it did during the original run.
//
// --replay re-renders a saved run with no live brain calls.
type Replay struct {
	mu     sync.Mutex
	World  *world.World
	events []world.Event
	next   int
}

// NewReplay constructs a Replay for the named world. Events are
// applied in order; their original IDs are honoured.
func NewReplay(name string, events []world.Event) (*Replay, error) {
	w, err := world.New(name)
	if err != nil {
		return nil, fmt.Errorf("sim: replay world: %w", err)
	}
	cp := make([]world.Event, len(events))
	copy(cp, events)
	return &Replay{World: w, events: cp}, nil
}

// Step applies the next event. The returned StepResult describes
// the event in plain language so the TUI's reasoning pane has
// something to show. ErrReplayDone is returned after the last
// event; further calls return the same error.
func (r *Replay) Step(_ context.Context) (StepResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next >= len(r.events) {
		return StepResult{Tick: r.World.Tick()}, ErrReplayDone
	}
	ev := r.events[r.next]
	r.next++
	if err := r.World.ApplyEventForLoad(ev); err != nil {
		return StepResult{}, fmt.Errorf("sim: replay apply: %w", err)
	}
	return StepResult{
		Tick:       ev.Tick,
		AgentID:    ev.Agent,
		Thought:    fmt.Sprintf("replay event #%d", ev.ID),
		ToolName:   string(ev.Kind),
		ToolResult: replaySummary(ev),
	}, nil
}

// Progress reports how many events have been applied and how many
// remain. Useful for the TUI status line.
func (r *Replay) Progress() (applied, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next, len(r.events)
}

// Done reports whether the replay has finished.
func (r *Replay) Done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next >= len(r.events)
}

// Close is a no-op; included so Replay satisfies the same
// "close after use" pattern as Sim.
func (r *Replay) Close() error { return nil }

func replaySummary(e world.Event) string {
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
		return fmt.Sprintf("tick %d begins", e.Tick)
	default:
		return string(e.Kind)
	}
}

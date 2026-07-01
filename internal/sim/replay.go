package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

var ErrReplayDone = errors.New("sim: replay complete")

type Replay struct {
	mu     sync.Mutex
	World  *world.World
	events []world.Event
	next   int
}

func NewReplay(name string, events []world.Event) (*Replay, error) {
	w, err := world.New(name)
	if err != nil {
		return nil, fmt.Errorf("sim: replay world: %w", err)
	}
	cp := make([]world.Event, len(events))
	copy(cp, events)
	return &Replay{World: w, events: cp}, nil
}

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

func (r *Replay) Progress() (applied, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next, len(r.events)
}

func (r *Replay) Done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next >= len(r.events)
}

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

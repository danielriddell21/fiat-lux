package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Universe holds N independent worlds running in parallel. Each
// world has its own Sim, agents, memory, and event log; agents
// cannot perceive across worlds.
//
// The TUI shows one focused world at a time; Step rotates which
// world is ticked on each call so all worlds advance independently.
type Universe struct {
	mu       sync.Mutex
	sims     []*Sim
	cursor   int // which sim to step next (round-robin across worlds)
	focused  int // which sim the TUI is currently rendering
	closeErr error
}

// NewUniverse constructs a Universe from a non-empty roster of sims.
// The first sim is the initial focused world.
func NewUniverse(sims []*Sim) (*Universe, error) {
	if len(sims) == 0 {
		return nil, errors.New("sim: universe needs at least one sim")
	}
	return &Universe{sims: sims}, nil
}

// Sims returns the universe's sims in declaration order.
func (u *Universe) Sims() []*Sim {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]*Sim, len(u.sims))
	copy(out, u.sims)
	return out
}

// Focused returns the currently-focused sim. Never nil after a
// successful NewUniverse.
func (u *Universe) Focused() *Sim {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sims[u.focused]
}

// FocusedIdx returns the index of the focused sim within Sims().
func (u *Universe) FocusedIdx() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.focused
}

// SetFocusedIdx sets the focused-sim index, clamping to a valid
// range. Out-of-range inputs become no-ops; passing a negative
// value or one >= len(sims) preserves the current focus.
func (u *Universe) SetFocusedIdx(i int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if i < 0 || i >= len(u.sims) {
		return
	}
	u.focused = i
}

// CycleFocus advances the focused index by delta (typically +1 or
// -1) with wrap-around.
func (u *Universe) CycleFocus(delta int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := len(u.sims)
	u.focused = (u.focused + delta + n) % n
}

// Step ticks the next sim in the round-robin. Worlds tick
// independently; rotating sims at the Universe.Step boundary keeps
// each world advancing without any cross-world coupling on
// per-tick latency.
func (u *Universe) Step(ctx context.Context) (StepResult, error) {
	u.mu.Lock()
	if u.cursor >= len(u.sims) {
		u.cursor = 0
	}
	s := u.sims[u.cursor]
	u.cursor++
	u.mu.Unlock()
	return s.Step(ctx)
}

// Close releases every sim's brain.
func (u *Universe) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var firstErr error
	for _, s := range u.sims {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	u.closeErr = firstErr
	return firstErr
}

// FocusedWorldName returns the name of the focused world.
func (u *Universe) FocusedWorldName() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sims[u.focused].World.Name()
}

// WorldInfo is a TUI-facing summary of one world in the universe.
type WorldInfo struct {
	Index       int
	Name        string
	EntityCount int
	AgentCount  int
	Tick        world.Tick
}

// Worlds returns a summary slice of every world. The slice is
// safe to read after the call returns.
func (u *Universe) Worlds() []WorldInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]WorldInfo, len(u.sims))
	for i, s := range u.sims {
		out[i] = WorldInfo{
			Index:       i,
			Name:        s.World.Name(),
			EntityCount: s.World.EntityCount(),
			AgentCount:  len(s.Agents()),
			Tick:        s.World.Tick(),
		}
	}
	return out
}

// String renders a WorldInfo as "<name> #i (tick=T, ents=N, agents=M)".
func (w WorldInfo) String() string {
	return fmt.Sprintf("%s #%d (tick=%d ents=%d agents=%d)",
		w.Name, w.Index, w.Tick, w.EntityCount, w.AgentCount)
}

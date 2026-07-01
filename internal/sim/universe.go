package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

type Universe struct {
	mu       sync.Mutex
	sims     []*Sim
	cursor   int
	focused  int
	closeErr error
}

func NewUniverse(sims []*Sim) (*Universe, error) {
	if len(sims) == 0 {
		return nil, errors.New("sim: universe needs at least one sim")
	}
	return &Universe{sims: sims}, nil
}

func (u *Universe) Sims() []*Sim {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]*Sim, len(u.sims))
	copy(out, u.sims)
	return out
}

func (u *Universe) Focused() *Sim {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sims[u.focused]
}

func (u *Universe) FocusedIdx() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.focused
}

func (u *Universe) SetFocusedIdx(i int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if i < 0 || i >= len(u.sims) {
		return
	}
	u.focused = i
}

func (u *Universe) CycleFocus(delta int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := len(u.sims)
	u.focused = (u.focused + delta + n) % n
}

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

func (u *Universe) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var errs []error
	for _, s := range u.sims {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	u.closeErr = errors.Join(errs...)
	return u.closeErr
}

func (u *Universe) FocusedWorldName() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sims[u.focused].World.Name()
}

type WorldInfo struct {
	Index       int
	Name        string
	EntityCount int
	AgentCount  int
	Tick        world.Tick
}

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

func (w WorldInfo) String() string {
	return fmt.Sprintf("%s #%d (tick=%d ents=%d agents=%d)",
		w.Name, w.Index, w.Tick, w.EntityCount, w.AgentCount)
}

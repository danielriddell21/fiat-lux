package sim

import (
	"context"
	"slices"

	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/narrator"
)

func (s *Sim) maybeWriteChapter(ctx context.Context) {
	s.mu.Lock()
	n := s.narrator
	busy := s.narratorBusy
	s.mu.Unlock()
	if n == nil || busy {
		return
	}
	if !n.ShouldChapter(s.World) {
		return
	}

	s.mu.Lock()
	s.narratorBusy = true
	roster := slices.Clone(s.agents)
	s.mu.Unlock()

	chapter, err := n.WriteChapter(ctx, s.World, roster)

	s.mu.Lock()
	s.narratorBusy = false
	if err == nil && chapter.Content != "" {
		s.annals = append(s.annals, chapter)
	}
	s.mu.Unlock()

	if err != nil || chapter.Content == "" {
		return
	}

	// Persist the chapter into the world-level memory stream (when
	// one of the agents has a stream we can reach). The chapter is
	// keyed against agent 0 so it is unambiguously a world record.
	if len(roster) == 0 || roster[0].Memory == nil {
		return
	}
	_, _ = roster[0].Memory.Add(ctx, 0, memory.KindChapter, chapter.Content, s.World.Tick(),
		memory.AddOptions{})
}

func (s *Sim) Annals() []narrator.Chapter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.annals) == 0 {
		return nil
	}
	return slices.Clone(s.annals)
}

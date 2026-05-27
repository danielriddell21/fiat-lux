package sim

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Inheritance digest defaults. Each child of a dying agent receives
// up to TopKReflections + TopNRaw memory records, written into its
// stream as memory.KindInheritance.
const (
	defaultTopKReflections = 5
	defaultTopNRaw         = 10
)

// handleDie computes a memory digest, hands it to every direct child
// of the dying agent, soft-destroys the agent's entity, marks the
// runtime dead, and appends an EventDie. Final words (if any) are
// recorded on the event.
func (s *Sim) handleDie(ctx context.Context, ag *agent.Agent, raw json.RawMessage, tick world.Tick) (string, error) {
	var a tools.DieArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("Die: bad args: %w", err)
		}
	}

	digest := buildInheritanceDigest(ag, defaultTopKReflections, defaultTopNRaw)

	s.mu.Lock()
	children := s.directChildrenLocked(ag.EntityID)
	s.mu.Unlock()

	for _, child := range children {
		if child.Memory == nil {
			continue
		}
		opts := memory.AddOptions{Embedder: child.Embedder, Scorer: child.Importance}
		for _, content := range digest {
			if _, err := child.Memory.Add(ctx, child.EntityID, memory.KindInheritance, content, tick, opts); err != nil {
				return "", fmt.Errorf("Die: inherit to #%d: %w", child.EntityID, err)
			}
		}
	}

	props := world.Properties{"name": ag.Name}
	if a.Final != "" {
		props["final"] = a.Final
	}
	s.World.EmitInfo(ag.EntityID, world.EventDie, props)

	// Soft-destroy the agent's entity so the world reflects death too.
	if err := s.World.Destroy(world.NoAgent, ag.EntityID); err != nil {
		// Destroy may fail if the entity is already gone; non-fatal.
		_ = err
	}

	s.mu.Lock()
	if rt := s.perAgentRT[ag.EntityID]; rt != nil {
		rt.dead = true
	}
	s.mu.Unlock()

	return fmt.Sprintf("%s died; %d direct child(ren) inherited %d memor(ies)",
		ag.Name, len(children), len(digest)), nil
}

// directChildrenLocked returns the agents whose ParentEntityID
// matches parentID. Caller must hold s.mu.
func (s *Sim) directChildrenLocked(parentID world.EntityID) []*agent.Agent {
	var out []*agent.Agent
	for _, a := range s.agents {
		if a.ParentEntityID == parentID {
			out = append(out, a)
		}
	}
	return out
}

// buildInheritanceDigest selects up to topK reflection records and
// topN highest-importance non-reflection records from the agent's
// memory stream, formatted as inheritance entries.
func buildInheritanceDigest(ag *agent.Agent, topK, topN int) []string {
	if ag == nil || ag.Memory == nil {
		return nil
	}
	all := ag.Memory.All()
	reflections := make([]memory.Record, 0)
	others := make([]memory.Record, 0)
	for _, r := range all {
		if r.Kind == memory.KindReflection {
			reflections = append(reflections, r)
			continue
		}
		others = append(others, r)
	}
	slices.SortFunc(reflections, func(a, b memory.Record) int {
		return cmp.Compare(b.Importance, a.Importance)
	})
	slices.SortFunc(others, func(a, b memory.Record) int {
		return cmp.Compare(b.Importance, a.Importance)
	})
	if topK > len(reflections) {
		topK = len(reflections)
	}
	if topN > len(others) {
		topN = len(others)
	}
	out := make([]string, 0, topK+topN)
	for _, r := range reflections[:topK] {
		out = append(out, formatInheritance(ag, "reflection", r))
	}
	for _, r := range others[:topN] {
		out = append(out, formatInheritance(ag, string(r.Kind), r))
	}
	return out
}

func formatInheritance(ag *agent.Agent, kind string, r memory.Record) string {
	content := strings.TrimSpace(r.Content)
	return fmt.Sprintf("from %s (%s, tick %d): %s", ag.Name, kind, r.CreatedAt, content)
}

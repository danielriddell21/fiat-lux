package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// ReflectionSystemPrompt is the canonical instruction Smallville
// (Park et al. 2023) sends to its reflection brain, adapted to the
// fiat-lux agent. The agent is asked to synthesise a small number
// of higher-level insights from the most recent raw memories.
const ReflectionSystemPrompt = `You are an agent reflecting on what you have observed and done. ` +
	`Below are your recent memories. Synthesise at most three brief, high-level insights about what is happening - ` +
	`the patterns, surprises, or implications that a low-level memory stream misses. ` +
	`Output one insight per line, prefixed with "- ", and nothing else. Be concise.`

// Reflector runs the periodic reflection pass: every Interval ticks
// it gathers the most recent raw memories, asks a brain to synthesise
// higher-level insights, and writes them back as KindReflection
// records.
//
// Reflection runs on the agent's own brain; multi-brain routing
// can be added later.
type Reflector struct {
	// Brain is the LLM used for synthesis. Required.
	Brain brain.Brain

	// Interval is the tick gap between reflection passes. Zero
	// disables the pass.
	Interval uint64

	// Lookback is how many of the most recent memories to feed the
	// reflection brain. Defaults to 20.
	Lookback int

	// MaxInsights caps the number of reflection records emitted per
	// pass. Defaults to 3.
	MaxInsights int

	// MinRawMemories is the minimum number of non-reflection records
	// required before a pass will fire. Below this the pass is a
	// no-op; reflecting on nothing yields nothing useful and just
	// costs tokens. Defaults to 5.
	MinRawMemories int
}

// ShouldReflect reports whether a reflection pass is due at the
// given tick. Reflection fires when the tick is a non-zero multiple
// of the interval. Callers must also check the stream's content
// against MinRawMemories before invoking Reflect.
func (r *Reflector) ShouldReflect(tick world.Tick) bool {
	if r == nil || r.Interval == 0 {
		return false
	}
	if tick == 0 {
		return false
	}
	return uint64(tick)%r.Interval == 0
}

// Reflect runs one reflection pass. It pulls the most recent raw
// (non-reflection) memories from the stream, asks the brain to
// synthesise insights, and appends the insights back into the
// stream as KindReflection records.
//
// Returns the inserted records (empty when nothing new was
// synthesised) and the token usage reported by the brain so the
// budget tracker can record it.
func (r *Reflector) Reflect(
	ctx context.Context,
	stream *Stream,
	agentID world.AgentID,
	tick world.Tick,
	embedder Embedder,
) ([]Record, brain.TokenUsage, error) {
	if r == nil || r.Brain == nil || stream == nil {
		return nil, brain.TokenUsage{}, nil
	}
	lookback := r.Lookback
	if lookback <= 0 {
		lookback = 20
	}
	maxInsights := r.MaxInsights
	if maxInsights <= 0 {
		maxInsights = 3
	}
	minRaw := r.MinRawMemories
	if minRaw <= 0 {
		minRaw = 5
	}

	recent := lastRawMemories(stream.All(), lookback)
	if len(recent) < minRaw {
		return nil, brain.TokenUsage{}, nil
	}

	// Build a reflection-specific perception. We pack the recent
	// memories into the Memories slice and use a per-pass system
	// prompt; tools are deliberately empty so the brain returns
	// plain text.
	rp := brain.Perception{
		Tick:     uint64(tick),
		Memories: PerceptionViews(recent),
	}
	decision, err := r.Brain.Decide(ctx, rp, nil)
	if err != nil {
		return nil, brain.TokenUsage{}, fmt.Errorf("memory: reflection brain failed: %w", err)
	}

	insights := parseInsights(decision.Thought, maxInsights)
	if len(insights) == 0 {
		return nil, decision.Usage, nil
	}

	out := make([]Record, 0, len(insights))
	for _, text := range insights {
		rec, err := stream.Add(ctx, agentID, KindReflection, text, tick, AddOptions{
			Embedder: embedder,
			// Reflections get a moderately high importance floor so
			// they bubble up in retrieval. Heuristic on the actual
			// content can push higher.
			Importance: 7.5,
		})
		if err != nil {
			return out, decision.Usage, fmt.Errorf("memory: store reflection: %w", err)
		}
		out = append(out, rec)
	}
	return out, decision.Usage, nil
}

// lastRawMemories returns up to n of the most recent
// non-reflection records, oldest-first within the returned slice
// so the brain reads them chronologically.
func lastRawMemories(all []Record, n int) []Record {
	filtered := make([]Record, 0, n)
	// Walk backward and collect raw records up to n.
	for i := len(all) - 1; i >= 0 && len(filtered) < n; i-- {
		if all[i].Kind == KindReflection {
			continue
		}
		filtered = append(filtered, all[i])
	}
	// Reverse so the brain sees them in chronological order.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered
}

// parseInsights extracts up to maxN insight lines from the brain's
// freeform text. Accepted formats: "- insight", "* insight",
// "1. insight", "1) insight", or plain lines. Empty lines are
// dropped.
func parseInsights(text string, maxN int) []string {
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Strip common list prefixes.
		line = stripListPrefix(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

func stripListPrefix(s string) string {
	// Bullet markers.
	for _, p := range []string{"- ", "* ", "• ", "– "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	// Numbered: "1. ", "1) ", "10. ", up to two leading digits.
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		i := 1
		if i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i < len(s) && (s[i] == '.' || s[i] == ')') {
			i++
			if i < len(s) && s[i] == ' ' {
				return strings.TrimSpace(s[i+1:])
			}
		}
	}
	return s
}

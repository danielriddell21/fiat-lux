package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

const ReflectionSystemPrompt = `You are an agent reflecting on what you have observed and done. ` +
	`Below are your recent memories. Synthesise at most three brief, high-level insights about what is happening - ` +
	`the patterns, surprises, or implications that a low-level memory stream misses. ` +
	`Output one insight per line, prefixed with "- ", and nothing else. Be concise.`

type Reflector struct {
	Brain brain.Brain

	Interval uint64

	Lookback int

	MaxInsights int

	MinRawMemories int
}

func (r *Reflector) ShouldReflect(tick world.Tick) bool {
	if r == nil || r.Interval == 0 {
		return false
	}
	if tick == 0 {
		return false
	}
	return uint64(tick)%r.Interval == 0
}

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

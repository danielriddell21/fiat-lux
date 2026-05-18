package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

// LLMScorer asks the configured brain to rate a memory's importance
// on a 0-10 scale. Opt-in via:
//
//	--importance=llm
//
// Each Add becomes an extra brain call so this is meaningfully more
// expensive than the heuristic default; meant for cloud users who
// care about retrieval quality and have budget.
type LLMScorer struct {
	// Brain is the model used for scoring. We pass the agent's own
	// brain; multi-brain configs could route this to a cheaper model
	// specifically.
	Brain brain.Brain
}

// Score implements Importance by asking the brain for a single
// number between 0 and 10. On any parse failure or brain error it
// falls back to the heuristic so the pipeline keeps running.
func (l LLMScorer) Score(ctx context.Context, kind Kind, content string) (float64, error) {
	if l.Brain == nil {
		return HeuristicScorer{}.Score(ctx, kind, content)
	}
	p := brain.Perception{
		Memories: []brain.MemoryView{{Content: scorePromptUser(kind, content), Importance: 0}},
	}
	decision, err := l.Brain.Decide(ctx, p, nil)
	if err != nil {
		return HeuristicScorer{}.Score(ctx, kind, content)
	}
	if got, ok := parseScore(decision.Thought); ok {
		return got, nil
	}
	return HeuristicScorer{}.Score(ctx, kind, content)
}

func scorePromptUser(kind Kind, content string) string {
	return fmt.Sprintf("Rate the importance of this %s memory on a 0-10 scale, where 0 is "+
		"trivial/forgettable and 10 is a life-defining event. Reply with the number only.\n\n%s",
		kind, content)
}

// parseScore extracts the rating from the brain's response. Models
// often phrase their answer like "On a scale of 0 to 10 I'd say 4",
// so we take the LAST parseable number in [0,10]; ranges and prose
// at the start of the string don't fool us.
func parseScore(text string) (float64, bool) {
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.' && r != '-'
	})
	for i := len(tokens) - 1; i >= 0; i-- {
		v, err := strconv.ParseFloat(tokens[i], 64)
		if err != nil {
			continue
		}
		if v < 0 {
			continue
		}
		if v > 10 {
			v = 10
		}
		return v, true
	}
	return 0, false
}

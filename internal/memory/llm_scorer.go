package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

type LLMScorer struct {
	Brain brain.Brain
}

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

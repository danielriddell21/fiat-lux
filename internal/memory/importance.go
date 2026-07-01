package memory

import (
	"context"
	"strings"
	"unicode/utf8"
)

type Importance interface {
	Score(ctx context.Context, kind Kind, content string) (float64, error)
}

type HeuristicScorer struct{}

func (HeuristicScorer) Score(_ context.Context, kind Kind, content string) (float64, error) {
	base := 4.0
	switch kind {
	case KindReflection:
		base = 8.0
	case KindAction:
		base = 5.0
	case KindOutcome:
		base = 4.5
	case KindObservation:
		base = 3.0
	case KindThought:
		base = 3.5
	}

	// Length adjustment - terse single-word content sits below the
	// kind baseline, longer rationale climbs above it. Capped at
	// +/- 2 so a wall of text can't overwhelm a kind-set base.
	length := utf8.RuneCountInString(strings.TrimSpace(content))
	switch {
	case length <= 10:
		base -= 1.0
	case length <= 30:
		// no adjustment
	case length <= 120:
		base += 0.5
	case length <= 400:
		base += 1.0
	default:
		base += 2.0
	}

	// Phrases that often herald a meaningful shift get a small bump.
	lower := strings.ToLower(content)
	for _, kw := range []string{"first", "discovered", "realised", "realized", "important", "now i"} {
		if strings.Contains(lower, kw) {
			base += 0.5
			break
		}
	}

	if base < 1.0 {
		base = 1.0
	}
	if base > 10.0 {
		base = 10.0
	}
	return base, nil
}

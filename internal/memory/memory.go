package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// RecordID is the stable identifier of a memory record within a
// single stream. IDs are assigned monotonically starting at 1.
type RecordID uint64

// Kind classifies a memory record so the heuristic scorer and
// reflection pass can treat the different categories distinctly.
type Kind string

// The complete set of memory kinds the sim emits.
const (
	KindObservation Kind = "observation" // world events the agent perceived
	KindAction      Kind = "action"      // a tool call the agent issued
	KindOutcome     Kind = "outcome"     // the tool's result string
	KindThought     Kind = "thought"     // inner monologue from the brain
	KindReflection  Kind = "reflection"  // higher-level synthesis
)

// Record is one entry in an agent's memory stream. Smallville's
// "MemoryRecord" - id, owner, when, what, why-it-matters, how-to-find.
type Record struct {
	ID          RecordID
	AgentID     world.AgentID
	Kind        Kind
	Content     string
	CreatedAt   world.Tick
	CreatedWall time.Time
	Importance  float64   // 0..10 scale
	Embedding   []float64 // nil-or-empty when no embedder is configured
	// LastAccessTick tracks when this record was most recently
	// retrieved; recency is measured as "since last access" rather
	// than "since creation". Zero means never retrieved.
	LastAccessTick world.Tick
}

// ScoreWeights controls the relative contributions of the three
// Smallville factors. Defaults sum to 1.0; callers may tune.
type ScoreWeights struct {
	Recency    float64
	Importance float64
	Relevance  float64
}

// DefaultWeights are roughly equal across the three factors with a
// gentle relevance bias - intuition: in a fast-moving sim, "what's
// going on right now" usually wins over "what mattered a long time
// ago".
func DefaultWeights() ScoreWeights {
	return ScoreWeights{Recency: 0.5, Importance: 1.0, Relevance: 1.0}
}

// Stream is one agent's memory stream. Safe for concurrent use.
type Stream struct {
	mu      sync.RWMutex
	records []Record
	nextID  RecordID

	weights  ScoreWeights
	halfLife float64
}

// New constructs an empty Stream. halfLife is the recency
// exponential's half-life in ticks; default 100.
func New(weights ScoreWeights, halfLife float64) *Stream {
	if halfLife <= 0 {
		halfLife = 100
	}
	if weights == (ScoreWeights{}) {
		weights = DefaultWeights()
	}
	return &Stream{
		nextID:   1,
		weights:  weights,
		halfLife: halfLife,
	}
}

// Len returns the total number of records.
func (s *Stream) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// All returns deep copies of every record, in insertion order.
// Exposed for the TUI memory inspector and tests.
func (s *Stream) All() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, len(s.records))
	copy(out, s.records)
	for i := range out {
		if len(out[i].Embedding) > 0 {
			cp := make([]float64, len(out[i].Embedding))
			copy(cp, out[i].Embedding)
			out[i].Embedding = cp
		}
	}
	return out
}

// AddOptions carries the per-Add knobs that don't fit on the
// minimal (kind, content, tick) trio.
type AddOptions struct {
	// Embedder, when non-nil, is called to compute the record's
	// embedding before insertion. Failures are surfaced as Add's
	// returned error; the record is not inserted.
	Embedder Embedder

	// Scorer, when non-nil, overrides importance scoring. Nil falls
	// back to the heuristic Importance.
	Scorer Importance

	// Importance is a pre-computed override. When > 0 it bypasses
	// the Scorer.
	Importance float64
}

// Add appends a record to the stream. The record is assigned an ID
// and timestamped. The Embedder and Scorer (or the heuristic
// Importance) annotate the record before insertion.
func (s *Stream) Add(ctx context.Context, ag world.AgentID, kind Kind, content string, tick world.Tick, opts AddOptions) (Record, error) {
	if content == "" {
		return Record{}, errors.New("memory: empty content")
	}

	var emb []float64
	if opts.Embedder != nil {
		v, err := opts.Embedder.Embed(ctx, content)
		if err != nil {
			return Record{}, fmt.Errorf("memory: embed: %w", err)
		}
		emb = v
	}

	importance := opts.Importance
	if importance <= 0 {
		scorer := opts.Scorer
		if scorer == nil {
			scorer = HeuristicScorer{}
		}
		score, err := scorer.Score(ctx, kind, content)
		if err != nil {
			return Record{}, fmt.Errorf("memory: importance: %w", err)
		}
		importance = score
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r := Record{
		ID:          s.nextID,
		AgentID:     ag,
		Kind:        kind,
		Content:     content,
		CreatedAt:   tick,
		CreatedWall: time.Now(),
		Importance:  importance,
		Embedding:   emb,
	}
	s.nextID++
	s.records = append(s.records, r)
	return r, nil
}

// Retrieve returns the top-K records by combined recency *
// importance * relevance score, given a query string the embedder
// hashes into a query vector for relevance.
//
// k <= 0 returns no records. An empty stream returns an empty
// slice with no error. When the embedder is nil or the query is
// empty, relevance contributes zero (the score reduces to recency
// * importance).
func (s *Stream) Retrieve(ctx context.Context, query string, tick world.Tick, k int, embedder Embedder) ([]Record, error) {
	if k <= 0 {
		return nil, nil
	}
	s.mu.RLock()
	if len(s.records) == 0 {
		s.mu.RUnlock()
		return nil, nil
	}
	weights := s.weights
	halfLife := s.halfLife
	records := make([]Record, len(s.records))
	copy(records, s.records)
	s.mu.RUnlock()

	var queryVec []float64
	if embedder != nil && query != "" {
		v, err := embedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("memory: embed query: %w", err)
		}
		queryVec = v
	}

	type scored struct {
		r     Record
		score float64
	}
	xs := make([]scored, len(records))
	for i, r := range records {
		// Recency uses "since most recent contact" - either last
		// access or creation - so a memory retrieved again resets
		// its recency. Follows Smallville here.
		anchor := r.CreatedAt
		if r.LastAccessTick > anchor {
			anchor = r.LastAccessTick
		}
		rec := recencyScore(tick, anchor, halfLife)
		imp := r.Importance / 10.0
		var rel float64
		if len(queryVec) > 0 && len(r.Embedding) > 0 {
			rel = cosine(queryVec, r.Embedding)
			if rel < 0 {
				rel = 0
			}
		}
		xs[i] = scored{
			r:     r,
			score: rec*weights.Recency + imp*weights.Importance + rel*weights.Relevance,
		}
	}
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].score != xs[j].score {
			return xs[i].score > xs[j].score
		}
		// Tie-break on recency, then on ID, so retrieval order is
		// deterministic regardless of insertion order.
		if xs[i].r.CreatedAt != xs[j].r.CreatedAt {
			return xs[i].r.CreatedAt > xs[j].r.CreatedAt
		}
		return xs[i].r.ID > xs[j].r.ID
	})

	limit := k
	if limit > len(xs) {
		limit = len(xs)
	}
	out := make([]Record, limit)
	idsTouched := make([]RecordID, limit)
	for i := 0; i < limit; i++ {
		out[i] = xs[i].r
		idsTouched[i] = xs[i].r.ID
	}

	// Bump LastAccessTick for the touched records.
	s.mu.Lock()
	for _, id := range idsTouched {
		for j := range s.records {
			if s.records[j].ID == id {
				if tick > s.records[j].LastAccessTick {
					s.records[j].LastAccessTick = tick
				}
				break
			}
		}
	}
	s.mu.Unlock()
	return out, nil
}

// PerceptionViews converts Records to brain.MemoryViews for
// injection into a Perception.
func PerceptionViews(rs []Record) []brain.MemoryView {
	out := make([]brain.MemoryView, len(rs))
	for i, r := range rs {
		out[i] = brain.MemoryView{
			Content:    r.Content,
			Tick:       uint64(r.CreatedAt),
			Importance: r.Importance,
		}
	}
	return out
}

func recencyScore(now, anchor world.Tick, halfLife float64) float64 {
	delta := float64(now) - float64(anchor)
	if delta <= 0 {
		return 1.0
	}
	return math.Pow(0.5, delta/halfLife)
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// SummariseQuery is the canonical query string the sim feeds into
// Retrieve. It is a stable, agent-side projection of perception
// that the embedder can hash. Exposed so the sim and tests share a
// single definition.
func SummariseQuery(entities int, recent []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "world with %d entities", entities)
	if len(recent) > 0 {
		b.WriteString("; recent: ")
		b.WriteString(strings.Join(recent, "; "))
	}
	return b.String()
}

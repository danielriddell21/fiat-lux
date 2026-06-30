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

type RecordID uint64

type Kind string

const (
	KindObservation Kind = "observation"
	KindAction      Kind = "action"
	KindOutcome     Kind = "outcome"
	KindThought     Kind = "thought"
	KindReflection  Kind = "reflection"
	KindInheritance Kind = "inheritance"
	KindChapter     Kind = "chapter"
)

type Record struct {
	ID          RecordID      `json:"id"`
	AgentID     world.AgentID `json:"agent_id"`
	Kind        Kind          `json:"kind"`
	Content     string        `json:"content"`
	CreatedAt   world.Tick    `json:"created_at"`
	CreatedWall time.Time     `json:"created_wall"`
	Importance  float64       `json:"importance"`
	Embedding   []float64     `json:"-"`

	LastAccessTick world.Tick `json:"last_access_tick,omitempty"`
}

type ScoreWeights struct {
	Recency    float64
	Importance float64
	Relevance  float64
}

func DefaultWeights() ScoreWeights {
	return ScoreWeights{Recency: 0.5, Importance: 1.0, Relevance: 1.0}
}

type Stream struct {
	mu      sync.RWMutex
	records []Record
	nextID  RecordID

	weights  ScoreWeights
	halfLife float64
}

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

func (s *Stream) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

func (s *Stream) Restore(records []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records[:0], records...)
	var maxID RecordID
	for _, r := range records {
		if r.ID > maxID {
			maxID = r.ID
		}
	}
	s.nextID = maxID + 1
	if s.nextID == 0 {
		s.nextID = 1
	}
}

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

type AddOptions struct {
	Embedder Embedder

	Scorer Importance

	Importance float64
}

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

func (s *Stream) Retrieve(ctx context.Context, query string, tick world.Tick, k int, embedder Embedder) ([]Record, error) {
	if k <= 0 {
		return nil, nil
	}
	records, weights, halfLife, ok := s.snapshot()
	if !ok {
		return nil, nil
	}

	queryVec, err := embedQuery(ctx, query, embedder)
	if err != nil {
		return nil, err
	}

	ranked := rankRecords(records, queryVec, tick, weights, halfLife)

	limit := k
	if limit > len(ranked) {
		limit = len(ranked)
	}
	out := make([]Record, limit)
	idsTouched := make([]RecordID, limit)
	for i := 0; i < limit; i++ {
		out[i] = ranked[i]
		idsTouched[i] = ranked[i].ID
	}

	s.bumpAccess(idsTouched, tick)
	return out, nil
}

func (s *Stream) snapshot() (records []Record, weights ScoreWeights, halfLife float64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.records) == 0 {
		return nil, ScoreWeights{}, 0, false
	}
	records = make([]Record, len(s.records))
	copy(records, s.records)
	return records, s.weights, s.halfLife, true
}

func embedQuery(ctx context.Context, query string, embedder Embedder) ([]float64, error) {
	if embedder == nil || query == "" {
		return nil, nil
	}
	v, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory: embed query: %w", err)
	}
	return v, nil
}

func rankRecords(records []Record, queryVec []float64, tick world.Tick, weights ScoreWeights, halfLife float64) []Record {
	type scored struct {
		r     Record
		score float64
	}
	xs := make([]scored, len(records))
	for i, r := range records {
		xs[i] = scored{r: r, score: recordScore(r, queryVec, tick, weights, halfLife)}
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
	out := make([]Record, len(xs))
	for i := range xs {
		out[i] = xs[i].r
	}
	return out
}

func recordScore(r Record, queryVec []float64, tick world.Tick, weights ScoreWeights, halfLife float64) float64 {
	// Recency uses "since most recent contact" - either last access or
	// creation - so a memory retrieved again resets its recency.
	// Follows Smallville here.
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
	return rec*weights.Recency + imp*weights.Importance + rel*weights.Relevance
}

func (s *Stream) bumpAccess(ids []RecordID, tick world.Tick) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		for j := range s.records {
			if s.records[j].ID == id {
				if tick > s.records[j].LastAccessTick {
					s.records[j].LastAccessTick = tick
				}
				break
			}
		}
	}
}

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

func SummariseQuery(entities int, recent []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "world with %d entities", entities)
	if len(recent) > 0 {
		b.WriteString("; recent: ")
		b.WriteString(strings.Join(recent, "; "))
	}
	return b.String()
}

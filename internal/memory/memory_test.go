package memory

import (
	"context"
	"math"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

const tAgent world.AgentID = 1

func TestNew_EmptyStream(t *testing.T) {
	t.Parallel()
	s := New(ScoreWeights{}, 0)
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	got, err := s.Retrieve(context.Background(), "anything", 0, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("retrieve on empty stream returned %d records", len(got))
	}
}

func TestAdd_AssignsIDAndScoresImportance(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	r, err := s.Add(context.Background(), tAgent, KindAction, "I made a planet", 5, AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 1 {
		t.Errorf("first ID = %d, want 1", r.ID)
	}
	if r.AgentID != tAgent {
		t.Errorf("AgentID = %d, want %d", r.AgentID, tAgent)
	}
	if r.Importance <= 0 {
		t.Errorf("Importance = %v, want > 0", r.Importance)
	}
	if r.CreatedAt != 5 {
		t.Errorf("CreatedAt = %d, want 5", r.CreatedAt)
	}
}

func TestAdd_RejectsEmptyContent(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	if _, err := s.Add(context.Background(), tAgent, KindAction, "", 0, AddOptions{}); err == nil {
		t.Errorf("expected error for empty content")
	}
}

func TestAdd_WithEmbedder(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	r, err := s.Add(context.Background(), tAgent, KindAction, "I made a planet", 0, AddOptions{
		Embedder: HashEmbedder{Dim: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Embedding) != 16 {
		t.Errorf("Embedding len = %d, want 16", len(r.Embedding))
	}
}

func TestRetrieve_RecencyDominatesWhenImportanceEqual(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 10)
	ctx := context.Background()

	// All same kind and length so heuristic importance is the same.
	r1, _ := s.Add(ctx, tAgent, KindAction, "alpha alpha", 0, AddOptions{})
	_, _ = s.Add(ctx, tAgent, KindAction, "beta beta", 5, AddOptions{})
	r3, _ := s.Add(ctx, tAgent, KindAction, "gamma gamma", 10, AddOptions{})

	got, err := s.Retrieve(ctx, "", 10, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}
	if got[0].ID != r3.ID {
		t.Errorf("most recent should rank first; got ID %d, want %d", got[0].ID, r3.ID)
	}
	if got[2].ID != r1.ID {
		t.Errorf("oldest should rank last; got ID %d, want %d", got[2].ID, r1.ID)
	}
}

func TestRetrieve_ImportanceBoostsLowerRecency(t *testing.T) {
	t.Parallel()
	s := New(ScoreWeights{Recency: 0.2, Importance: 2.0, Relevance: 0}, 5)
	ctx := context.Background()
	// Old but high-importance reflection vs newer trivial action.
	hi, _ := s.Add(ctx, tAgent, KindReflection, "the world is now alive with structure", 0, AddOptions{})
	_, _ = s.Add(ctx, tAgent, KindAction, "tap", 100, AddOptions{})

	got, _ := s.Retrieve(ctx, "", 100, 1, nil)
	if got[0].ID != hi.ID {
		t.Errorf("expected high-importance reflection to win; got ID %d", got[0].ID)
	}
}

func TestRetrieve_RelevanceWithEmbedder(t *testing.T) {
	t.Parallel()
	emb := HashEmbedder{Dim: 32}
	s := New(ScoreWeights{Recency: 0.1, Importance: 0.1, Relevance: 5.0}, 100)
	ctx := context.Background()

	// "planet of erith" gets a query for "planet erith" - should
	// outrank an unrelated memory.
	target, _ := s.Add(ctx, tAgent, KindObservation, "planet of erith", 0, AddOptions{Embedder: emb})
	_, _ = s.Add(ctx, tAgent, KindObservation, "completely unrelated content", 0, AddOptions{Embedder: emb})

	got, err := s.Retrieve(ctx, "planet of erith", 0, 1, emb)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != target.ID {
		t.Errorf("relevance should pull target first; got ID %d", got[0].ID)
	}
}

func TestRetrieve_NoEmbedderFallsBack(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	_, _ = s.Add(ctx, tAgent, KindAction, "alpha", 0, AddOptions{})
	got, err := s.Retrieve(ctx, "anything", 0, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d records, want 1", len(got))
	}
}

func TestRetrieve_BumpsLastAccess(t *testing.T) {
	t.Parallel()
	s := New(DefaultWeights(), 100)
	ctx := context.Background()
	r, _ := s.Add(ctx, tAgent, KindAction, "alpha", 0, AddOptions{})
	if r.LastAccessTick != 0 {
		t.Errorf("LastAccessTick = %d, want 0 on insert", r.LastAccessTick)
	}
	_, _ = s.Retrieve(ctx, "", 42, 1, nil)
	all := s.All()
	if all[0].LastAccessTick != 42 {
		t.Errorf("LastAccessTick = %d, want 42 after Retrieve", all[0].LastAccessTick)
	}
}

func TestRecencyScore(t *testing.T) {
	t.Parallel()
	// half-life = 10; t=0 -> 1.0, t=10 -> 0.5, t=20 -> 0.25
	if got := recencyScore(0, 0, 10); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("recency(0,0) = %v, want 1.0", got)
	}
	if got := recencyScore(10, 0, 10); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("recency(10,0) = %v, want 0.5", got)
	}
	if got := recencyScore(20, 0, 10); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("recency(20,0) = %v, want 0.25", got)
	}
}

func TestCosine(t *testing.T) {
	t.Parallel()
	if got := cosine([]float64{1, 0}, []float64{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Errorf("self cosine = %v, want 1", got)
	}
	if got := cosine([]float64{1, 0}, []float64{0, 1}); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal cosine = %v, want 0", got)
	}
	if got := cosine([]float64{1, 0}, []float64{0, 0}); got != 0 {
		t.Errorf("zero-vector cosine = %v, want 0", got)
	}
}

func TestHeuristic_Calibration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := HeuristicScorer{}

	cases := []struct {
		kind    Kind
		content string
		minWant float64
		maxWant float64
	}{
		{KindReflection, "the world is now alive with rich, complex structure", 8.0, 10.0},
		{KindAction, "I tapped a thing", 4.0, 6.0},
		{KindObservation, "x", 1.0, 3.5},
	}
	for _, c := range cases {
		got, err := h.Score(ctx, c.kind, c.content)
		if err != nil {
			t.Fatal(err)
		}
		if got < c.minWant || got > c.maxWant {
			t.Errorf("kind=%q content=%q -> %v; want in [%v, %v]", c.kind, c.content, got, c.minWant, c.maxWant)
		}
	}
}

func TestPerceptionViews(t *testing.T) {
	t.Parallel()
	rs := []Record{
		{Content: "a", CreatedAt: 1, Importance: 3.0},
		{Content: "b", CreatedAt: 2, Importance: 8.0},
	}
	v := PerceptionViews(rs)
	if len(v) != 2 {
		t.Fatalf("got %d views, want 2", len(v))
	}
	if v[0].Content != "a" || v[1].Tick != 2 || v[1].Importance != 8.0 {
		t.Errorf("views = %+v", v)
	}
}

func TestHashEmbedder_DeterministicAndNormalised(t *testing.T) {
	t.Parallel()
	emb := HashEmbedder{Dim: 16}
	v1, err := emb.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := emb.Embed(context.Background(), "hello")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("hash embedder not deterministic at index %d", i)
		}
	}
	var norm float64
	for _, x := range v1 {
		norm += x * x
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-9 {
		t.Errorf("hash embedder norm = %v, want 1", math.Sqrt(norm))
	}
}

func TestZeroEmbedder(t *testing.T) {
	t.Parallel()
	v, err := ZeroEmbedder{}.Embed(context.Background(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range v {
		if x != 0 {
			t.Errorf("zero embedder has non-zero component: %v", x)
		}
	}
}

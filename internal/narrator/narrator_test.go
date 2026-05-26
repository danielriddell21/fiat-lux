package narrator

import (
	"context"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type fakeBrain struct {
	thought string
	called  int
}

func (f *fakeBrain) Decide(_ context.Context, _ brain.Perception, _ []brain.ToolDef) (brain.Decision, error) {
	f.called++
	return brain.Decision{Thought: f.thought}, nil
}
func (f *fakeBrain) Model() string    { return "fake" }
func (f *fakeBrain) Provider() string { return "fake" }
func (f *fakeBrain) Close() error     { return nil }

func TestNew_RequiresBrain(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Error("expected error with nil brain")
	}
}

func TestShouldChapter_RespectsIntervalAndMinEvents(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	n, _ := New(Options{Brain: &fakeBrain{}, IntervalTicks: 5, MinEvents: 2})
	if n.ShouldChapter(w) {
		t.Error("fresh world with no events should not warrant a chapter")
	}
	for range 3 {
		if _, err := w.Create(world.NoAgent, "thing", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Tick is still 0; interval requires advancing to >= 5.
	if n.ShouldChapter(w) {
		t.Error("interval should not yet allow a chapter")
	}
	for range 6 {
		_ = w.AdvanceTick()
	}
	if !n.ShouldChapter(w) {
		t.Error("expected ShouldChapter to return true once interval and event threshold are met")
	}
}

func TestWriteChapter_ResetsCounters(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	for range 4 {
		if _, err := w.Create(world.NoAgent, "thing", nil); err != nil {
			t.Fatal(err)
		}
	}
	for range 5 {
		_ = w.AdvanceTick()
	}
	br := &fakeBrain{thought: "and on the fifth tick, somethings arose."}
	n, _ := New(Options{Brain: br, IntervalTicks: 5, MinEvents: 2})
	if !n.ShouldChapter(w) {
		t.Fatal("expected first chapter to be due")
	}
	ch, err := n.WriteChapter(context.Background(), w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Content == "" {
		t.Error("chapter content empty")
	}
	if ch.WorldName != "kosmos" {
		t.Errorf("world name = %q", ch.WorldName)
	}
	if n.ShouldChapter(w) {
		t.Error("immediately after WriteChapter, ShouldChapter should be false")
	}
	if br.called != 1 {
		t.Errorf("brain called = %d, want 1", br.called)
	}
}

func TestWriteChapter_TrimsContent(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	for range 6 {
		_, _ = w.Create(world.NoAgent, "thing", nil)
	}
	for range 5 {
		_ = w.AdvanceTick()
	}
	long := make([]byte, 2000)
	for i := range long {
		long[i] = 'x'
	}
	br := &fakeBrain{thought: string(long)}
	n, _ := New(Options{Brain: br, IntervalTicks: 5, MinEvents: 1, MaxChapterLength: 100})
	ch, err := n.WriteChapter(context.Background(), w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Content) != 100 {
		t.Errorf("chapter length = %d, want 100", len(ch.Content))
	}
}

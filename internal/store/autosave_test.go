package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// fakeSaver records every Save call.
type fakeSaver struct {
	mu       sync.Mutex
	count    atomic.Int64
	lastTick world.Tick
	err      error
}

func (f *fakeSaver) Save(_ context.Context, w *world.World) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count.Add(1)
	f.lastTick = w.Tick()
	return f.err
}

func TestRunAutosave_FiresOnInterval(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	if _, err := w.Create(world.NoAgent, "planet", nil); err != nil {
		t.Fatal(err)
	}
	saver := &fakeSaver{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Simulate sim activity: keep advancing the tick so the world
	// gains a new event between each autosave fire.
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.AdvanceTick()
			}
		}
	}()

	done := make(chan error, 1)
	go func() { done <- RunAutosave(ctx, saver, w, 30*time.Millisecond, nil) }()

	// 150ms / 30ms = ~5 ticks. Allow scheduler slack.
	time.Sleep(150 * time.Millisecond)
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("RunAutosave returned %v, want context.Canceled", err)
	}
	got := saver.count.Load()
	if got < 3 {
		t.Errorf("Save calls = %d, want >= 3 across 150ms / 30ms interval", got)
	}
}

func TestRunAutosave_SkipsWhenIdle(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	if _, err := w.Create(world.NoAgent, "planet", nil); err != nil {
		t.Fatal(err)
	}
	saver := &fakeSaver{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = RunAutosave(ctx, saver, w, 20*time.Millisecond, nil) }()

	// Wait long enough for several ticks. The world hasn't gained
	// events since the first save, so the saver count must plateau.
	time.Sleep(120 * time.Millisecond)
	first := saver.count.Load()
	time.Sleep(80 * time.Millisecond)
	second := saver.count.Load()
	cancel()

	if first == 0 {
		t.Fatalf("expected at least one initial save, got 0")
	}
	if second != first {
		t.Errorf("save count advanced while idle: first=%d second=%d", first, second)
	}
}

func TestRunAutosave_ResumesAfterChange(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	if _, err := w.Create(world.NoAgent, "planet", nil); err != nil {
		t.Fatal(err)
	}
	saver := &fakeSaver{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = RunAutosave(ctx, saver, w, 20*time.Millisecond, nil) }()

	time.Sleep(80 * time.Millisecond)
	before := saver.count.Load()

	// Add another event; the next tick should pick it up.
	if _, err := w.Create(world.NoAgent, "ocean", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	after := saver.count.Load()
	cancel()

	if after <= before {
		t.Errorf("save count did not resume after world change: before=%d after=%d", before, after)
	}
}

func TestRunAutosave_ContinuesPastSaveErrors(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	if _, err := w.Create(world.NoAgent, "planet", nil); err != nil {
		t.Fatal(err)
	}
	saver := &fakeSaver{err: errors.New("simulated failure")}
	var errCount atomic.Int64

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = RunAutosave(ctx, saver, w, 20*time.Millisecond, func(error) {
			errCount.Add(1)
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	if errCount.Load() < 2 {
		t.Errorf("expected multiple error callbacks; got %d", errCount.Load())
	}
}

func TestRunAutosave_RejectsBadInputs(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	ctx := context.Background()
	if err := RunAutosave(ctx, nil, w, time.Second, nil); err == nil {
		t.Errorf("nil saver: expected error")
	}
	if err := RunAutosave(ctx, &fakeSaver{}, nil, time.Second, nil); err == nil {
		t.Errorf("nil world: expected error")
	}
	if err := RunAutosave(ctx, &fakeSaver{}, w, 0, nil); err == nil {
		t.Errorf("zero interval: expected error")
	}
}

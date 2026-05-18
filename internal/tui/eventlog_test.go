package tui

import (
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

func TestRenderEventLog_EmptyPlaceholder(t *testing.T) {
	t.Parallel()
	out := RenderEventLog(nil, 5, DefaultStyles())
	if !strings.Contains(out, "no events") {
		t.Errorf("empty log missing placeholder: %q", out)
	}
}

func TestRenderEventLog_TruncatesToLastN(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	for i := 0; i < 10; i++ {
		if _, err := w.Create(world.NoAgent, "thing", nil); err != nil {
			t.Fatal(err)
		}
	}
	events := w.Events()
	out := RenderEventLog(events, 3, DefaultStyles())
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
}

func TestRenderEventLog_RendersAllEventKinds(t *testing.T) {
	t.Parallel()
	w := mustWorld(t)
	a, _ := w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	b, _ := w.Create(world.NoAgent, "ocean", nil)
	_ = w.AdvanceTick()
	_ = w.Modify(world.NoAgent, b, world.Properties{"depth": float64(7)})
	rid, _ := w.Relate(world.NoAgent, b, a, "part of")
	_ = w.Unrelate(world.NoAgent, rid)
	_ = w.Destroy(world.NoAgent, a)

	out := RenderEventLog(w.Events(), 0, DefaultStyles())
	for _, want := range []string{"create", "modify", "relate", "unrelate", "destroy", "tick_start", "part of"} {
		if !strings.Contains(out, want) {
			t.Errorf("event log missing %q\noutput:\n%s", want, out)
		}
	}
}

package tui

import (
	"strings"
	"testing"
)

func TestRenderStatus_IncludesCoreFields(t *testing.T) {
	t.Parallel()
	out := RenderStatus(StatusInfo{
		WorldName:   "kosmos",
		Tick:        42,
		EntityCount: 17,
		AgentCount:  3,
		Paused:      false,
	}, DefaultStyles())

	for _, want := range []string{"fiat-lux", "kosmos", "42", "17", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q\noutput: %s", want, out)
		}
	}
	if strings.Contains(out, "PAUSED") {
		t.Errorf("status unexpectedly contains PAUSED: %s", out)
	}
}

func TestRenderStatus_PausedFlag(t *testing.T) {
	t.Parallel()
	out := RenderStatus(StatusInfo{WorldName: "kosmos", Paused: true}, DefaultStyles())
	if !strings.Contains(out, "PAUSED") {
		t.Errorf("paused status missing PAUSED marker: %s", out)
	}
}

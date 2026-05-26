package tui

import (
	"strings"
	"testing"
)

func TestRenderLineage_EmptyRoster(t *testing.T) {
	t.Parallel()
	out := RenderLineage(nil, DefaultStyles())
	if !strings.Contains(out, "(no agents)") {
		t.Errorf("RenderLineage(nil) = %q", out)
	}
}

func TestRenderLineage_ShowsParentChild(t *testing.T) {
	t.Parallel()
	agents := []AgentInfo{
		{ID: 1, Name: "creator", IsCreator: true, ParentID: 0},
		{ID: 2, Name: "child", ParentID: 1},
		{ID: 3, Name: "grandchild", ParentID: 2},
	}
	out := RenderLineage(agents, DefaultStyles())
	if !strings.Contains(out, "creator") || !strings.Contains(out, "child") || !strings.Contains(out, "grandchild") {
		t.Errorf("missing agent in lineage output: %s", out)
	}
	// child should be indented more than creator; grandchild more than child.
	creatorLine := lineWith(out, "creator")
	childLine := lineWith(out, "child #2")
	grandLine := lineWith(out, "grandchild")
	if leading(creatorLine) >= leading(childLine) {
		t.Errorf("expected child indented more than creator: c=%q ch=%q", creatorLine, childLine)
	}
	if leading(childLine) >= leading(grandLine) {
		t.Errorf("expected grandchild indented more than child: ch=%q g=%q", childLine, grandLine)
	}
}

func TestRenderLineage_MarksDead(t *testing.T) {
	t.Parallel()
	out := RenderLineage([]AgentInfo{
		{ID: 1, Name: "ghost", IsCreator: true, Dead: true},
	}, DefaultStyles())
	if !strings.Contains(out, "[dead]") {
		t.Errorf("expected [dead] badge: %s", out)
	}
}

func lineWith(s, substr string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

func leading(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else {
			break
		}
	}
	return n
}

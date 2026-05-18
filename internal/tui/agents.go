package tui

import (
	"fmt"
	"strings"
)

// RenderAgentsStrip renders a one-line summary of the agent roster
// with the focused agent highlighted. Shown above the reasoning
// pane when a sim with multiple agents is attached.
func RenderAgentsStrip(agents []AgentInfo, focused uint64, s Styles) string {
	if len(agents) == 0 {
		return ""
	}
	parts := make([]string, 0, len(agents))
	for _, a := range agents {
		label := a.Name
		if a.IsCreator {
			label = "✦ " + label
		}
		label = fmt.Sprintf("%s #%d", label, a.ID)
		if a.ID == focused {
			parts = append(parts, s.HelpKey.Render("["+label+"]"))
		} else {
			parts = append(parts, s.Faint.Render(label))
		}
	}
	return strings.Join(parts, "  ")
}

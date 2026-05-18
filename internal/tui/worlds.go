package tui

import (
	"fmt"
	"strings"
)

// RenderWorldsStrip renders a one-line summary of the world roster
// with the focused world highlighted. Shown above the agent strip
// when a multi-world Universe is attached.
func RenderWorldsStrip(worlds []WorldInfo, focused int, s Styles) string {
	if len(worlds) < 2 {
		return ""
	}
	parts := make([]string, 0, len(worlds))
	for _, w := range worlds {
		label := fmt.Sprintf("%s (t%d, %d ents, %d agents)", w.Name, w.Tick, w.EntityCount, w.AgentCount)
		if w.Index == focused {
			parts = append(parts, s.HelpKey.Render("«"+label+"»"))
		} else {
			parts = append(parts, s.Faint.Render(label))
		}
	}
	return s.Faint.Render("worlds: ") + strings.Join(parts, "  ")
}

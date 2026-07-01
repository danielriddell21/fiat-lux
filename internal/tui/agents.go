package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

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

func RenderDrivesLine(agents []AgentInfo, focused uint64, s Styles) string {
	for _, a := range agents {
		if a.ID != focused || len(a.Drives) == 0 {
			continue
		}
		keys := slices.Sorted(maps.Keys(a.Drives))
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s:%.2f", k, a.Drives[k]))
		}
		return s.Faint.Render("drives  " + strings.Join(parts, "  "))
	}
	return ""
}

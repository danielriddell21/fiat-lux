package tui

import (
	"fmt"
	"slices"
	"strings"
)

// RenderLineage renders the agent roster as a parent->child tree.
// Dead agents are marked with a [dead] suffix. The output is plain
// indented text suitable for the lineage overlay (key "l").
func RenderLineage(agents []AgentInfo, s Styles) string {
	if len(agents) == 0 {
		return s.Faint.Render("(no agents)")
	}
	byParent := make(map[uint64][]AgentInfo, len(agents))
	for _, a := range agents {
		byParent[a.ParentID] = append(byParent[a.ParentID], a)
	}
	for k := range byParent {
		slices.SortFunc(byParent[k], func(x, y AgentInfo) int {
			if x.ID < y.ID {
				return -1
			}
			if x.ID > y.ID {
				return 1
			}
			return 0
		})
	}

	var b strings.Builder
	b.WriteString(s.PaneTitle.Render("agent lineage"))
	b.WriteString("\n\n")
	roots, ok := byParent[0]
	if !ok {
		// Fallback: any agent flagged as creator is treated as root.
		for _, a := range agents {
			if a.IsCreator {
				roots = append(roots, a)
			}
		}
	}
	for _, r := range roots {
		writeLineageNode(&b, r, byParent, 0, s)
	}
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("press l again or esc to close"))
	return b.String()
}

func writeLineageNode(b *strings.Builder, a AgentInfo, byParent map[uint64][]AgentInfo, depth int, s Styles) {
	indent := strings.Repeat("  ", depth)
	label := fmt.Sprintf("%s%s #%d", indent, a.Name, a.ID)
	if a.IsCreator {
		label = indent + "✦ " + label[len(indent):]
	}
	if a.Dead {
		label += s.Faint.Render(" [dead]")
	}
	b.WriteString(label)
	b.WriteByte('\n')
	for _, c := range byParent[a.ID] {
		writeLineageNode(b, c, byParent, depth+1, s)
	}
}

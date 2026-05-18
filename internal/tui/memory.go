package tui

import (
	"fmt"
	"strings"
)

// MemoryRecord is the TUI's minimal projection of memory.Record.
// Keeps internal/tui independent of internal/memory so its tests
// stay self-contained; cmd/fiatlux maps between them.
type MemoryRecord struct {
	ID         uint64
	Kind       string
	Content    string
	Tick       uint64
	Importance float64
}

// MemorySnapshot is the agent-state slice the inspector renders.
type MemorySnapshot struct {
	AgentName string
	Records   []MemoryRecord
}

// MemorySource is the read interface the TUI uses to populate the
// memory inspector overlay. The sim adapter in cmd/fiatlux
// satisfies it.
type MemorySource interface {
	MemorySnapshot() MemorySnapshot
}

// RenderMemoryOverlay renders the full memory list. It is shown when
// the user presses 'm'. The most recently inserted records appear
// first.
func RenderMemoryOverlay(snap MemorySnapshot, s Styles) string {
	var b strings.Builder
	title := "memory stream"
	if snap.AgentName != "" {
		title = fmt.Sprintf("memory stream - %s", snap.AgentName)
	}
	b.WriteString(s.PaneTitle.Render(title))
	b.WriteString("\n\n")

	if len(snap.Records) == 0 {
		b.WriteString(s.Faint.Render("(no memories yet)"))
		b.WriteString("\n\n")
		b.WriteString(s.Faint.Render("press 'm' or esc to close"))
		return b.String()
	}

	for i := len(snap.Records) - 1; i >= 0; i-- {
		r := snap.Records[i]
		header := fmt.Sprintf("t%-4d  %s  imp=%.1f",
			r.Tick,
			padRight(r.Kind, 11),
			r.Importance,
		)
		if r.Kind == "reflection" {
			// Visually distinct: italic, bold, yellow-toned so
			// reflections stand out.
			b.WriteString(s.Reflection.Render("✦ " + header))
			b.WriteByte('\n')
			b.WriteString("  ")
			b.WriteString(s.Reflection.Render(r.Content))
		} else {
			b.WriteString(s.HelpKey.Render(header))
			b.WriteByte('\n')
			b.WriteString("  ")
			b.WriteString(s.HelpDesc.Render(r.Content))
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("press 'm' or esc to close"))
	return b.String()
}

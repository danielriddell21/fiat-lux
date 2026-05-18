package tui

import (
	"fmt"
	"strings"
)

// RenderReasoning renders the reasoning pane. When a sim is attached
// it shows the focused agent's most recent thought and chosen tool;
// when no sim is wired (or no step has happened yet) it shows a
// placeholder pointing at the keymap.
func RenderReasoning(last StepSummary, simAttached bool, s Styles) string {
	if !simAttached {
		return s.Faint.Render("(no sim attached - press 'n' to add dummy entities)") +
			"\n" + s.Faint.Render("    wire a brain to see the creator's reasoning here.")
	}
	if last.Tick == 0 && last.ToolName == "" && last.Thought == "" {
		return s.Faint.Render("(press space to start the creator)") +
			"\n" + s.Faint.Render("    the reasoning pane will fill with the agent's monologue.")
	}

	var b strings.Builder
	header := s.PaneTitle.Render(fmt.Sprintf("tick %d", last.Tick))
	if last.Skipped {
		header += " " + s.Faint.Render("(skipped - perception unchanged)")
	}
	b.WriteString(header)
	b.WriteByte('\n')

	if last.Thought != "" {
		b.WriteString(s.Reasoning.Render(last.Thought))
		b.WriteByte('\n')
	}
	if last.ToolName != "" {
		b.WriteString("\n")
		b.WriteString(s.HelpKey.Render("→ "))
		b.WriteString(s.HelpKey.Render(last.ToolName))
		if last.ToolResult != "" {
			b.WriteByte('\n')
			b.WriteString(s.HelpDesc.Render("  " + last.ToolResult))
		}
		if last.ToolErr != nil {
			b.WriteByte('\n')
			b.WriteString(s.Paused.Render("  error: " + last.ToolErr.Error()))
		}
	}
	return b.String()
}

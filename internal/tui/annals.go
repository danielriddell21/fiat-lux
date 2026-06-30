package tui

import (
	"fmt"
	"strings"
)

func RenderAnnals(chapters []ChapterSummary, s Styles) string {
	var b strings.Builder
	b.WriteString(s.PaneTitle.Render("annals"))
	b.WriteString("\n\n")
	if len(chapters) == 0 {
		b.WriteString(s.Faint.Render("(no chapters yet; the narrator is silent)"))
		return b.String()
	}
	for i, c := range chapters {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s\n", s.HelpKey.Render(fmt.Sprintf("tick %d", c.Tick)))
		b.WriteString(s.Reasoning.Render(c.Content))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("press a again or esc to close"))
	return b.String()
}

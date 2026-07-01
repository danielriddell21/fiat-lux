package tui

import (
	"fmt"
	"strings"
)

type helpEntry struct {
	keys string
	desc string
}

func helpEntries() []helpEntry {
	return []helpEntry{
		{"q / esc / ctrl+c", "quit"},
		{"space", "pause/resume"},
		{"tab / shift+tab", "focus next/prev agent"},
		{"ctrl+tab / ctrl+shift+tab", "focus next/prev world"},
		{"+ / -", "speed up/down"},
		{"s", "save world to store"},
		{"m", "memory inspector"},
		{"l", "lineage overlay"},
		{"a", "annals (narrator log)"},
		{"i + c/d/s", "intervene as the void: create / destroy / speak"},
		{"n", "(debug) create a random entity"},
		{"x", "(debug) destroy the most recent entity"},
		{"r", "(debug) relate the two most recent entities"},
		{"t", "(debug) advance tick"},
		{"?", "toggle this help"},
	}
}

func RenderHelpFooter(s Styles) string {
	parts := []string{
		key("q", "quit", s),
		key("space", "pause", s),
		key("+/-", "speed", s),
		key("tab", "focus", s),
		key("m", "memory", s),
		key("s", "save", s),
		key("n", "new", s),
		key("?", "help", s),
	}
	return strings.Join(parts, "  ")
}

func key(k, label string, s Styles) string {
	return fmt.Sprintf("%s%s",
		s.HelpKey.Render("["+k+"]"),
		s.HelpDesc.Render(label),
	)
}

func RenderHelpOverlay(s Styles) string {
	var b strings.Builder
	b.WriteString(s.PaneTitle.Render("fiat-lux key bindings"))
	b.WriteString("\n\n")
	for _, h := range helpEntries() {
		fmt.Fprintf(&b, "  %s   %s\n",
			s.HelpKey.Render(padRight(h.keys, 20)),
			s.HelpDesc.Render(h.desc),
		)
	}
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("press ? again or esc to close"))
	return b.String()
}

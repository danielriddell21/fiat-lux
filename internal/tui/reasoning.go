package tui

import "fmt"

// RenderReasoning renders the reasoning pane. In Phase 2 there is
// no agent, so it shows a placeholder explaining the absence. Later
// phases will stream the focused agent's thought tokens here.
func RenderReasoning(focusedAgent string, s Styles) string {
	if focusedAgent == "" {
		return s.Faint.Render("(no agent yet - the world is empty and no creator has been spawned)") +
			"\n" + s.Faint.Render("    when Phase 3 lands this pane will stream the agent's inner monologue.")
	}
	return s.Reasoning.Render(fmt.Sprintf("focused: %s\n(no thoughts yet)", focusedAgent))
}

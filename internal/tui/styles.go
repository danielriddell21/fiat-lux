package tui

import lg "charm.land/lipgloss/v2"

// Styles is the visual theme. It is constructed once per session and
// passed by value into renderers.
type Styles struct {
	StatusBar   lg.Style
	StatusKey   lg.Style
	StatusValue lg.Style
	Paused      lg.Style

	PaneBorder lg.Style
	PaneTitle  lg.Style

	TreeRoot    lg.Style
	TreeBranch  lg.Style
	TreeLeaf    lg.Style
	TreeMeta    lg.Style
	TreeDeleted lg.Style

	EventTick   lg.Style
	EventAgent  lg.Style
	EventKind   lg.Style
	EventTarget lg.Style
	EventCasc   lg.Style

	Reasoning lg.Style
	HelpKey   lg.Style
	HelpDesc  lg.Style

	Faint lg.Style
}

// DefaultStyles returns the canonical theme. It uses 256-colour
// palette codes so it renders consistently across terminals.
func DefaultStyles() Styles {
	subtle := lg.Color("241")
	highlight := lg.Color("212")
	accent := lg.Color("117")
	warn := lg.Color("214")
	dim := lg.Color("243")

	return Styles{
		StatusBar:   lg.NewStyle().Background(lg.Color("236")).Foreground(lg.Color("252")).Padding(0, 1),
		StatusKey:   lg.NewStyle().Foreground(subtle),
		StatusValue: lg.NewStyle().Foreground(accent).Bold(true),
		Paused:      lg.NewStyle().Foreground(warn).Bold(true),

		PaneBorder: lg.NewStyle().Border(lg.RoundedBorder()).BorderForeground(subtle),
		PaneTitle:  lg.NewStyle().Foreground(highlight).Bold(true),

		TreeRoot:    lg.NewStyle().Foreground(highlight).Bold(true),
		TreeBranch:  lg.NewStyle().Foreground(lg.Color("252")),
		TreeLeaf:    lg.NewStyle().Foreground(lg.Color("250")),
		TreeMeta:    lg.NewStyle().Foreground(dim).Italic(true),
		TreeDeleted: lg.NewStyle().Foreground(lg.Color("196")).Strikethrough(true),

		EventTick:   lg.NewStyle().Foreground(dim),
		EventAgent:  lg.NewStyle().Foreground(accent),
		EventKind:   lg.NewStyle().Foreground(highlight).Bold(true),
		EventTarget: lg.NewStyle().Foreground(lg.Color("252")),
		EventCasc:   lg.NewStyle().Foreground(warn).Italic(true),

		Reasoning: lg.NewStyle().Foreground(lg.Color("250")),
		HelpKey:   lg.NewStyle().Foreground(highlight).Bold(true),
		HelpDesc:  lg.NewStyle().Foreground(lg.Color("252")),

		Faint: lg.NewStyle().Foreground(dim),
	}
}

package tui

type KeyMap struct {
	Quit          []string
	TogglePause   []string
	Help          []string
	Save          []string
	FocusNext     []string
	FocusPrev     []string
	WorldNext     []string
	WorldPrev     []string
	SpeedUp       []string
	SpeedDown     []string
	MemoryToggle  []string
	LineageToggle []string
	AnnalsToggle  []string
	Intervene     []string
	DebugCreate   []string
	DebugDestroy  []string
	DebugRelate   []string
	DebugAdvance  []string
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:          []string{"q", "ctrl+c", "esc"},
		TogglePause:   []string{"space", " "},
		Help:          []string{"?"},
		Save:          []string{"s"},
		FocusNext:     []string{"tab"},
		FocusPrev:     []string{"shift+tab"},
		WorldNext:     []string{"ctrl+tab", "ctrl+n"},
		WorldPrev:     []string{"ctrl+shift+tab", "ctrl+p"},
		SpeedUp:       []string{"+", "="},
		SpeedDown:     []string{"-", "_"},
		MemoryToggle:  []string{"m"},
		LineageToggle: []string{"l"},
		AnnalsToggle:  []string{"a"},
		Intervene:     []string{"i"},
		DebugCreate:   []string{"n"},
		DebugDestroy:  []string{"x"},
		DebugRelate:   []string{"r"},
		DebugAdvance:  []string{"t"},
	}
}

func Matches(key string, keys []string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

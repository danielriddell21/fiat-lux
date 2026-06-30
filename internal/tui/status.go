package tui

import (
	"fmt"
	"strings"
)

type StatusInfo struct {
	WorldName   string
	Tick        uint64
	EntityCount int
	AgentCount  int
	Paused      bool
	Width       int
}

func RenderStatus(info StatusInfo, s Styles) string {
	var parts []string
	add := func(key, val string) {
		parts = append(parts, fmt.Sprintf("%s %s",
			s.StatusKey.Render(key+":"),
			s.StatusValue.Render(val),
		))
	}
	parts = append(parts, s.StatusValue.Render("fiat-lux"))
	add("world", nonEmpty(info.WorldName, "?"))
	add("tick", fmt.Sprintf("%d", info.Tick))
	add("entities", fmt.Sprintf("%d", info.EntityCount))
	add("agents", fmt.Sprintf("%d", info.AgentCount))

	if info.Paused {
		parts = append(parts, s.Paused.Render("[PAUSED]"))
	}

	line := strings.Join(parts, "  ")
	return s.StatusBar.Render(line)
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

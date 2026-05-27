package tui

import (
	"fmt"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// RenderEventLog renders the last n events as one line each. If
// events is empty a styled placeholder is returned. n <= 0 means
// "render all".
func RenderEventLog(events []world.Event, n int, s Styles) string {
	if len(events) == 0 {
		return s.Faint.Render("(no events yet)")
	}
	start := 0
	if n > 0 && len(events) > n {
		start = len(events) - n
	}
	var b strings.Builder
	for i := start; i < len(events); i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(renderEvent(events[i], s))
	}
	return b.String()
}

func renderEvent(e world.Event, s Styles) string {
	tick := s.EventTick.Render(fmt.Sprintf("t%-5d", e.Tick))
	var agent string
	if e.Agent == world.AgentIntervener {
		agent = s.EventAgent.Render("[void]")
	} else {
		agent = s.EventAgent.Render(fmt.Sprintf("a%-4d", e.Agent))
	}
	kind := s.EventKind.Render(padRight(string(e.Kind), 9))
	target := s.EventTarget.Render(eventTarget(e))
	out := fmt.Sprintf("%s %s %s %s", tick, agent, kind, target)
	if e.Cascade {
		out += " " + s.EventCasc.Render("(cascade)")
	}
	return out
}

func eventTarget(e world.Event) string {
	switch e.Kind {
	case world.EventCreate:
		name := e.TypeLabel
		if e.Props != nil {
			if n, ok := e.Props["name"].(string); ok && n != "" {
				name = fmt.Sprintf("%s %q", e.TypeLabel, n)
			}
		}
		return fmt.Sprintf("%s #%d", name, e.EntityID)
	case world.EventModify:
		return fmt.Sprintf("#%d", e.EntityID)
	case world.EventDestroy:
		return fmt.Sprintf("#%d", e.EntityID)
	case world.EventRelate:
		return fmt.Sprintf("#%d -[%s]-> #%d", e.From, e.RelKind, e.To)
	case world.EventUnrelate:
		return fmt.Sprintf("rel #%d", e.RelID)
	case world.EventTickStart:
		return ""
	default:
		return ""
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

package openai

import (
	"encoding/json"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

func RenderPerception(p brain.Perception) string {
	return brain.RenderPerceptionJSON(p)
}

func parseJSONFallback(content string) (*brain.ToolCall, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || trimmed[0] != '{' {
		return nil, false
	}
	var fall struct {
		Tool      string          `json:"tool"`
		Name      string          `json:"name"`
		Args      json.RawMessage `json:"args"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(trimmed), &fall); err != nil {
		return nil, false
	}
	name := fall.Tool
	if name == "" {
		name = fall.Name
	}
	if name == "" {
		return nil, false
	}
	args := fall.Args
	if len(args) == 0 {
		args = fall.Arguments
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return &brain.ToolCall{Name: name, Args: args}, true
}

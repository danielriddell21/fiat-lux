package brain

import (
	"context"
	"encoding/json"
)

type Brain interface {
	Decide(ctx context.Context, p Perception, tools []ToolDef) (Decision, error)

	Model() string

	Provider() string

	Close() error
}

type Decision struct {
	Thought string

	ToolCall *ToolCall

	Usage TokenUsage
}

type ToolCall struct {
	Name string
	Args json.RawMessage
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

type TokenUsage struct {
	Input  int64
	Output int64
	Cached int64
}

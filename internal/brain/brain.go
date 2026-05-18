package brain

import (
	"context"
	"encoding/json"
)

// Brain is the LLM-shaped decision-maker behind an agent. Each
// implementation - stub, anthropic, openai, ollama, openaicompat -
// translates between the simulation's tool vocabulary and the
// provider's wire format. The interface is intentionally narrow so
// the simulation depends only on its shape.
type Brain interface {
	// Decide consults the underlying provider given the agent's
	// current Perception and the set of available tools. The returned
	// Decision may carry a tool call (the agent's chosen action) and
	// an inner-monologue thought.
	Decide(ctx context.Context, p Perception, tools []ToolDef) (Decision, error)

	// Model returns the model identifier the brain uses, for
	// telemetry and logs.
	Model() string

	// Provider returns a stable human-readable label for telemetry
	// and TUI display: "anthropic", "openai", "ollama",
	// "openaicompat", or "stub".
	Provider() string

	// Close releases any provider resources (HTTP clients, sockets,
	// caches). Safe to call on a nil receiver.
	Close() error
}

// Decision is what a Brain returns for a single tick.
type Decision struct {
	// Thought is the inner monologue. It is recorded in the agent's
	// memory stream but never affects the world directly.
	Thought string

	// ToolCall is the chosen action. nil means "skip this tick".
	ToolCall *ToolCall

	// Usage reports tokens consumed; surfaced for telemetry.
	Usage TokenUsage
}

// ToolCall identifies a tool by name and carries its JSON-encoded
// arguments verbatim. The tools package parses and applies the call.
type ToolCall struct {
	Name string
	Args json.RawMessage
}

// ToolDef describes one tool available to a brain. The Schema is a
// JSON Schema object (Anthropic tool-use shape); each provider
// adapter translates it to its own wire format.
type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

// TokenUsage is the per-call token accounting.
// Cached counts tokens served from a prompt-cache hit (Anthropic).
type TokenUsage struct {
	Input  int64
	Output int64
	Cached int64
}

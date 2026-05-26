// Package imagegen attaches images to newly-created entities. A
// Generator turns a prompt into bytes; a Cache deduplicates calls by
// prompt hash so a deterministic-stub world re-renders without
// burning API quota. fiat-lux deliberately keeps images optional and
// off by default - cost discipline matters here.
package imagegen

import (
	"context"
	"errors"
)

// ErrNoGenerator is returned when an image is requested but no
// Generator has been configured.
var ErrNoGenerator = errors.New("imagegen: no generator configured")

// Generator produces image bytes from a text prompt. Implementations
// must be safe for concurrent use - the sim invokes Generate from a
// goroutine per entity creation.
type Generator interface {
	// Generate returns the image bytes and MIME type, or an error.
	// The returned slice is owned by the caller.
	Generate(ctx context.Context, prompt string) (data []byte, mime string, err error)

	// Model returns the model identifier for logs.
	Model() string

	// Provider returns a stable label (e.g. "openai").
	Provider() string
}

// PromptBuilder turns an entity's type_label and properties into a
// prompt string. Replace via Options.PromptBuilder for a custom voice.
type PromptBuilder func(typeLabel string, props map[string]any) string

// DefaultPromptBuilder produces a short, image-suitable description.
func DefaultPromptBuilder(typeLabel string, props map[string]any) string {
	if len(props) == 0 {
		return typeLabel
	}
	out := typeLabel
	if name, ok := props["name"].(string); ok && name != "" {
		out = name + ", a " + typeLabel
	}
	if desc, ok := props["description"].(string); ok && desc != "" {
		out += "; " + desc
	}
	if colour, ok := props["colour"].(string); ok && colour != "" {
		out += "; colour: " + colour
	}
	if mood, ok := props["mood"].(string); ok && mood != "" {
		out += "; mood: " + mood
	}
	return out
}

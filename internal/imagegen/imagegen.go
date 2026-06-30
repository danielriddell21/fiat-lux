package imagegen

import (
	"context"
	"errors"
)

var ErrNoGenerator = errors.New("imagegen: no generator configured")

type Generator interface {
	Generate(ctx context.Context, prompt string) (data []byte, mime string, err error)

	Model() string

	Provider() string
}

type PromptBuilder func(typeLabel string, props map[string]any) string

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

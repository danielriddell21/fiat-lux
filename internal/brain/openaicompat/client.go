// Package openaicompat implements brain.Brain against any
// OpenAI-compatible HTTP endpoint: LM Studio, llama.cpp server,
// vLLM, OpenRouter, or any other gateway speaking the OpenAI Chat
// Completions protocol. Base URL, optional API key, and model are
// user-supplied.
package openaicompat

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/brain/openai"
)

// Options configures an OpenAI-compatible Brain. BaseURL and Model
// are required; APIKey is optional (some gateways enforce auth,
// others don't).
type Options struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

// New constructs a Brain against the given endpoint. Returns the
// underlying *openai.Brain so it satisfies brain.Brain directly.
func New(opts Options) (*openai.Brain, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("openaicompat: BaseURL is required")
	}
	if opts.Model == "" {
		return nil, errors.New("openaicompat: Model is required")
	}
	b, err := openai.New(openai.Options{
		BaseURL:      opts.BaseURL,
		APIKey:       opts.APIKey,
		Model:        opts.Model,
		SystemPrompt: opts.SystemPrompt,
		HTTPClient:   opts.HTTPClient,
		Provider:     "openaicompat",
	})
	if err != nil {
		return nil, fmt.Errorf("openaicompat: %w", err)
	}
	return b, nil
}

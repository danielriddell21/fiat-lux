package ollama

import (
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/brain/openai"
)

const DefaultBaseURL = "http://localhost:11434/v1"

const DefaultModel = "qwen3:8b"

type Options struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

func New(opts Options) (*openai.Brain, error) {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := opts.Model
	if model == "" {
		model = DefaultModel
	}
	b, err := openai.New(openai.Options{
		BaseURL:      baseURL,
		APIKey:       "", // Ollama ignores the auth header
		Model:        model,
		SystemPrompt: opts.SystemPrompt,
		HTTPClient:   opts.HTTPClient,
		Provider:     "ollama",
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	return b, nil
}

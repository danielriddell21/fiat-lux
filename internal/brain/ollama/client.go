package ollama

import (
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/brain/openai"
)

// DefaultBaseURL is the conventional Ollama OpenAI-compatible
// endpoint. Ollama exposes `/v1/chat/completions` alongside its
// native `/api/chat`; we use the OpenAI-shaped one so the same
// client supports both native tool calling and JSON-mode fallback.
const DefaultBaseURL = "http://localhost:11434/v1"

// DefaultModel is the default local model.
const DefaultModel = "qwen3:8b"

// Options configures an Ollama Brain. Defaults match a vanilla
// `ollama serve` on localhost.
type Options struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

// New constructs a Brain talking to Ollama's OpenAI-compatible
// chat completions endpoint. The returned brain is *openai.Brain so
// callers can use it interchangeably.
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

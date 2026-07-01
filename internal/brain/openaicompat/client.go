package openaicompat

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/brain/openai"
)

type Options struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

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

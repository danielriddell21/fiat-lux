// Package openaicompat implements imagegen.Generator against any
// OpenAI-compatible /v1/images/generations endpoint: LM Studio,
// llama.cpp's server, vLLM, openedai-images, or any other gateway
// that speaks the OpenAI Images protocol. Base URL and model are
// user-supplied; API key is optional (most local gateways do not
// enforce auth).
package openaicompat

import (
	"errors"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/imagegen/openai"
)

// Options configures an OpenAI-compatible image Generator. BaseURL
// and Model are required; APIKey is optional. Size, when blank,
// defers to the openai package default.
type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Size       string
	HTTPClient *http.Client
}

// New constructs a Client against the given endpoint. Returns the
// underlying *openai.Client so it satisfies imagegen.Generator
// directly.
func New(opts Options) (*openai.Client, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("openaicompat imagegen: BaseURL is required")
	}
	if opts.Model == "" {
		return nil, errors.New("openaicompat imagegen: Model is required")
	}
	return openai.New(openai.Options{
		BaseURL:    opts.BaseURL,
		APIKey:     opts.APIKey,
		Model:      opts.Model,
		Size:       opts.Size,
		HTTPClient: opts.HTTPClient,
	})
}

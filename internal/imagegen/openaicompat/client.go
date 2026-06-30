package openaicompat

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/imagegen/openai"
)

type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Size       string
	HTTPClient *http.Client
}

func New(opts Options) (*openai.Client, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("openaicompat imagegen: BaseURL is required")
	}
	if opts.Model == "" {
		return nil, errors.New("openaicompat imagegen: Model is required")
	}
	c, err := openai.New(openai.Options{
		BaseURL:    opts.BaseURL,
		APIKey:     opts.APIKey,
		Model:      opts.Model,
		Size:       opts.Size,
		HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("openaicompat imagegen: %w", err)
	}
	return c, nil
}

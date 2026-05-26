// Package openai implements imagegen.Generator against OpenAI's
// /v1/images/generations endpoint. The default model is gpt-image-1;
// dall-e-3 also works. The endpoint requires OPENAI_API_KEY in the
// environment.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/imagegen"
)

// DefaultBaseURL is OpenAI's API root.
const DefaultBaseURL = "https://api.openai.com/v1"

// DefaultModel is gpt-image-1, the current production image model.
const DefaultModel = "gpt-image-1"

// Options configures a Client.
type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Size       string
	HTTPClient *http.Client
}

// Client is the OpenAI Images generator.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	size       string
	httpClient *http.Client
}

// New constructs a Client. APIKey is required when BaseURL is empty
// (i.e. when targeting api.openai.com); openai-compatible endpoints
// may leave it blank.
func New(opts Options) (*Client, error) {
	if opts.APIKey == "" && opts.BaseURL == "" {
		return nil, errors.New("openai imagegen: APIKey is required for the OpenAI endpoint")
	}
	c := &Client{
		baseURL: opts.BaseURL,
		apiKey:  opts.APIKey,
		model:   opts.Model,
		size:    opts.Size,
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.model == "" {
		c.model = DefaultModel
	}
	if c.size == "" {
		c.size = "512x512"
	}
	c.httpClient = opts.HTTPClient
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return c, nil
}

// Compile-time check that Client implements the Generator interface.
var _ imagegen.Generator = (*Client)(nil)

type request struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size"`
}

type response struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Generate posts to /images/generations and decodes the first base64
// image. Returns ("image/png").
func (c *Client) Generate(ctx context.Context, prompt string) ([]byte, string, error) {
	body, err := json.Marshal(request{Model: c.model, Prompt: prompt, N: 1, Size: c.size})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode/100 != 2 {
		var r response
		_ = json.Unmarshal(raw, &r)
		if r.Error != nil {
			return nil, "", fmt.Errorf("openai imagegen: %s", r.Error.Message)
		}
		return nil, "", fmt.Errorf("openai imagegen: status %d: %s", resp.StatusCode, raw)
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, "", fmt.Errorf("openai imagegen: decode: %w", err)
	}
	if len(r.Data) == 0 || r.Data[0].B64JSON == "" {
		return nil, "", errors.New("openai imagegen: empty data")
	}
	data, err := base64.StdEncoding.DecodeString(r.Data[0].B64JSON)
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: base64: %w", err)
	}
	return data, "image/png", nil
}

// Model returns the configured model name.
func (c *Client) Model() string { return c.model }

// Provider returns the stable "openai" label.
func (c *Client) Provider() string { return "openai" }

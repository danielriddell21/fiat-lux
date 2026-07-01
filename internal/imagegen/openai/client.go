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

const DefaultBaseURL = "https://api.openai.com/v1"

const DefaultModel = "gpt-image-1"

type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Size       string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	size       string
	httpClient *http.Client
}

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

func (c *Client) Generate(ctx context.Context, prompt string) ([]byte, string, error) {
	body, err := json.Marshal(request{Model: c.model, Prompt: prompt, N: 1, Size: c.size})
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("openai imagegen: read response: %w", err)
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

func (c *Client) Model() string { return c.model }

func (c *Client) Provider() string { return "openai" }

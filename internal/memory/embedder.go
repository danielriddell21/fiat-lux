package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Close() error
}

type ZeroEmbedder struct{}

func (ZeroEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	return []float64{0}, nil
}

func (ZeroEmbedder) Close() error { return nil }

type HashEmbedder struct {
	Dim int
}

func (h HashEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	dim := h.Dim
	if dim <= 0 {
		dim = 32
	}
	v := make([]float64, dim)
	hasher := fnv.New64a()
	for i := range dim {
		hasher.Reset()
		_, _ = io.WriteString(hasher, text)
		_, _ = fmt.Fprintf(hasher, "|%d", i)
		raw := hasher.Sum64()
		// Map to [-1, 1].
		v[i] = float64(int64(raw))/(1<<31) - 1
	}
	// Normalise.
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	if norm == 0 {
		return v, nil
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	return v, nil
}

func (HashEmbedder) Close() error { return nil }

type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

type OllamaOptions struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func NewOllama(opts OllamaOptions) *OllamaEmbedder {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	model := opts.Model
	if model == "" {
		model = "nomic-embed-text"
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &OllamaEmbedder{baseURL: base, model: model, client: hc}
}

type ollamaReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}
type ollamaResp struct {
	Embedding []float64 `json:"embedding"`
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(ollamaReq{Model: e.model, Prompt: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out ollamaResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama embed: decode: %w", err)
	}
	if len(out.Embedding) == 0 {
		return nil, errors.New("ollama embed: empty embedding")
	}
	return out.Embedding, nil
}

func (e *OllamaEmbedder) Close() error { return nil }

type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

type OpenAIOptions struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func NewOpenAI(opts OpenAIOptions) (*OpenAIEmbedder, error) {
	if opts.APIKey == "" {
		return nil, errors.New("openai embedder: APIKey is required")
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := opts.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAIEmbedder{baseURL: base, apiKey: opts.APIKey, model: model, client: hc}, nil
}

type openAIReq struct {
	Model string `json:"model"`
	Input string `json:"input"`
}
type openAIResp struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(openAIReq{Model: e.model, Input: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed: build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("openai embed: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out openAIResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai embed: decode: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, errors.New("openai embed: empty embedding")
	}
	return out.Data[0].Embedding, nil
}

func (e *OpenAIEmbedder) Close() error { return nil }

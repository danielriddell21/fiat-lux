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

// Embedder turns text into a fixed-dimension embedding vector.
// Implementations may call out to a network service or compute
// locally. The pipeline tolerates a nil embedder - relevance simply
// contributes zero to the combined retrieval score.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Close() error
}

// ZeroEmbedder returns a zero-norm vector for every text. Cosine
// similarity against any other vector is zero, so relevance
// contributes nothing - retrieval falls back to recency *
// importance. This is the safe default when no embedder is
// configured.
type ZeroEmbedder struct{}

// Embed returns a fixed-size zero vector.
func (ZeroEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	return []float64{0}, nil
}

// Close is a no-op.
func (ZeroEmbedder) Close() error { return nil }

// HashEmbedder is a deterministic, dependency-free embedder useful
// in tests: it produces a normalised vector seeded by an FNV hash of
// the input. Identical inputs produce identical vectors; different
// inputs produce uncorrelated vectors. Not appropriate for
// production semantic retrieval - it has no notion of meaning -
// but excellent for verifying the retrieval pipeline.
type HashEmbedder struct {
	Dim int
}

// Embed implements Embedder using a fast, deterministic hash.
func (h HashEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	dim := h.Dim
	if dim <= 0 {
		dim = 32
	}
	v := make([]float64, dim)
	hasher := fnv.New64a()
	for i := 0; i < dim; i++ {
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

// Close is a no-op.
func (HashEmbedder) Close() error { return nil }

// OllamaEmbedder calls Ollama's /api/embeddings endpoint with the
// configured model. Default model is nomic-embed-text.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// OllamaOptions configures an OllamaEmbedder.
type OllamaOptions struct {
	BaseURL    string // default http://localhost:11434
	Model      string // default nomic-embed-text
	HTTPClient *http.Client
}

// NewOllama constructs an OllamaEmbedder.
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

// Embed calls the Ollama embeddings endpoint.
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

// Close is a no-op for the default HTTP client.
func (e *OllamaEmbedder) Close() error { return nil }

// OpenAIEmbedder calls OpenAI's /v1/embeddings endpoint. Used for
// cloud users who don't want a local embedder.
type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// OpenAIOptions configures an OpenAIEmbedder.
type OpenAIOptions struct {
	BaseURL    string // default https://api.openai.com/v1
	APIKey     string // required
	Model      string // default text-embedding-3-small
	HTTPClient *http.Client
}

// NewOpenAI constructs an OpenAIEmbedder.
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

// Embed calls the OpenAI embeddings endpoint.
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

// Close is a no-op for the default HTTP client.
func (e *OpenAIEmbedder) Close() error { return nil }

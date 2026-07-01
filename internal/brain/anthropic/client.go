package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

const DefaultBaseURL = "https://api.anthropic.com"

const DefaultModel = "claude-haiku-4-5"

const DefaultAPIVersion = "2023-06-01"

const DefaultMaxTokens = 1024

type Options struct {
	BaseURL string

	APIKey string

	Model string

	APIVersion string

	SystemPrompt string

	PromptCaching *bool

	MaxTokens int

	HTTPClient *http.Client
}

type Brain struct {
	baseURL       string
	apiKey        string
	apiVersion    string
	model         string
	systemPrompt  string
	maxTokens     int
	promptCaching bool
	httpClient    *http.Client
}

func New(opts Options) (*Brain, error) {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := opts.Model
	if model == "" {
		model = DefaultModel
	}
	apiVersion := opts.APIVersion
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	caching := true
	if opts.PromptCaching != nil {
		caching = *opts.PromptCaching
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Brain{
		baseURL:       baseURL,
		apiKey:        opts.APIKey,
		apiVersion:    apiVersion,
		model:         model,
		systemPrompt:  opts.SystemPrompt,
		maxTokens:     maxTokens,
		promptCaching: caching,
		httpClient:    hc,
	}, nil
}

func (b *Brain) Model() string { return b.model }

func (b *Brain) Provider() string { return "anthropic" }

func (b *Brain) Close() error { return nil }

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type messagesRequest struct {
	Model     string           `json:"model"`
	System    []systemBlock    `json:"system,omitempty"`
	Tools     []anthroTool     `json:"tools,omitempty"`
	Messages  []requestMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens"`
}

type requestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthroTool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"input_schema"`
	CacheControl *cacheControl  `json:"cache_control,omitempty"`
}

type messagesResponse struct {
	Content []responseBlock `json:"content"`
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Usage   responseUsage   `json:"usage"`
}

type responseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type responseUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

func (b *Brain) Decide(ctx context.Context, p brain.Perception, tools []brain.ToolDef) (brain.Decision, error) {
	req := messagesRequest{
		Model:     b.model,
		System:    b.buildSystem(),
		Tools:     b.buildTools(tools),
		Messages:  []requestMessage{{Role: "user", Content: brain.RenderPerceptionJSON(p)}},
		MaxTokens: b.maxTokens,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return brain.Decision{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return brain.Decision{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", b.apiVersion)
	if b.apiKey != "" {
		httpReq.Header.Set("x-api-key", b.apiKey)
	}

	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return brain.Decision{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return brain.Decision{}, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, brain.Redact(string(buf)))
	}

	var out messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return brain.Decision{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	return parseDecision(out)
}

func (b *Brain) buildSystem() []systemBlock {
	if b.systemPrompt == "" {
		return nil
	}
	block := systemBlock{Type: "text", Text: b.systemPrompt}
	if b.promptCaching {
		block.CacheControl = &cacheControl{Type: "ephemeral"}
	}
	return []systemBlock{block}
}

func (b *Brain) buildTools(defs []brain.ToolDef) []anthroTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]anthroTool, len(defs))
	for i, d := range defs {
		out[i] = anthroTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.Schema,
		}
	}
	if b.promptCaching {
		out[len(out)-1].CacheControl = &cacheControl{Type: "ephemeral"}
	}
	return out
}

func parseDecision(r messagesResponse) (brain.Decision, error) {
	if len(r.Content) == 0 {
		return brain.Decision{}, errors.New("anthropic: response had no content blocks")
	}

	d := brain.Decision{
		Usage: brain.TokenUsage{
			Input:  r.Usage.InputTokens,
			Output: r.Usage.OutputTokens,
			// Anthropic reports cache reads separately; forward them.
			Cached: r.Usage.CacheReadInputTokens,
		},
	}
	// Cache creations are counted with normal Input.
	d.Usage.Input += r.Usage.CacheCreationInputTokens

	var thoughtParts []string
	for _, blk := range r.Content {
		switch blk.Type {
		case "text":
			if blk.Text != "" {
				thoughtParts = append(thoughtParts, blk.Text)
			}
		case "tool_use":
			args := blk.Input
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			d.ToolCall = &brain.ToolCall{Name: blk.Name, Args: args}
		}
	}
	d.Thought = strings.Join(thoughtParts, "\n")
	return d, nil
}

var _ brain.Brain = (*Brain)(nil)

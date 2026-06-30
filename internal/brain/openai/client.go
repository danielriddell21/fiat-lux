package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

const DefaultBaseURL = "https://api.openai.com/v1"

const DefaultModel = "gpt-5.4-nano"

type Options struct {
	BaseURL string

	APIKey string

	Model string

	SystemPrompt string

	HTTPClient *http.Client

	Provider string
}

type Brain struct {
	baseURL      string
	apiKey       string
	model        string
	systemPrompt string
	httpClient   *http.Client
	provider     string
}

func New(opts Options) (*Brain, error) {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("openai: invalid base URL %q: %w", baseURL, err)
	}
	model := opts.Model
	if model == "" {
		model = DefaultModel
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	provider := opts.Provider
	if provider == "" {
		provider = "openai"
	}
	return &Brain{
		baseURL:      baseURL,
		apiKey:       opts.APIKey,
		model:        model,
		systemPrompt: opts.SystemPrompt,
		httpClient:   hc,
		provider:     provider,
	}, nil
}

func (b *Brain) Model() string { return b.model }

func (b *Brain) Provider() string { return b.provider }

func (b *Brain) Close() error { return nil }

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type chatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
	Model   string       `json:"model"`
}

type chatChoice struct {
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func (b *Brain) Decide(ctx context.Context, p brain.Perception, tools []brain.ToolDef) (brain.Decision, error) {
	req := chatRequest{
		Model: b.model,
		Messages: []chatMessage{
			{Role: "system", Content: b.systemPrompt},
			{Role: "user", Content: RenderPerception(p)},
		},
		Tools:       buildTools(tools),
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return brain.Decision{}, fmt.Errorf("%s: marshal request: %w", b.provider, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return brain.Decision{}, fmt.Errorf("%s: build request: %w", b.provider, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		// net/http transport errors do not include the Authorization
		// header in their text, so wrapping err is safe. Upstream
		// 4xx/5xx bodies are scrubbed by redact() below.
		return brain.Decision{}, fmt.Errorf("%s: request failed: %w", b.provider, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return brain.Decision{}, fmt.Errorf("%s: HTTP %d: %s", b.provider, resp.StatusCode, redact(string(buf)))
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return brain.Decision{}, fmt.Errorf("%s: decode response: %w", b.provider, err)
	}

	return parseDecision(out)
}

func buildTools(defs []brain.ToolDef) []chatTool {
	out := make([]chatTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, chatTool{
			Type: "function",
			Function: chatToolFunc{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Schema,
			},
		})
	}
	return out
}

func parseDecision(r chatResponse) (brain.Decision, error) {
	if len(r.Choices) == 0 {
		return brain.Decision{}, errors.New("openai: response had no choices")
	}
	msg := r.Choices[0].Message

	usage := brain.TokenUsage{
		Input:  r.Usage.PromptTokens,
		Output: r.Usage.CompletionTokens,
	}
	if r.Usage.PromptTokensDetails != nil {
		usage.Cached = r.Usage.PromptTokensDetails.CachedTokens
		// Reduce Input by the cached portion so the cost calculation
		// (which prices cached separately) doesn't double-count.
		if usage.Cached > 0 && usage.Input >= usage.Cached {
			usage.Input -= usage.Cached
		}
	}

	d := brain.Decision{
		Thought: extractThought(msg.Content),
		Usage:   usage,
	}
	if len(msg.ToolCalls) > 0 {
		call := msg.ToolCalls[0]
		var args json.RawMessage
		if call.Function.Arguments != "" {
			args = json.RawMessage(call.Function.Arguments)
		} else {
			args = json.RawMessage(`{}`)
		}
		d.ToolCall = &brain.ToolCall{Name: call.Function.Name, Args: args}
	} else {
		// JSON-mode fallback: the model returned plain text. If it
		// looks like a JSON object naming a tool, use it. Otherwise
		// treat as a skip.
		if tc, ok := parseJSONFallback(msg.Content); ok {
			d.ToolCall = tc
		}
	}
	return d, nil
}

func extractThought(content string) string {
	if !strings.Contains(content, "<think>") {
		return strings.TrimSpace(content)
	}
	var parts []string
	push := func(s string) {
		if t := strings.TrimSpace(s); t != "" {
			parts = append(parts, t)
		}
	}
	s := content
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			push(s)
			break
		}
		push(s[:i])
		s = s[i+len("<think>"):]
		j := strings.Index(s, "</think>")
		if j < 0 {
			push(s)
			break
		}
		push(s[:j])
		s = s[j+len("</think>"):]
	}
	return strings.Join(parts, "\n\n")
}

func redact(s string) string {
	// "sk-" covers both OpenAI keys and the common "sk-ant-" prefix
	// for Anthropic; matching one substring captures both.
	const prefix = "sk-"
	var out strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			out.WriteString(s)
			break
		}
		end := i + len(prefix)
		for end < len(s) && (isAlnum(s[end]) || s[end] == '-' || s[end] == '_') {
			end++
		}
		out.WriteString(s[:i])
		out.WriteString("[redacted]")
		s = s[end:]
	}
	return out.String()
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

var _ brain.Brain = (*Brain)(nil)

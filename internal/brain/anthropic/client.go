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

// DefaultBaseURL is Anthropic's Messages API endpoint host.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultModel is the default cloud model.
const DefaultModel = "claude-haiku-4-5"

// DefaultAPIVersion is the anthropic-version header value.
const DefaultAPIVersion = "2023-06-01"

// DefaultMaxTokens is the max_tokens cap. Tool-use responses are
// usually short - one tool call plus a brief justification - so
// 1024 is generous without inviting runaway billing.
const DefaultMaxTokens = 1024

// Options configures a Brain.
type Options struct {
	// BaseURL defaults to the production Anthropic endpoint.
	BaseURL string

	// APIKey is sent as the x-api-key header. Required.
	APIKey string

	// Model is the model identifier; defaults to DefaultModel.
	Model string

	// APIVersion is sent as anthropic-version. Defaults to
	// DefaultAPIVersion.
	APIVersion string

	// SystemPrompt is the static instruction the brain prepends as
	// the API's top-level `system` field.
	SystemPrompt string

	// PromptCaching toggles cache_control attachment on the system
	// prompt and tool catalogue. Enabled by default; pass &boolFalse
	// to disable.
	PromptCaching *bool

	// MaxTokens caps generated tokens. Defaults to DefaultMaxTokens.
	MaxTokens int

	// HTTPClient lets tests inject a transport. Defaults to a
	// 60-second-timeout client.
	HTTPClient *http.Client
}

// Brain is the Anthropic Messages implementation.
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

// New constructs a Brain. APIKey is required for production use;
// tests may leave it empty when using a recorded fixture.
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

// Model returns the model identifier the brain uses.
func (b *Brain) Model() string { return b.model }

// Provider returns "anthropic" for telemetry labels.
func (b *Brain) Provider() string { return "anthropic" }

// Close releases any provider resources. No-op for the default
// client.
func (b *Brain) Close() error { return nil }

// systemBlock is one element of the system field when it is sent as
// an array (the form that allows cache_control attachment).
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

// messagesResponse is the (subset of) Anthropic Messages response we
// consume.
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

// Decide consults the Messages API.
func (b *Brain) Decide(ctx context.Context, p brain.Perception, tools []brain.ToolDef) (brain.Decision, error) {
	req := messagesRequest{
		Model:     b.model,
		System:    b.buildSystem(),
		Tools:     b.buildTools(tools),
		Messages:  []requestMessage{{Role: "user", Content: renderPerceptionJSON(p)}},
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
		return brain.Decision{}, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, redact(string(buf)))
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

// buildTools translates brain.ToolDef into Anthropic's wire form,
// attaching cache_control to the final tool when prompt caching is
// enabled. Anthropic propagates a single cache_control across the
// full tools block, so marking the last entry is sufficient.
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

// renderPerceptionJSON encodes the agent's Perception as a JSON
// string. Inlined here (rather than imported from internal/brain/openai)
// to keep the anthropic package self-contained; both implementations
// happen to want the same compact JSON representation.
func renderPerceptionJSON(p brain.Perception) string {
	type rendered struct {
		Tick          uint64             `json:"tick"`
		EntityCount   int                `json:"entity_count"`
		Drives        map[string]float64 `json:"drives,omitempty"`
		Entities      []entityV          `json:"entities,omitempty"`
		Relationships []relV             `json:"relationships,omitempty"`
		RecentEvents  []eventV           `json:"recent_events,omitempty"`
		Memories      []memoryV          `json:"memories,omitempty"`
		Focus         *focusV            `json:"focus,omitempty"`
		Frontier      *frontierV         `json:"frontier,omitempty"`
		Suggestions   []suggestionV      `json:"spawn_suggestions,omitempty"`
	}
	r := rendered{Tick: p.Tick, EntityCount: p.EntityCount, Drives: p.Drives}
	for _, e := range p.AliveEntities {
		r.Entities = append(r.Entities, entityV{ID: e.ID, Type: e.TypeLabel, Props: e.Properties})
	}
	for _, rel := range p.AliveRelationships {
		r.Relationships = append(r.Relationships, relV{ID: rel.ID, From: rel.From, To: rel.To, Kind: rel.Kind})
	}
	for _, ev := range p.RecentEvents {
		s := ev.Summary
		if s == "" {
			s = ev.Kind
		}
		r.RecentEvents = append(r.RecentEvents, eventV{Tick: ev.Tick, Summary: s})
	}
	for _, m := range p.Memories {
		r.Memories = append(r.Memories, memoryV{Tick: m.Tick, Content: m.Content})
	}
	r.Focus = renderFocus(p.Focus)
	r.Frontier = renderFrontier(p.Frontier)
	for _, s := range p.Suggestions {
		r.Suggestions = append(r.Suggestions, suggestionV{
			EntityID: s.EntityID, Type: s.TypeLabel,
			ChildCount: s.ChildCount, Reason: s.Reason,
		})
	}
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("(perception render failed: %v)", err)
	}
	return string(out)
}

func renderFocus(f *brain.FocusView) *focusV {
	if f == nil {
		return nil
	}
	out := &focusV{
		EntityID:   f.EntityID,
		Type:       f.TypeLabel,
		TurnsLeft:  f.TurnsLeft,
		DepthBelow: f.DepthBelow,
	}
	for _, e := range f.Subtree {
		out.Subtree = append(out.Subtree, entityV{ID: e.ID, Type: e.TypeLabel, Props: e.Properties})
	}
	for _, r := range f.SubRels {
		out.SubRels = append(out.SubRels, relV{ID: r.ID, From: r.From, To: r.To, Kind: r.Kind})
	}
	return out
}

func renderFrontier(f brain.FrontierView) *frontierV {
	if len(f.Leaves) == 0 && len(f.DeepestPath) == 0 {
		return nil
	}
	out := &frontierV{}
	for _, l := range f.Leaves {
		out.Leaves = append(out.Leaves, leafV{ID: l.EntityID, Type: l.TypeLabel, Depth: l.Depth})
	}
	for _, s := range f.DeepestPath {
		out.DeepestPath = append(out.DeepestPath, stepV{ID: s.EntityID, Type: s.TypeLabel})
	}
	return out
}

type entityV struct {
	ID    uint64         `json:"id"`
	Type  string         `json:"type"`
	Props map[string]any `json:"properties,omitempty"`
}
type relV struct {
	ID   uint64 `json:"id"`
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
	Kind string `json:"kind"`
}
type eventV struct {
	Tick    uint64 `json:"tick"`
	Summary string `json:"event"`
}
type memoryV struct {
	Tick    uint64 `json:"tick"`
	Content string `json:"content"`
}
type focusV struct {
	EntityID   uint64    `json:"entity_id"`
	Type       string    `json:"type"`
	TurnsLeft  int       `json:"turns_left"`
	DepthBelow int       `json:"depth_below"`
	Subtree    []entityV `json:"subtree,omitempty"`
	SubRels    []relV    `json:"subtree_relationships,omitempty"`
}
type frontierV struct {
	Leaves      []leafV `json:"leaves,omitempty"`
	DeepestPath []stepV `json:"deepest_path,omitempty"`
}
type leafV struct {
	ID    uint64 `json:"id"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
}
type stepV struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`
}
type suggestionV struct {
	EntityID   uint64 `json:"entity_id"`
	Type       string `json:"type"`
	ChildCount int    `json:"child_count"`
	Reason     string `json:"reason"`
}

func redact(s string) string {
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

// Compile-time assertion.
var _ brain.Brain = (*Brain)(nil)

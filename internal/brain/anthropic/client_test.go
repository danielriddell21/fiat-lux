package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

type fakeRT struct {
	respBody string
	status   int
	lastReq  *http.Request
	lastBody []byte
	lastHdr  http.Header
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	if req.Body != nil {
		f.lastBody, _ = io.ReadAll(req.Body)
	}
	f.lastHdr = req.Header.Clone()
	status := f.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader([]byte(f.respBody))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func newBrain(t *testing.T, rt http.RoundTripper, opts Options) *Brain {
	t.Helper()
	opts.HTTPClient = &http.Client{Transport: rt}
	b, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecide_ParsesToolUseBlock(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"model": "claude-haiku-4-5",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "I'll make a planet."},
			{"type": "tool_use", "id": "tu_1", "name": "Create", "input": {"type_label": "planet"}}
		],
		"usage": {"input_tokens": 200, "output_tokens": 30, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	}`}
	b := newBrain(t, rt, Options{
		APIKey:       "sk-ant-fake",
		Model:        "claude-haiku-4-5",
		SystemPrompt: "you are a creator",
	})

	d, err := b.Decide(context.Background(), brain.Perception{Tick: 1}, []brain.ToolDef{
		{Name: "Create", Description: "create", Schema: map[string]any{"type": "object"}},
		{Name: "Wait", Description: "wait", Schema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.ToolCall == nil || d.ToolCall.Name != "Create" {
		t.Errorf("ToolCall = %+v, want Create", d.ToolCall)
	}
	if d.Thought != "I'll make a planet." {
		t.Errorf("Thought = %q", d.Thought)
	}
	if d.Usage.Input != 200 || d.Usage.Output != 30 {
		t.Errorf("Usage = %+v", d.Usage)
	}

	if got := rt.lastReq.URL.Path; got != "/v1/messages" {
		t.Errorf("path = %q", got)
	}
	if got, want := rt.lastHdr.Get("x-api-key"), "sk-ant-fake"; got != want {
		t.Errorf("x-api-key = %q", got)
	}
	if got := rt.lastHdr.Get("anthropic-version"); got != DefaultAPIVersion {
		t.Errorf("anthropic-version = %q", got)
	}

	var sent messagesRequest
	if err := json.Unmarshal(rt.lastBody, &sent); err != nil {
		t.Fatalf("parse sent: %v", err)
	}
	if len(sent.System) != 1 || sent.System[0].Text != "you are a creator" {
		t.Errorf("system block not sent: %+v", sent.System)
	}
	// Prompt caching is on by default - cache_control should be on
	// the system block AND on the last tool.
	if sent.System[0].CacheControl == nil {
		t.Errorf("expected cache_control on system block, got nil")
	}
	if len(sent.Tools) != 2 {
		t.Fatalf("tools sent = %d, want 2", len(sent.Tools))
	}
	if sent.Tools[len(sent.Tools)-1].CacheControl == nil {
		t.Errorf("expected cache_control on last tool, got nil")
	}
	if sent.Tools[0].CacheControl != nil {
		t.Errorf("did not expect cache_control on first tool")
	}
}

func TestDecide_CacheTokensReportedSeparately(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"role": "assistant",
		"content": [{"type":"tool_use","id":"x","name":"Wait","input":{"n":1}}],
		"usage": {"input_tokens": 50, "output_tokens": 5, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 800}
	}`}
	b := newBrain(t, rt, Options{APIKey: "sk-ant-x"})
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Input = reported input + cache_creation; Cached = cache_read.
	if d.Usage.Input != 150 {
		t.Errorf("Usage.Input = %d, want 150 (50 + 100)", d.Usage.Input)
	}
	if d.Usage.Cached != 800 {
		t.Errorf("Usage.Cached = %d, want 800", d.Usage.Cached)
	}
}

func TestPromptCaching_Disabled(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"role": "assistant",
		"content": [{"type":"tool_use","id":"x","name":"Wait","input":{}}],
		"usage": {"input_tokens": 10, "output_tokens": 2}
	}`}
	off := false
	b := newBrain(t, rt, Options{APIKey: "k", SystemPrompt: "sys", PromptCaching: &off})
	_, err := b.Decide(context.Background(), brain.Perception{}, []brain.ToolDef{{Name: "Wait", Schema: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	var sent messagesRequest
	_ = json.Unmarshal(rt.lastBody, &sent)
	if len(sent.System) == 1 && sent.System[0].CacheControl != nil {
		t.Errorf("cache_control set on system block despite PromptCaching=false")
	}
	if len(sent.Tools) == 1 && sent.Tools[0].CacheControl != nil {
		t.Errorf("cache_control set on tool despite PromptCaching=false")
	}
}

func TestDecide_TextOnlyMeansSkip(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"role": "assistant",
		"content": [{"type":"text","text":"I'll wait."}],
		"usage": {"input_tokens": 10, "output_tokens": 4}
	}`}
	b := newBrain(t, rt, Options{APIKey: "k"})
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.ToolCall != nil {
		t.Errorf("expected skip, got %+v", d.ToolCall)
	}
	if d.Thought != "I'll wait." {
		t.Errorf("Thought = %q", d.Thought)
	}
}

func TestDecide_HTTPError_RedactsKeys(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{
		status:   400,
		respBody: `{"error":"bad request from key sk-ant-secret789"}`,
	}
	b := newBrain(t, rt, Options{APIKey: "sk-ant-secret789"})
	_, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret789") {
		t.Errorf("error leaked key: %v", err)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	in := "key sk-ant-XYZ123 and sk-abc-def end"
	out := redact(in)
	if strings.Contains(out, "XYZ123") || strings.Contains(out, "abc-def") {
		t.Errorf("redact left fragments: %q", out)
	}
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	b, err := New(Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Model() != DefaultModel {
		t.Errorf("Model() = %q, want %q", b.Model(), DefaultModel)
	}
	if b.Provider() != "anthropic" {
		t.Errorf("Provider() = %q", b.Provider())
	}
}

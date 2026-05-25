package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

// fakeRT is a RoundTripper that synchronously serves a single
// canned response and captures the inbound request for assertions.
type fakeRT struct {
	respBody   string
	status     int
	lastReq    *http.Request
	lastBody   []byte
	lastHeader http.Header
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	if req.Body != nil {
		buf, _ := io.ReadAll(req.Body)
		f.lastBody = buf
	}
	f.lastHeader = req.Header.Clone()
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

// errRT returns the given error from every RoundTrip call.
type errRT struct{ err error }

func (e errRT) RoundTrip(_ *http.Request) (*http.Response, error) { return nil, e.err }

func newBrain(t *testing.T, rt http.RoundTripper, opts Options) *Brain {
	t.Helper()
	if opts.BaseURL == "" {
		opts.BaseURL = "https://example.invalid"
	}
	opts.HTTPClient = &http.Client{Transport: rt}
	b, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecide_BuildsRequestAndParsesToolCall(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{
		respBody: `{
			"model": "gpt-test",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "let there be light",
					"tool_calls": [{
						"id": "tc_1",
						"type": "function",
						"function": {"name": "Create", "arguments": "{\"type_label\":\"planet\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 42, "completion_tokens": 8}
		}`,
	}
	b := newBrain(t, rt, Options{
		APIKey:       "sk-test-12345",
		Model:        "gpt-test",
		SystemPrompt: "you are a creator",
	})

	d, err := b.Decide(context.Background(), brain.Perception{Tick: 1}, []brain.ToolDef{
		{Name: "Create", Description: "make a thing", Schema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if d.ToolCall == nil || d.ToolCall.Name != "Create" {
		t.Errorf("Decision ToolCall = %+v, want Create", d.ToolCall)
	}
	if d.Thought != "let there be light" {
		t.Errorf("Thought = %q", d.Thought)
	}
	if d.Usage.Input != 42 || d.Usage.Output != 8 {
		t.Errorf("Usage = %+v, want Input=42 Output=8", d.Usage)
	}

	if got, want := rt.lastReq.URL.Path, "/chat/completions"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := rt.lastHeader.Get("Authorization"), "Bearer sk-test-12345"; got != want {
		t.Errorf("auth header = %q, want %q", got, want)
	}

	var sent chatRequest
	if err := json.Unmarshal(rt.lastBody, &sent); err != nil {
		t.Fatalf("could not parse sent body: %v\n%s", err, rt.lastBody)
	}
	if sent.Model != "gpt-test" {
		t.Errorf("sent model = %q", sent.Model)
	}
	if len(sent.Tools) != 1 || sent.Tools[0].Function.Name != "Create" {
		t.Errorf("tools not forwarded: %+v", sent.Tools)
	}
}

func TestDecide_FallbackJSONMode(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"choices":[{"message":{"role":"assistant","content":"{\"tool\":\"Wait\",\"args\":{\"n\":2}}"}}],
		"usage":{"prompt_tokens":5,"completion_tokens":5}
	}`}
	b := newBrain(t, rt, Options{})

	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.ToolCall == nil || d.ToolCall.Name != "Wait" {
		t.Errorf("expected fallback Wait, got %+v", d.ToolCall)
	}
}

func TestDecide_NoToolCall_IsSkip(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"choices":[{"message":{"role":"assistant","content":"I'm thinking."}}],
		"usage":{"prompt_tokens":4,"completion_tokens":4}
	}`}
	b := newBrain(t, rt, Options{})
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.ToolCall != nil {
		t.Errorf("expected nil ToolCall (skip); got %+v", d.ToolCall)
	}
	if d.Thought != "I'm thinking." {
		t.Errorf("Thought = %q", d.Thought)
	}
}

func TestDecide_HTTPError(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{
		status:   429,
		respBody: `{"error":"rate limited; key sk-anth-secret123abc was throttled"}`,
	}
	b := newBrain(t, rt, Options{APIKey: "sk-ant-secret123abc"})
	_, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret123abc") {
		t.Errorf("error leaked API key fragment: %v", err)
	}
}

func TestDecide_TransportError(t *testing.T) {
	t.Parallel()
	b := newBrain(t, errRT{err: fmt.Errorf("connection refused")}, Options{APIKey: "sk-secret"})
	_, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("error mentions key: %v", err)
	}
}

func TestDecide_CachedTokensSplitOff(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{respBody: `{
		"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":[{"type":"function","function":{"name":"Wait","arguments":"{\"n\":1}"}}]}}],
		"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":60}}
	}`}
	b := newBrain(t, rt, Options{})
	d, err := b.Decide(context.Background(), brain.Perception{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Usage.Input != 40 || d.Usage.Cached != 60 {
		t.Errorf("Usage = %+v, want Input=40 Cached=60", d.Usage)
	}
}

func TestExtractThought(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no tags passes through", in: "  just a thought  ", want: "just a thought"},
		{name: "qwen reasoning block", in: "<think>weighing options</think>final", want: "weighing options\nfinal"},
		{name: "multiple think blocks", in: "<think>one</think>mid<think>two</think>end", want: "one\nmid\ntwo\nend"},
		{name: "open tag without close", in: "<think>trailing", want: "trailing"},
		{name: "empty input", in: "", want: ""},
		{name: "tag-only", in: "<think>only the reasoning</think>", want: "only the reasoning"},
	}
	for _, tc := range cases {
		got := extractThought(tc.in)
		// Normalise repeated whitespace so test results don't depend
		// on whether the helper happens to leave a stray space.
		got = strings.Join(strings.Fields(got), " ")
		want := strings.Join(strings.Fields(tc.want), " ")
		if got != want {
			t.Errorf("%s: extractThought(%q) = %q, want %q", tc.name, tc.in, got, want)
		}
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	in := "auth failed for key sk-abc-XYZ123 and also sk-ant-secret456 fine"
	out := redact(in)
	if strings.Contains(out, "XYZ123") || strings.Contains(out, "secret456") {
		t.Errorf("redact left fragments in: %q", out)
	}
}

func TestRenderPerception_StableJSON(t *testing.T) {
	t.Parallel()
	p := brain.Perception{
		Tick:        42,
		EntityCount: 2,
		AliveEntities: []brain.EntityView{
			{ID: 1, TypeLabel: "agent", Properties: map[string]any{"name": "the creator"}},
			{ID: 2, TypeLabel: "planet", Properties: map[string]any{"name": "Erith"}},
		},
		AliveRelationships: []brain.RelationshipView{
			{ID: 1, From: 2, To: 1, Kind: "part of"},
		},
	}
	out := RenderPerception(p)
	for _, want := range []string{`"tick": 42`, `"planet"`, `"Erith"`, `"part of"`} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderPerception missing %q\n%s", want, out)
		}
	}
}

func TestParseJSONFallback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		wantName  string
		wantValid bool
	}{
		{`{"tool":"Wait","args":{"n":2}}`, "Wait", true},
		{`{"name":"Create","arguments":{"type_label":"x"}}`, "Create", true},
		{`I am thinking, no tool.`, "", false},
		{`{"unrelated":"yes"}`, "", false},
		{`not json at all`, "", false},
	}
	for _, c := range cases {
		got, ok := parseJSONFallback(c.in)
		if ok != c.wantValid {
			t.Errorf("input %q: ok = %v, want %v", c.in, ok, c.wantValid)
			continue
		}
		if ok && got.Name != c.wantName {
			t.Errorf("input %q: name = %q, want %q", c.in, got.Name, c.wantName)
		}
	}
}

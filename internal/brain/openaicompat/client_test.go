package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
)

type fakeRT struct{ url string }

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.url = req.URL.String()
	return &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"choices":[{"message":{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"Wait","arguments":"{}"}}]}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1}
		}`))),
		Header:  make(http.Header),
		Request: req,
	}, nil
}

func TestNew_RequiresBaseURLAndModel(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{Model: "x"}); err == nil {
		t.Errorf("expected error with missing BaseURL")
	}
	if _, err := New(Options{BaseURL: "http://x"}); err == nil {
		t.Errorf("expected error with missing Model")
	}
}

func TestNew_PointsAtBaseURL(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{}
	b, err := New(Options{
		BaseURL:    "http://lmstudio:1234/v1",
		Model:      "qwen-coder",
		APIKey:     "sk-anything",
		HTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Provider() != "openaicompat" {
		t.Errorf("Provider() = %q", b.Provider())
	}
	if _, err := b.Decide(context.Background(), brain.Perception{}, nil); err != nil {
		t.Fatal(err)
	}
	if want := "http://lmstudio:1234/v1/chat/completions"; rt.url != want {
		t.Errorf("URL = %q, want %q", rt.url, want)
	}
}

type errRT struct{}

func (errRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: lookup lmstudio: no such host")
}

func TestDecide_TransportError(t *testing.T) {
	t.Parallel()
	b, err := New(Options{
		BaseURL:    "http://lmstudio:1234/v1",
		Model:      "x",
		HTTPClient: &http.Client{Transport: errRT{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decide(context.Background(), brain.Perception{}, nil); err == nil {
		t.Fatal("expected error")
	}
}

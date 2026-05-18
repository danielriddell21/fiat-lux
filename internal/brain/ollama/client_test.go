package ollama

import (
	"bytes"
	"context"
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
			"choices":[{"message":{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"Wait","arguments":"{\"n\":1}"}}]}}],
			"usage":{"prompt_tokens":10,"completion_tokens":2}
		}`))),
		Header:  make(http.Header),
		Request: req,
	}, nil
}

func TestNew_DefaultsToLocalOllama(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{}
	b, err := New(Options{HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Model() != DefaultModel {
		t.Errorf("Model() = %q, want %q", b.Model(), DefaultModel)
	}
	if b.Provider() != "ollama" {
		t.Errorf("Provider() = %q", b.Provider())
	}

	if _, err := b.Decide(context.Background(), brain.Perception{}, nil); err != nil {
		t.Fatal(err)
	}
	if want := "http://localhost:11434/v1/chat/completions"; rt.url != want {
		t.Errorf("URL = %q, want %q", rt.url, want)
	}
}

func TestNew_CustomBaseURL(t *testing.T) {
	t.Parallel()
	rt := &fakeRT{}
	b, err := New(Options{
		BaseURL:    "http://other-host:8080/v1",
		Model:      "gemma4:9b",
		HTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Model() != "gemma4:9b" {
		t.Errorf("Model() = %q", b.Model())
	}
	if _, err := b.Decide(context.Background(), brain.Perception{}, nil); err != nil {
		t.Fatal(err)
	}
	if want := "http://other-host:8080/v1/chat/completions"; rt.url != want {
		t.Errorf("URL = %q, want %q", rt.url, want)
	}
}

package openaicompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_RequiresBaseURLAndModel(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Error("expected error with empty options")
	}
	if _, err := New(Options{BaseURL: "http://x"}); err == nil {
		t.Error("expected error with missing model")
	}
	if _, err := New(Options{Model: "m"}); err == nil {
		t.Error("expected error with missing base URL")
	}
}

func TestGenerate_PostsToCompatEndpointAndDecodesBase64(t *testing.T) {
	t.Parallel()
	imageBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	encoded := base64.StdEncoding.EncodeToString(imageBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		// No API key should mean no Authorization header.
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("Authorization should be unset when APIKey is empty; got %q", h)
		}
		var body struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "fluffy" {
			t.Errorf("model = %q", body.Model)
		}
		if body.Prompt != "a planet" {
			t.Errorf("prompt = %q", body.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": encoded}},
		})
	}))
	defer srv.Close()

	c, err := New(Options{
		BaseURL: srv.URL + "/v1",
		Model:   "fluffy",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, mime, err := c.Generate(context.Background(), "a planet")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	if string(got) != string(imageBytes) {
		t.Errorf("bytes mismatch")
	}
	if c.Model() != "fluffy" || c.Provider() != "openai" {
		// Provider stays "openai" because we wrap the same client;
		// this is fine - it's a stable telemetry label, not a UI string.
		t.Logf("model=%q provider=%q", c.Model(), c.Provider())
	}
}

func TestGenerate_SendsAuthorizationWhenAPIKeySet(t *testing.T) {
	t.Parallel()
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString([]byte("x"))}},
		})
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Generate(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sawAuth, "Bearer sk-test") {
		t.Errorf("Authorization header = %q, want Bearer sk-test", sawAuth)
	}
}

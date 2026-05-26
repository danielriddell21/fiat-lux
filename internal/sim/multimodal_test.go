package sim

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/imagegen"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type fakeGenerator struct {
	bytes []byte
	calls int
}

func (f *fakeGenerator) Generate(_ context.Context, _ string) ([]byte, string, error) {
	f.calls++
	return f.bytes, "image/png", nil
}
func (f *fakeGenerator) Model() string    { return "fake" }
func (f *fakeGenerator) Provider() string { return "fake" }

func TestMultimodal_AttachesImageURL(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{
			"type_label":"planet",
			"properties":{"name":"Eos","colour":"gold"}
		}`)}},
	}}
	gen := &fakeGenerator{bytes: []byte("fakepng")}
	cache := imagegen.NewCache(t.TempDir())
	s, err := New(Options{
		World: w,
		Brain: br,
		Multimodal: &MultimodalOptions{
			Generator: gen,
			Cache:     cache,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.WaitForImages()
	if gen.calls != 1 {
		t.Errorf("generator calls = %d, want 1", gen.calls)
	}
	// The most recent live entity should carry an image_url.
	var url string
	for _, e := range w.Entities() {
		if u, ok := e.Properties["image_url"].(string); ok {
			url = u
		}
	}
	if url == "" || !strings.HasPrefix(url, "/api/image/") {
		t.Errorf("entity missing image_url: %v", w.Entities())
	}
}

func TestMultimodal_RespectsMinPropsCount(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{
			"type_label":"ocean"
		}`)}},
	}}
	gen := &fakeGenerator{bytes: []byte("x")}
	s, err := New(Options{
		World: w,
		Brain: br,
		Multimodal: &MultimodalOptions{
			Generator:     gen,
			Cache:         imagegen.NewCache(t.TempDir()),
			MinPropsCount: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.WaitForImages()
	if gen.calls != 0 {
		t.Errorf("expected MinPropsCount to suppress generator call; got %d", gen.calls)
	}
}

func TestMultimodal_CacheHitSkipsGenerator(t *testing.T) {
	t.Parallel()
	cache := imagegen.NewCache(t.TempDir())
	prompt := imagegen.DefaultPromptBuilder("planet", map[string]any{"name": "Eos", "colour": "gold"})
	if err := cache.Put(prompt, []byte("cached")); err != nil {
		t.Fatal(err)
	}

	w, _ := world.New("kosmos")
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{
			"type_label":"planet",
			"properties":{"name":"Eos","colour":"gold"}
		}`)}},
	}}
	gen := &fakeGenerator{bytes: []byte("ignored")}
	s, err := New(Options{
		World:      w,
		Brain:      br,
		Multimodal: &MultimodalOptions{Generator: gen, Cache: cache},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.WaitForImages()
	if gen.calls != 0 {
		t.Errorf("cache hit should suppress generator; calls=%d", gen.calls)
	}
}

func TestMultimodal_DisabledWhenNil(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	br := &scriptedBrain{decisions: []brain.Decision{
		{ToolCall: &brain.ToolCall{Name: "Create", Args: json.RawMessage(`{
			"type_label":"planet",
			"properties":{"name":"Eos"}
		}`)}},
	}}
	s, _ := New(Options{World: w, Brain: br})
	if _, err := s.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range w.Entities() {
		if _, ok := e.Properties["image_url"]; ok {
			t.Errorf("entity unexpectedly carries image_url: %+v", e.Properties)
		}
	}
}

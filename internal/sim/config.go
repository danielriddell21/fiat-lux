package sim

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/danielriddell21/fiat-lux/internal/drives"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/narrator"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Config is the YAML-deserialised multi-world configuration. The
// production form lives in examples/cloud-haiku.yaml and
// examples/local-qwen.yaml.
type Config struct {
	Worlds          []WorldConfig `yaml:"worlds"`
	ReflectInterval uint64        `yaml:"reflect_interval,omitempty"`
	MaxAgents       int           `yaml:"max_agents,omitempty"`
	MaxSpawnDepth   int           `yaml:"max_spawn_depth,omitempty"`

	// Web configures the optional in-process web viewer. When Addr is
	// non-empty the CLI starts an HTTP server at that address alongside
	// the TUI. The --web-addr CLI flag takes precedence when both set.
	Web WebConfig `yaml:"web,omitempty"`

	// Save configures persistence cadence. Empty / "manual" means the
	// user presses 's' to save (legacy behaviour). The --save-mode CLI
	// flag takes precedence when both set.
	Save SaveConfig `yaml:"save,omitempty"`

	// Multimodal, when set, attaches an image generator to every
	// world. The --multimodal CLI flag takes precedence when both
	// are supplied; the --image-cache flag overrides CacheDir.
	Multimodal MultimodalConfig `yaml:"multimodal,omitempty"`
}

// WebConfig is the YAML form of the web viewer options.
type WebConfig struct {
	// Addr is the listen address, e.g. ":8080" or "127.0.0.1:8080".
	// Empty disables the web viewer.
	Addr string `yaml:"addr,omitempty"`
}

// SaveConfig controls how frequently the world is persisted.
type SaveConfig struct {
	// Mode is parsed by store.ParseSaveMode. Accepted forms:
	//   "" / "manual"         - user-triggered save only (default)
	//   "interval:<duration>" - periodic background save, e.g. "30s"
	Mode string `yaml:"mode,omitempty"`
}

// MultimodalConfig is the YAML form of the image generator options.
// An empty Provider disables image generation.
type MultimodalConfig struct {
	// Provider selects the generator. Accepted: "openai" |
	// "openaicompat". Empty disables.
	Provider string `yaml:"provider,omitempty"`

	// Model is the image model name (e.g. "gpt-image-1", "dall-e-3",
	// or a local server's model id). Optional for "openai".
	Model string `yaml:"model,omitempty"`

	// BaseURL is required for "openaicompat" and ignored for
	// "openai".
	BaseURL string `yaml:"base_url,omitempty"`

	// CacheDir overrides the default on-disk cache location
	// ($XDG_CACHE_HOME/fiatlux/images). The --image-cache CLI flag
	// takes precedence when both are set.
	CacheDir string `yaml:"cache_dir,omitempty"`

	// MinPropsCount skips image generation for entities with fewer
	// than this many properties. Zero defaults to 1.
	MinPropsCount int `yaml:"min_props_count,omitempty"`
}

// Spec returns the CLI-equivalent --multimodal spec for this config,
// or "" when the provider is empty (disabled). The string is
// suitable for handing to the same parser the --multimodal flag
// uses, so YAML and CLI paths converge.
func (m MultimodalConfig) Spec() string {
	switch m.Provider {
	case "":
		return ""
	case "openai":
		if m.Model == "" {
			return "openai"
		}
		return "openai:" + m.Model
	case "openaicompat":
		if m.BaseURL == "" || m.Model == "" {
			return ""
		}
		return "openaicompat:" + m.BaseURL + "::" + m.Model
	default:
		return ""
	}
}

// WorldConfig is one world's spec.
type WorldConfig struct {
	Name     string         `yaml:"name"`
	Agent    AgentConfig    `yaml:"agent"`
	Narrator NarratorConfig `yaml:"narrator,omitempty"`
}

// NarratorConfig configures the per-world chronicler. Empty Brain
// disables the narrator.
type NarratorConfig struct {
	Brain            BrainConfig `yaml:"brain"`
	IntervalTicks    uint64      `yaml:"interval_ticks,omitempty"`
	MinEvents        int         `yaml:"min_events,omitempty"`
	MaxChapterLength int         `yaml:"max_chapter_length,omitempty"`
	SystemPrompt     string      `yaml:"system_prompt,omitempty"`
}

// AgentConfig is the root agent's spec for a world.
type AgentConfig struct {
	Name         string             `yaml:"name,omitempty"`
	SystemPrompt string             `yaml:"system_prompt,omitempty"`
	Brain        BrainConfig        `yaml:"brain"`
	Drives       map[string]float64 `yaml:"drives,omitempty"`
}

// BrainConfig captures provider + model. Spec is an alternative
// form: a full string like "anthropic:claude-haiku-4-5".
type BrainConfig struct {
	Spec     string `yaml:"spec,omitempty"`
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

// resolveSpec returns the brain spec string used by BrainFactory.
func (b BrainConfig) resolveSpec() string {
	if b.Spec != "" {
		return b.Spec
	}
	if b.Provider == "" {
		return "stub"
	}
	if b.Model == "" {
		return b.Provider
	}
	return b.Provider + ":" + b.Model
}

// LoadYAML parses a Config from the reader.
func LoadYAML(r io.Reader) (*Config, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("sim: read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(buf, &c); err != nil {
		return nil, fmt.Errorf("sim: parse yaml: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks that the config is internally consistent before
// the universe is built.
func (c *Config) Validate() error {
	if len(c.Worlds) == 0 {
		return errors.New("sim: config has no worlds")
	}
	seen := map[string]struct{}{}
	for i, w := range c.Worlds {
		if w.Name == "" {
			return fmt.Errorf("sim: worlds[%d] has empty name", i)
		}
		if _, dup := seen[w.Name]; dup {
			return fmt.Errorf("sim: duplicate world name %q", w.Name)
		}
		seen[w.Name] = struct{}{}
		if w.Agent.Brain.resolveSpec() == "" {
			return fmt.Errorf("sim: world %q has no agent.brain spec", w.Name)
		}
	}
	if err := c.Multimodal.validate(); err != nil {
		return err
	}
	return nil
}

func (m MultimodalConfig) validate() error {
	switch m.Provider {
	case "", "openai":
		return nil
	case "openaicompat":
		if m.BaseURL == "" {
			return errors.New("sim: multimodal.base_url required for provider=openaicompat")
		}
		if m.Model == "" {
			return errors.New("sim: multimodal.model required for provider=openaicompat")
		}
		return nil
	default:
		return fmt.Errorf("sim: unknown multimodal.provider %q (try openai | openaicompat)", m.Provider)
	}
}

// WorldLoader returns a pre-existing world by name. BuildUniverse
// calls it for each configured world; nil or an error means the
// universe falls back to creating a fresh world.
type WorldLoader func(ctx context.Context, name string) (*world.World, error)

// BuildUniverse turns a Config into a Universe by constructing one
// Sim per world. The embedder is shared so all worlds use the same
// embedding pipeline. When loader is non-nil it is consulted first
// for each world so saved state is preserved across runs.
func (c *Config) BuildUniverse(
	ctx context.Context,
	factory BrainFactory,
	embedder memory.Embedder,
	loader WorldLoader,
) (*Universe, error) {
	if factory == nil {
		return nil, errors.New("sim: BuildUniverse requires a BrainFactory")
	}
	sims := make([]*Sim, 0, len(c.Worlds))
	for _, wc := range c.Worlds {
		w, err := loadOrNew(ctx, loader, wc.Name)
		if err != nil {
			closeAll(sims)
			return nil, fmt.Errorf("sim: world %q: %w", wc.Name, err)
		}
		spec := wc.Agent.Brain.resolveSpec()
		br, err := factory(ctx, spec)
		if err != nil {
			closeAll(sims)
			return nil, fmt.Errorf("sim: world %q brain %q: %w", wc.Name, spec, err)
		}
		narr, err := buildNarrator(ctx, factory, wc.Narrator)
		if err != nil {
			_ = br.Close()
			closeAll(sims)
			return nil, fmt.Errorf("sim: world %q narrator: %w", wc.Name, err)
		}
		s, err := New(Options{
			World:           w,
			Brain:           br,
			Embedder:        embedder,
			ReflectInterval: c.ReflectInterval,
			BrainFactory:    factory,
			MaxAgents:       c.MaxAgents,
			MaxSpawnDepth:   c.MaxSpawnDepth,
			AgentName:       wc.Agent.Name,
			SystemPrompt:    wc.Agent.SystemPrompt,
			Drives:          drives.State(wc.Agent.Drives),
			Narrator:        narr,
		})
		if err != nil {
			_ = br.Close()
			if narr != nil {
				_ = narr.Close()
			}
			closeAll(sims)
			return nil, fmt.Errorf("sim: world %q: %w", wc.Name, err)
		}
		sims = append(sims, s)
	}
	return NewUniverse(sims)
}

// buildNarrator constructs a narrator from its YAML config slice.
// Returns nil with no error when the config has no brain spec
// (narrator disabled for that world).
func buildNarrator(ctx context.Context, factory BrainFactory, cfg NarratorConfig) (*narrator.Narrator, error) {
	spec := cfg.Brain.resolveSpec()
	if spec == "" || spec == "stub" && cfg.Brain.Provider == "" {
		// Empty config means disabled.
		if cfg.Brain.Provider == "" && cfg.Brain.Model == "" && cfg.Brain.Spec == "" {
			return nil, nil
		}
	}
	br, err := factory(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("narrator brain %q: %w", spec, err)
	}
	return narrator.New(narrator.Options{
		Brain:            br,
		SystemPrompt:     cfg.SystemPrompt,
		IntervalTicks:    cfg.IntervalTicks,
		MinEvents:        cfg.MinEvents,
		MaxChapterLength: cfg.MaxChapterLength,
	})
}

// loadOrNew consults the loader for a saved world; falls back to a
// fresh empty world when no loader is supplied or the lookup yields
// no result. Any non-nil error from the loader is returned verbatim.
func loadOrNew(ctx context.Context, loader WorldLoader, name string) (*world.World, error) {
	if loader != nil {
		w, err := loader(ctx, name)
		if err == nil && w != nil {
			return w, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return world.New(name)
}

func closeAll(sims []*Sim) {
	for _, s := range sims {
		_ = s.Close()
	}
}

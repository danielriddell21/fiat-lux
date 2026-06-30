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

type Config struct {
	Worlds          []WorldConfig `yaml:"worlds"`
	ReflectInterval uint64        `yaml:"reflect_interval,omitempty"`
	MaxAgents       int           `yaml:"max_agents,omitempty"`
	MaxSpawnDepth   int           `yaml:"max_spawn_depth,omitempty"`

	Web WebConfig `yaml:"web,omitempty"`

	Save SaveConfig `yaml:"save,omitempty"`

	Multimodal MultimodalConfig `yaml:"multimodal,omitempty"`
}

type WebConfig struct {
	Addr string `yaml:"addr,omitempty"`
}

type SaveConfig struct {
	Mode string `yaml:"mode,omitempty"`
}

type MultimodalConfig struct {
	Provider string `yaml:"provider,omitempty"`

	Model string `yaml:"model,omitempty"`

	BaseURL string `yaml:"base_url,omitempty"`

	CacheDir string `yaml:"cache_dir,omitempty"`

	MinPropsCount int `yaml:"min_props_count,omitempty"`
}

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

type WorldConfig struct {
	Name     string         `yaml:"name"`
	Agent    AgentConfig    `yaml:"agent"`
	Narrator NarratorConfig `yaml:"narrator,omitempty"`
}

type NarratorConfig struct {
	Brain            BrainConfig `yaml:"brain"`
	IntervalTicks    uint64      `yaml:"interval_ticks,omitempty"`
	MinEvents        int         `yaml:"min_events,omitempty"`
	MaxChapterLength int         `yaml:"max_chapter_length,omitempty"`
	SystemPrompt     string      `yaml:"system_prompt,omitempty"`
}

type AgentConfig struct {
	Name         string             `yaml:"name,omitempty"`
	SystemPrompt string             `yaml:"system_prompt,omitempty"`
	Brain        BrainConfig        `yaml:"brain"`
	Drives       map[string]float64 `yaml:"drives,omitempty"`
}

type BrainConfig struct {
	Spec     string `yaml:"spec,omitempty"`
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

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

type WorldLoader func(ctx context.Context, name string) (*world.World, error)

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
	n, err := narrator.New(narrator.Options{
		Brain:            br,
		SystemPrompt:     cfg.SystemPrompt,
		IntervalTicks:    cfg.IntervalTicks,
		MinEvents:        cfg.MinEvents,
		MaxChapterLength: cfg.MaxChapterLength,
	})
	if err != nil {
		return nil, fmt.Errorf("build narrator: %w", err)
	}
	return n, nil
}

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
	w, err := world.New(name)
	if err != nil {
		return nil, fmt.Errorf("new world: %w", err)
	}
	return w, nil
}

func closeAll(sims []*Sim) {
	for _, s := range sims {
		_ = s.Close()
	}
}

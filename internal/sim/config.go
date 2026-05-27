package sim

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/danielriddell21/fiat-lux/internal/drives"
	"github.com/danielriddell21/fiat-lux/internal/memory"
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

// WorldConfig is one world's spec.
type WorldConfig struct {
	Name  string      `yaml:"name"`
	Agent AgentConfig `yaml:"agent"`
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
	return nil
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
		})
		if err != nil {
			_ = br.Close()
			closeAll(sims)
			return nil, fmt.Errorf("sim: world %q: %w", wc.Name, err)
		}
		sims = append(sims, s)
	}
	return NewUniverse(sims)
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

// Command fiatlux is the entrypoint for the fiat-lux CLI/TUI.
//
// fiat-lux drops an AI agent into an empty world and gives it tools to
// create. The `run` subcommand launches the TUI. Bare invocation
// prints a banner.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/brain/anthropic"
	"github.com/danielriddell21/fiat-lux/internal/brain/ollama"
	"github.com/danielriddell21/fiat-lux/internal/brain/openai"
	"github.com/danielriddell21/fiat-lux/internal/brain/openaicompat"
	"github.com/danielriddell21/fiat-lux/internal/brain/stub"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/sim"
	"github.com/danielriddell21/fiat-lux/internal/store"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/tui"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const banner = `
   .-.   _       _    _
  ( _ ) | | __ _| |_ | |  _   ___  __
  / _ \ | |/ _' | __|| | | | | \ \/ /
 | (_) || | (_| | |_ | |_| |_| |>  <
  \___/ |_|\__,_|\__||_|\__,_|___/_/\_\

  fiat-lux  -  let there be light
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "fiatlux:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return cmdRun(args[1:], stdout, stderr)
		case "replay":
			return cmdReplay(args[1:], stdout, stderr)
		case "help", "-h", "--help":
			return printHelp(stdout)
		case "-version", "--version", "version":
			_, err := fmt.Fprintln(stdout, version)
			return err
		}
	}
	return printBanner(args, stdout)
}

func printBanner(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("fiatlux", flag.ContinueOnError)
	fs.SetOutput(out)
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(out, version)
		return err
	}
	if _, err := fmt.Fprint(out, banner); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "\n  version: %s\n", version); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "  try 'fiatlux run' to launch the TUI, or 'fiatlux help' for options.")
	return err
}

func printHelp(out io.Writer) error {
	_, err := fmt.Fprintln(out, `Usage:

  fiatlux                       Print banner and version.
  fiatlux -version              Print version.
  fiatlux run [flags]           Launch the TUI.
  fiatlux replay --db <path> --world <name> [--replay-tick <duration>]
                                Replay a saved world tick by tick.

run flags:
  --world <name>      Name of the world to open or create.    (default: kosmos)
  --db <path>         SQLite database path for persistence.   (default: in-memory)
  --brain <spec>      Brain config. Supported:
                        stub                        random valid tool calls (default; free)
                        stub:<seed>                 deterministic stub
                        anthropic[:<model>]         Messages API (env: ANTHROPIC_API_KEY)
                        openai[:<model>]            Chat Completions (env: OPENAI_API_KEY)
                        ollama[:<model>]            local OpenAI-compatible endpoint
                        openaicompat:<url>::<model> generic OpenAI-compatible endpoint
                                                    (env: FIATLUX_OPENAICOMPAT_KEY for auth)
                        none                        no sim; viewer only
  --tick <duration>   Sim tick interval.                       (default: 1.5s)
  --embedder <spec>   Memory embedder for relevance retrieval:
                        zero                     (default; no relevance contribution)
                        hash                     (deterministic, free, useful for tests)
                        ollama[:<model>]         local Ollama embeddings
                        openai[:<model>]         OpenAI embeddings (uses OPENAI_API_KEY)
  --importance <spec> Memory importance scorer:
                        heuristic                (default; free, kind+length signals)
                        llm                      (extra brain call per memory)
  --reflect-interval  Ticks between reflection passes. 0 disables. (default: 20)
  --max-agents <n>    Cap on total agents per world.              (default: 8)
  --max-spawn-depth   Cap on the spawn graph height.              (default: 3)
  --config <path>     YAML config for a multi-world universe.
                      Overrides --brain. See examples/cloud-haiku.yaml.`)
	return err
}

func cmdRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fiatlux run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	worldName := fs.String("world", "kosmos", "name of the world to open or create")
	dbPath := fs.String("db", "", "SQLite database path (empty = in-memory, no persistence)")
	brainSpec := fs.String("brain", "stub", "brain config; see 'fiatlux help'")
	tickInterval := fs.Duration("tick", tui.DefaultTickInterval, "sim tick interval")
	embedderSpec := fs.String("embedder", "zero", "memory embedder (zero | hash | ollama[:model] | openai[:model])")
	importanceSpec := fs.String("importance", "heuristic", "memory importance scorer (heuristic | llm)")
	reflectInterval := fs.Uint64("reflect-interval", 20, "ticks between reflection passes (0 = disable)")
	maxAgents := fs.Int("max-agents", 8, "cap on total agents per world")
	maxSpawnDepth := fs.Int("max-spawn-depth", 3, "cap on the spawn graph height")
	configPath := fs.String("config", "", "YAML config for a multi-world universe (overrides --brain)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var (
		st     tui.Storer
		closer io.Closer
	)
	if *dbPath != "" {
		s, err := store.Open(ctx, *dbPath)
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		st = s
		closer = s
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	w, err := loadOrNewWorld(ctx, st, *worldName)
	if err != nil {
		return err
	}

	var stepper tui.Stepper
	switch {
	case *configPath != "":
		embedder, err := buildEmbedder(*embedderSpec)
		if err != nil {
			return fmt.Errorf("build embedder: %w", err)
		}
		u, err := loadUniverse(ctx, *configPath, embedder)
		if err != nil {
			return err
		}
		defer func() { _ = u.Close() }()
		// Override the loaded world with the universe's focused
		// world so save/load and the TUI agree.
		w = u.Focused().World
		stepper = newUniverseAdapter(u)
	case *brainSpec != "none":
		embedder, err := buildEmbedder(*embedderSpec)
		if err != nil {
			return fmt.Errorf("build embedder: %w", err)
		}
		s, err := buildSim(w, *brainSpec, embedder, *importanceSpec, *reflectInterval, *maxAgents, *maxSpawnDepth)
		if err != nil {
			return fmt.Errorf("build sim: %w", err)
		}
		defer func() { _ = s.Close() }()
		stepper = newSimAdapter(s)
	}

	if err := tui.Run(ctx, tui.RunOptions{
		World:        w,
		Store:        st,
		Sim:          stepper,
		TickInterval: *tickInterval,
		Output:       stdout,
	}); err != nil {
		return err
	}
	_ = stderr
	return nil
}

// buildSim wires up a Sim from the brain spec string. Supported specs:
//
//	stub                              - default; deterministic stub
//	stub:<seed>                       - stub with explicit seed
//	anthropic[:<model>]               - Anthropic Messages
//	openai[:<model>]                  - OpenAI Chat Completions
//	ollama[:<model>]                  - local Ollama (/v1)
//	openaicompat:<base_url>::<model>  - any OpenAI-compatible endpoint
//
// "none" is handled by the caller (no sim attached).
func buildSim(w *world.World, spec string, embedder memory.Embedder, importanceSpec string, reflectInterval uint64, maxAgents, maxSpawnDepth int) (*sim.Sim, error) {
	br, err := buildBrain(spec)
	if err != nil {
		return nil, err
	}
	var importance memory.Importance
	switch importanceSpec {
	case "", "heuristic":
		// Default - nil means memory.Add falls back to HeuristicScorer.
	case "llm":
		importance = memory.LLMScorer{Brain: br}
	default:
		return nil, fmt.Errorf("importance spec %q not recognised (try heuristic | llm)", importanceSpec)
	}
	factory := func(_ context.Context, childSpec string) (brain.Brain, error) {
		return buildBrain(childSpec)
	}
	return sim.New(sim.Options{
		World:           w,
		Brain:           br,
		Tools:           tools.Default(),
		Embedder:        embedder,
		Importance:      importance,
		ReflectInterval: reflectInterval,
		BrainFactory:    factory,
		MaxAgents:       maxAgents,
		MaxSpawnDepth:   maxSpawnDepth,
	})
}

// buildEmbedder parses --embedder. Supported:
//
//	zero | (nothing)             zero-vector fallback; no relevance
//	hash                         deterministic hash; for tests
//	ollama[:<model>]             Ollama /api/embeddings; default nomic-embed-text
//	openai[:<model>]             OpenAI /v1/embeddings; default text-embedding-3-small
func buildEmbedder(spec string) (memory.Embedder, error) {
	if spec == "" || spec == "zero" {
		return memory.ZeroEmbedder{}, nil
	}
	if spec == "hash" {
		return memory.HashEmbedder{Dim: 64}, nil
	}
	head, tail, _ := strings.Cut(spec, ":")
	switch head {
	case "ollama":
		return memory.NewOllama(memory.OllamaOptions{Model: tail}), nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("embedder openai: OPENAI_API_KEY is required")
		}
		return memory.NewOpenAI(memory.OpenAIOptions{APIKey: key, Model: tail})
	}
	return nil, fmt.Errorf("embedder spec %q not recognised", spec)
}

func buildBrain(spec string) (brain.Brain, error) {
	if spec == "" {
		spec = "stub"
	}
	head, tail, _ := strings.Cut(spec, ":")
	switch head {
	case "stub":
		if tail == "" {
			return stub.New(uint64(time.Now().UnixNano()), rand.Uint64(), tools.Default()), nil
		}
		seed, err := strconv.ParseUint(tail, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("brain stub seed %q: %w", tail, err)
		}
		return stub.New(seed, seed*0x9E3779B97F4A7C15, tools.Default()), nil

	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, errors.New("brain anthropic: ANTHROPIC_API_KEY is required")
		}
		model := tail
		if model == "" {
			model = anthropic.DefaultModel
		}
		return anthropic.New(anthropic.Options{
			APIKey:       key,
			Model:        model,
			SystemPrompt: sim.DefaultSystemPrompt,
			HTTPClient:   &http.Client{Timeout: 90 * time.Second},
		})

	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("brain openai: OPENAI_API_KEY is required")
		}
		model := tail
		if model == "" {
			model = openai.DefaultModel
		}
		return openai.New(openai.Options{
			APIKey:       key,
			Model:        model,
			SystemPrompt: sim.DefaultSystemPrompt,
			HTTPClient:   &http.Client{Timeout: 90 * time.Second},
		})

	case "ollama":
		model := tail
		if model == "" {
			model = ollama.DefaultModel
		}
		return ollama.New(ollama.Options{
			Model:        model,
			SystemPrompt: sim.DefaultSystemPrompt,
			HTTPClient:   &http.Client{Timeout: 5 * time.Minute},
		})

	case "openaicompat":
		// Spec form: openaicompat:<base_url>::<model>. We split on
		// the first "::" so URLs containing ":" (ports) parse cleanly.
		base, model, ok := strings.Cut(tail, "::")
		if !ok || base == "" || model == "" {
			return nil, errors.New(`brain openaicompat: expected "openaicompat:<base_url>::<model>"`)
		}
		return openaicompat.New(openaicompat.Options{
			BaseURL:      base,
			APIKey:       os.Getenv("FIATLUX_OPENAICOMPAT_KEY"),
			Model:        model,
			SystemPrompt: sim.DefaultSystemPrompt,
			HTTPClient:   &http.Client{Timeout: 5 * time.Minute},
		})
	}
	return nil, fmt.Errorf("brain spec %q not recognised; try 'fiatlux help'", spec)
}

// simAdapter bridges *sim.Sim to tui.Stepper / MemoryAccessor /
// AgentLister - the TUI is kept independent of internal/sim so its
// tests don't need the sim package.
type simAdapter struct {
	s       *sim.Sim
	focusMu sync.Mutex
	focusID uint64
}

func newSimAdapter(s *sim.Sim) *simAdapter {
	a := &simAdapter{s: s}
	if root := s.Agent(); root != nil {
		a.focusID = uint64(root.EntityID)
	}
	return a
}

func (a *simAdapter) Step(ctx context.Context) (tui.StepSummary, error) {
	res, err := a.s.Step(ctx)
	if err != nil {
		return tui.StepSummary{}, err
	}
	return tui.StepSummary{
		Tick:       uint64(res.Tick),
		Skipped:    res.Skipped,
		Thought:    res.Thought,
		ToolName:   res.ToolName,
		ToolResult: res.ToolResult,
		ToolErr:    res.ToolErr,
	}, nil
}

// Agents satisfies tui.AgentLister.
func (a *simAdapter) Agents() []tui.AgentInfo {
	roster := a.s.Agents()
	out := make([]tui.AgentInfo, len(roster))
	for i, ag := range roster {
		out[i] = tui.AgentInfo{
			ID:        uint64(ag.EntityID),
			Name:      ag.Name,
			IsCreator: ag.SpawnDepth == 0,
		}
	}
	return out
}

func (a *simAdapter) FocusedAgentID() uint64 {
	a.focusMu.Lock()
	defer a.focusMu.Unlock()
	return a.focusID
}

func (a *simAdapter) SetFocusedAgentID(id uint64) {
	a.focusMu.Lock()
	a.focusID = id
	a.focusMu.Unlock()
}

// MemorySnapshot satisfies tui.MemoryAccessor: returns the focused
// agent's memory stream.
func (a *simAdapter) MemorySnapshot() tui.MemorySnapshot {
	a.focusMu.Lock()
	focused := a.focusID
	a.focusMu.Unlock()

	roster := a.s.Agents()
	if len(roster) == 0 {
		return tui.MemorySnapshot{}
	}
	var ag = roster[0]
	for _, x := range roster {
		if uint64(x.EntityID) == focused {
			ag = x
			break
		}
	}
	if ag.Memory == nil {
		return tui.MemorySnapshot{AgentName: ag.Name}
	}
	recs := ag.Memory.All()
	out := make([]tui.MemoryRecord, len(recs))
	for i, r := range recs {
		out[i] = tui.MemoryRecord{
			ID:         uint64(r.ID),
			Kind:       string(r.Kind),
			Content:    r.Content,
			Tick:       uint64(r.CreatedAt),
			Importance: r.Importance,
		}
	}
	return tui.MemorySnapshot{AgentName: ag.Name, Records: out}
}

// universeAdapter bridges *sim.Universe to tui.Stepper /
// WorldProvider / UniverseLister / AgentLister / MemoryAccessor.
// Focus state for agents is held per-world so each world remembers
// its own selection across ctrl-tab swaps.
type universeAdapter struct {
	u            *sim.Universe
	focusMu      sync.Mutex
	focusByWorld map[int]uint64
}

func newUniverseAdapter(u *sim.Universe) *universeAdapter {
	a := &universeAdapter{u: u, focusByWorld: make(map[int]uint64)}
	for i, s := range u.Sims() {
		if root := s.Agent(); root != nil {
			a.focusByWorld[i] = uint64(root.EntityID)
		}
	}
	return a
}

func (a *universeAdapter) Step(ctx context.Context) (tui.StepSummary, error) {
	res, err := a.u.Step(ctx)
	if err != nil {
		return tui.StepSummary{}, err
	}
	return tui.StepSummary{
		Tick:       uint64(res.Tick),
		Skipped:    res.Skipped,
		Thought:    res.Thought,
		ToolName:   res.ToolName,
		ToolResult: res.ToolResult,
		ToolErr:    res.ToolErr,
	}, nil
}

// FocusedWorld satisfies tui.WorldProvider.
func (a *universeAdapter) FocusedWorld() *world.World {
	return a.u.Focused().World
}

// Worlds / FocusedWorldIdx / SetFocusedWorldIdx / CycleFocusedWorld
// satisfy tui.UniverseLister.
func (a *universeAdapter) Worlds() []tui.WorldInfo {
	src := a.u.Worlds()
	out := make([]tui.WorldInfo, len(src))
	for i, w := range src {
		out[i] = tui.WorldInfo{
			Index: w.Index, Name: w.Name,
			Tick: uint64(w.Tick), EntityCount: w.EntityCount, AgentCount: w.AgentCount,
		}
	}
	return out
}

func (a *universeAdapter) FocusedWorldIdx() int        { return a.u.FocusedIdx() }
func (a *universeAdapter) SetFocusedWorldIdx(i int)    { a.u.SetFocusedIdx(i) }
func (a *universeAdapter) CycleFocusedWorld(delta int) { a.u.CycleFocus(delta) }

// Agents / FocusedAgentID / SetFocusedAgentID satisfy
// tui.AgentLister, scoped to the focused world.
func (a *universeAdapter) Agents() []tui.AgentInfo {
	roster := a.u.Focused().Agents()
	out := make([]tui.AgentInfo, len(roster))
	for i, ag := range roster {
		out[i] = tui.AgentInfo{
			ID:        uint64(ag.EntityID),
			Name:      ag.Name,
			IsCreator: ag.SpawnDepth == 0,
		}
	}
	return out
}

func (a *universeAdapter) FocusedAgentID() uint64 {
	a.focusMu.Lock()
	defer a.focusMu.Unlock()
	return a.focusByWorld[a.u.FocusedIdx()]
}

func (a *universeAdapter) SetFocusedAgentID(id uint64) {
	a.focusMu.Lock()
	a.focusByWorld[a.u.FocusedIdx()] = id
	a.focusMu.Unlock()
}

// MemorySnapshot satisfies tui.MemoryAccessor: returns the
// currently-focused agent in the currently-focused world.
func (a *universeAdapter) MemorySnapshot() tui.MemorySnapshot {
	focused := a.FocusedAgentID()
	roster := a.u.Focused().Agents()
	if len(roster) == 0 {
		return tui.MemorySnapshot{}
	}
	ag := roster[0]
	for _, x := range roster {
		if uint64(x.EntityID) == focused {
			ag = x
			break
		}
	}
	if ag.Memory == nil {
		return tui.MemorySnapshot{AgentName: ag.Name}
	}
	recs := ag.Memory.All()
	out := make([]tui.MemoryRecord, len(recs))
	for i, r := range recs {
		out[i] = tui.MemoryRecord{
			ID:         uint64(r.ID),
			Kind:       string(r.Kind),
			Content:    r.Content,
			Tick:       uint64(r.CreatedAt),
			Importance: r.Importance,
		}
	}
	return tui.MemorySnapshot{AgentName: ag.Name, Records: out}
}

// loadUniverse opens the YAML config and builds a Universe from it.
func loadUniverse(ctx context.Context, path string, embedder memory.Embedder) (*sim.Universe, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := sim.LoadYAML(f)
	if err != nil {
		return nil, err
	}
	factory := func(_ context.Context, spec string) (brain.Brain, error) {
		return buildBrain(spec)
	}
	return cfg.BuildUniverse(ctx, factory, embedder)
}

// cmdReplay opens a stored world's event log and walks it tick by
// tick. The TUI watches a passive Stepper that applies one event
// per tick interval; no brain is consulted.
func cmdReplay(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fiatlux replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "SQLite database path (required)")
	worldName := fs.String("world", "kosmos", "name of the world to replay")
	replayTick := fs.Duration("replay-tick", 100*time.Millisecond, "delay between replayed events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("replay: --db is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	loaded, err := st.Load(ctx, *worldName)
	if err != nil {
		return fmt.Errorf("load world: %w", err)
	}
	events := loaded.Events()
	if len(events) == 0 {
		_, _ = fmt.Fprintln(stdout, "(world has no events to replay)")
		return nil
	}

	rp, err := sim.NewReplay(*worldName, events)
	if err != nil {
		return err
	}
	stepper := replayAdapter{r: rp}

	if err := tui.Run(ctx, tui.RunOptions{
		World:        rp.World,
		Sim:          stepper,
		TickInterval: *replayTick,
		Output:       stdout,
	}); err != nil {
		return err
	}
	return nil
}

// replayAdapter wraps a *sim.Replay so the TUI can consume it via
// the existing Stepper / WorldProvider interfaces.
type replayAdapter struct{ r *sim.Replay }

func (a replayAdapter) Step(ctx context.Context) (tui.StepSummary, error) {
	res, err := a.r.Step(ctx)
	if errors.Is(err, sim.ErrReplayDone) {
		// Treat completion as a skipped step so the TUI keeps
		// rendering the final frame without erroring.
		return tui.StepSummary{Tick: uint64(res.Tick), Skipped: true,
			ToolResult: "replay complete"}, nil
	}
	if err != nil {
		return tui.StepSummary{}, err
	}
	return tui.StepSummary{
		Tick:       uint64(res.Tick),
		Thought:    res.Thought,
		ToolName:   res.ToolName,
		ToolResult: res.ToolResult,
	}, nil
}

func (a replayAdapter) FocusedWorld() *world.World { return a.r.World }

// loadOrNewWorld returns the named world from the store if it
// exists, otherwise constructs a fresh empty one. With no store,
// it always returns a fresh world.
func loadOrNewWorld(ctx context.Context, st tui.Storer, name string) (*world.World, error) {
	if loader, ok := st.(interface {
		Load(ctx context.Context, name string) (*world.World, error)
	}); ok {
		w, err := loader.Load(ctx, name)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, store.ErrWorldNotFound) {
			return nil, fmt.Errorf("load world: %w", err)
		}
	}
	w, err := world.New(name)
	if err != nil {
		return nil, fmt.Errorf("new world: %w", err)
	}
	return w, nil
}

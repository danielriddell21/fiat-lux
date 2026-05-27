// Command fiatlux is the entrypoint for the fiat-lux CLI/TUI.
//
// fiat-lux drops an AI agent into an empty world and gives it tools to
// create. The `run` subcommand launches the TUI. Bare invocation
// prints a banner.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
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
	"github.com/danielriddell21/fiat-lux/internal/webui"
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
		case "step":
			return cmdStep(args[1:], stdout, stderr)
		case "serve":
			return cmdServe(args[1:], stdout, stderr)
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
	_, err := fmt.Fprintln(out, "  try 'fiatlux run' for the TUI, 'fiatlux serve' for the web viewer, or 'fiatlux help' for options.")
	return err
}

func printHelp(out io.Writer) error {
	_, err := fmt.Fprintln(out, `Usage:

  fiatlux                       Print banner and version.
  fiatlux -version              Print version.
  fiatlux run [flags]           Launch the TUI.
  fiatlux replay --db <dsn> --world <name> [--replay-tick <duration>]
                                Replay a saved world tick by tick.
  fiatlux step --count N [flags]
                                Run the sim headlessly for N steps and
                                print a summary. No TUI, no /dev/tty
                                required. Useful for CI smoke tests
                                and benchmarks. Accepts the same brain
                                / embedder / db / config flags as run,
                                plus --json for machine-readable output.
  fiatlux serve [flags]         Run the sim headlessly with the embedded
                                web viewer attached. No TUI. Suitable for
                                container deployments. Accepts the same
                                flags as run; --web-addr defaults to
                                :8080.

run flags:
  --world <name>      Name of the world to open or create.    (default: kosmos)
  --db <dsn>          Database DSN. Empty = in-memory, no persistence.
                      Local file:    ./kosmos.db, /abs/foo.db, :memory:
                      Local URI:     file:./foo.db?_journal_mode=WAL
                      libSQL remote: libsql://<host>?authToken=...
                                     http(s)://<sqld-host>:<port>
                                     ws(s)://<host>?authToken=...
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
                      Overrides --brain. See examples/cloud-haiku.yaml.
  --web-addr <addr>   Bind the embedded web viewer at this address
                      (e.g. 127.0.0.1:8080). Empty = disabled.
                      Also configurable via the YAML config's
                      'web.addr' key; the flag wins when both set.
  --save-mode <mode>  Persistence cadence. Requires --db.
                        manual              user presses 's' (default)
                        interval:<dur>      periodic background save,
                                            e.g. interval:30s
                      Also configurable via YAML 'save.mode'; the
                      flag wins when both set.`)
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
	webAddr := fs.String("web-addr", "", "address to bind the optional web viewer (e.g. 127.0.0.1:8080)")
	saveMode := fs.String("save-mode", "", "persistence cadence: manual | interval:<duration> (e.g. interval:30s)")
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

	var (
		stepper      tui.Stepper
		simProvider  webui.SimProvider
		yamlWebAddr  string
		yamlSaveMode string
		attachOnSims []*sim.Sim // sims whose Observer the webui will subscribe to
		simForWorld  func(name string) *sim.Sim
	)
	switch {
	case *configPath != "":
		embedder, err := buildEmbedder(*embedderSpec)
		if err != nil {
			return fmt.Errorf("build embedder: %w", err)
		}
		u, cfg, err := loadUniverse(ctx, *configPath, embedder, st)
		if err != nil {
			return err
		}
		defer func() { _ = u.Close() }()
		// Override the loaded world with the universe's focused
		// world so save/load and the TUI agree.
		w = u.Focused().World
		stepper = newUniverseAdapter(u)
		simProvider = func() *sim.Sim { return u.Focused() }
		attachOnSims = u.Sims()
		simForWorld = simByName(u.Sims())
		// Restore each world's root agent memory from the store.
		if dbStore, ok := st.(*store.Store); ok {
			for _, s := range u.Sims() {
				restoreRootMemory(ctx, dbStore, s)
			}
		}
		if cfg != nil {
			yamlWebAddr = cfg.Web.Addr
			yamlSaveMode = cfg.Save.Mode
		}
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
		simProvider = func() *sim.Sim { return s }
		attachOnSims = []*sim.Sim{s}
		simForWorld = simByName([]*sim.Sim{s})
		if dbStore, ok := st.(*store.Store); ok {
			restoreRootMemory(ctx, dbStore, s)
		}
	}

	// Wrap st with a full-saver so manual 's' and autosave persist
	// memory alongside world events.
	if dbStore, ok := st.(*store.Store); ok && simForWorld != nil {
		st = &fullSaver{store: dbStore, simForWorld: simForWorld}
	}

	// Resolve the web address: CLI flag wins, then YAML config.
	resolvedWebAddr := *webAddr
	if resolvedWebAddr == "" {
		resolvedWebAddr = yamlWebAddr
	}
	if resolvedWebAddr != "" && simProvider != nil {
		stop, err := startWebUI(ctx, resolvedWebAddr, simProvider, attachOnSims, stderr)
		if err != nil {
			return err
		}
		defer stop()
	}

	// Resolve the save mode: CLI flag wins, then YAML config.
	resolvedSaveMode := *saveMode
	if resolvedSaveMode == "" {
		resolvedSaveMode = yamlSaveMode
	}
	mode, err := store.ParseSaveMode(resolvedSaveMode)
	if err != nil {
		return err
	}
	if mode.Kind == "interval" {
		concreteStore, ok := st.(store.Saver)
		if !ok || concreteStore == nil {
			return fmt.Errorf("save-mode interval requires --db; no store attached")
		}
		go func() {
			err := store.RunAutosave(ctx, concreteStore, w, mode.Interval, func(saveErr error) {
				fmt.Fprintln(stderr, "autosave:", saveErr)
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "autosave:", err)
			}
		}()
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
	return nil
}

// startWebUI launches the embedded HTTP viewer and attaches its
// broker to each sim's Observer. Returns a stop func the caller
// must invoke before exiting so the listener releases its port.
func startWebUI(ctx context.Context, addr string, provider webui.SimProvider, attachOn []*sim.Sim, stderr io.Writer) (func(), error) {
	logger := log.New(stderr, "", log.LstdFlags)
	server, err := webui.New(webui.Options{
		Addr:     addr,
		Provider: provider,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("web ui: %w", err)
	}
	for _, s := range attachOn {
		s.Observer = chainObservers(s.Observer, server.Publish)
	}
	serveErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()
	// Surface a synchronous bind error quickly.
	select {
	case err := <-serveErr:
		if err != nil {
			return nil, fmt.Errorf("web ui: %w", err)
		}
	case <-time.After(100 * time.Millisecond):
	}
	return func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
		// Detach Publish from every sim we attached to so a slow
		// shutdown can't dispatch into a now-closed broker.
		for _, s := range attachOn {
			s.Observer = nil
		}
	}, nil
}

// chainObservers composes two Sim.Observer-shaped callbacks. nil
// callbacks are silently dropped.
func chainObservers(a, b func(sim.StepResult)) func(sim.StepResult) {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(r sim.StepResult) {
		a(r)
		b(r)
	}
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

// fullSaver implements tui.Storer by persisting both world events
// and root-agent memory in a single Save call. It's what the TUI's
// manual 's' binding and the autosave loop invoke.
type fullSaver struct {
	store       *store.Store
	simForWorld func(name string) *sim.Sim
}

func (f *fullSaver) Save(ctx context.Context, w *world.World) error {
	if err := f.store.Save(ctx, w); err != nil {
		return err
	}
	s := f.simForWorld(w.Name())
	if s == nil {
		return nil
	}
	root := s.Agent()
	if root == nil || root.Memory == nil {
		return nil
	}
	return f.store.SaveMemoryRecords(ctx, w.Name(), root.Memory.All())
}

// simByName returns a closure that finds the sim whose world.Name()
// matches the argument. Used by fullSaver to pair an incoming world
// snapshot with the right sim's memory.
func simByName(sims []*sim.Sim) func(string) *sim.Sim {
	return func(name string) *sim.Sim {
		for _, s := range sims {
			if s.World != nil && s.World.Name() == name {
				return s
			}
		}
		return nil
	}
}

// restoreRootMemory pulls the root agent's saved memory records out
// of the store and hands them to the freshly-built Stream. The
// AgentID baked into restored records may not match the new root's
// EntityID, but retrieval doesn't filter by AgentID so this is
// harmless and keeps continuity across sessions.
func restoreRootMemory(ctx context.Context, dbStore *store.Store, s *sim.Sim) {
	if s == nil || s.World == nil {
		return
	}
	root := s.Agent()
	if root == nil || root.Memory == nil {
		return
	}
	records, err := dbStore.LoadMemoryRecords(ctx, s.World.Name())
	if err != nil || len(records) == 0 {
		return
	}
	root.Memory.Restore(records)
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
			Drives:    ag.Drives.Clone(),
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
			Drives:    ag.Drives.Clone(),
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
// When st implements the world-loader contract, each configured
// world is restored from the store before the sim is wired so prior
// state survives a restart. Also returns the parsed Config so
// callers can read top-level settings (e.g. Web.Addr).
func loadUniverse(ctx context.Context, path string, embedder memory.Embedder, st tui.Storer) (*sim.Universe, *sim.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := sim.LoadYAML(f)
	if err != nil {
		return nil, nil, err
	}
	factory := func(_ context.Context, spec string) (brain.Brain, error) {
		return buildBrain(spec)
	}
	loader := storeWorldLoader(st)
	u, err := cfg.BuildUniverse(ctx, factory, embedder, loader)
	if err != nil {
		return nil, nil, err
	}
	return u, cfg, nil
}

// storeWorldLoader adapts a tui.Storer that also implements Load into
// a sim.WorldLoader. Missing worlds become a (nil, nil) result so
// BuildUniverse falls back to constructing a fresh empty world.
func storeWorldLoader(st tui.Storer) sim.WorldLoader {
	type worldLoader interface {
		Load(ctx context.Context, name string) (*world.World, error)
	}
	wl, ok := st.(worldLoader)
	if !ok || wl == nil {
		return nil
	}
	return func(ctx context.Context, name string) (*world.World, error) {
		w, err := wl.Load(ctx, name)
		if err == nil {
			return w, nil
		}
		if errors.Is(err, store.ErrWorldNotFound) {
			return nil, nil
		}
		return nil, err
	}
}

// cmdReplay opens a stored world's event log and walks it tick by
// tick. The TUI watches a passive Stepper that applies one event
// per tick interval; no brain is consulted.
func cmdReplay(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fiatlux replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "database DSN (required; accepts local files and libsql URLs)")
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

// stepSummary is the headless-mode output document.
type stepSummary struct {
	World              string         `json:"world"`
	Ticks              uint64         `json:"ticks"`
	Steps              int            `json:"steps"`
	StepsSkipped       int            `json:"steps_skipped"`
	EntitiesAlive      int            `json:"entities_alive"`
	RelationshipsAlive int            `json:"relationships_alive"`
	Agents             int            `json:"agents"`
	Events             int            `json:"events"`
	ToolErrors         int            `json:"tool_errors"`
	ToolCounts         map[string]int `json:"tool_counts"`
	ElapsedMS          int64          `json:"elapsed_ms"`
}

// cmdStep runs the sim headlessly for --count steps and prints a
// summary. No TUI, no /dev/tty required.
func cmdStep(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fiatlux step", flag.ContinueOnError)
	fs.SetOutput(stderr)
	count := fs.Int("count", 1, "number of Step calls to perform")
	worldName := fs.String("world", "kosmos", "name of the world to open or create")
	dbPath := fs.String("db", "", "database DSN (empty = in-memory)")
	brainSpec := fs.String("brain", "stub", "brain config; see 'fiatlux help'")
	tickDelay := fs.Duration("tick", 0, "delay between steps (default: no delay)")
	embedderSpec := fs.String("embedder", "zero", "memory embedder")
	importanceSpec := fs.String("importance", "heuristic", "memory importance scorer")
	reflectInterval := fs.Uint64("reflect-interval", 0, "ticks between reflection passes (0 = disable)")
	maxAgents := fs.Int("max-agents", 8, "cap on total agents per world")
	maxSpawnDepth := fs.Int("max-spawn-depth", 3, "cap on the spawn graph height")
	jsonOut := fs.Bool("json", false, "emit summary as JSON instead of human-readable text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *count <= 0 {
		return fmt.Errorf("step: --count must be > 0 (got %d)", *count)
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

	embedder, err := buildEmbedder(*embedderSpec)
	if err != nil {
		return fmt.Errorf("build embedder: %w", err)
	}
	s, err := buildSim(w, *brainSpec, embedder, *importanceSpec, *reflectInterval, *maxAgents, *maxSpawnDepth)
	if err != nil {
		return fmt.Errorf("build sim: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Restore root-agent memory from prior runs (if any) and wrap
	// the store with the full-saver so the final save persists
	// memory too.
	if dbStore, ok := st.(*store.Store); ok {
		restoreRootMemory(ctx, dbStore, s)
		st = &fullSaver{store: dbStore, simForWorld: simByName([]*sim.Sim{s})}
	}

	summary := stepSummary{ToolCounts: map[string]int{}}
	start := time.Now()
	for i := 0; i < *count; i++ {
		if ctx.Err() != nil {
			break
		}
		res, err := s.Step(ctx)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		summary.Steps++
		if res.Skipped {
			summary.StepsSkipped++
		}
		if res.ToolName != "" {
			summary.ToolCounts[res.ToolName]++
		}
		if res.ToolErr != nil {
			summary.ToolErrors++
		}
		if *tickDelay > 0 && i+1 < *count {
			select {
			case <-ctx.Done():
			case <-time.After(*tickDelay):
			}
		}
	}
	summary.ElapsedMS = time.Since(start).Milliseconds()
	summary.Ticks = uint64(w.Tick())
	summary.EntitiesAlive = w.EntityCount()
	summary.RelationshipsAlive = w.RelationshipCount()
	summary.Agents = len(s.Agents())
	summary.Events = len(w.Events())
	summary.World = w.Name()

	// Save before reporting if a store is attached - users running
	// headless probably want their work persisted.
	if st != nil {
		if err := st.Save(ctx, w); err != nil {
			return fmt.Errorf("save: %w", err)
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	return writeHumanSummary(stdout, summary)
}

// stepper is the minimal interface implemented by both *sim.Sim and
// *sim.Universe. cmdServe loops over it without depending on the
// tui.Stepper adapter pair.
type stepper interface {
	Step(ctx context.Context) (sim.StepResult, error)
}

// cmdServe runs the sim headlessly with the embedded web viewer
// attached. No TUI, no /dev/tty - this is the deployment-friendly
// entrypoint that fits in a container behind a Cloudflare Tunnel.
func cmdServe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fiatlux serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	worldName := fs.String("world", "kosmos", "name of the world to open or create")
	dbPath := fs.String("db", "", "database DSN (empty = in-memory, no persistence)")
	brainSpec := fs.String("brain", "stub", "brain config; see 'fiatlux help'")
	tickInterval := fs.Duration("tick", tui.DefaultTickInterval, "sim tick interval")
	embedderSpec := fs.String("embedder", "zero", "memory embedder (zero | hash | ollama[:model] | openai[:model])")
	importanceSpec := fs.String("importance", "heuristic", "memory importance scorer (heuristic | llm)")
	reflectInterval := fs.Uint64("reflect-interval", 20, "ticks between reflection passes (0 = disable)")
	maxAgents := fs.Int("max-agents", 8, "cap on total agents per world")
	maxSpawnDepth := fs.Int("max-spawn-depth", 3, "cap on the spawn graph height")
	configPath := fs.String("config", "", "YAML config for a multi-world universe (overrides --brain)")
	webAddr := fs.String("web-addr", ":8080", "address to bind the embedded web viewer (e.g. :8080 or 127.0.0.1:8080)")
	saveMode := fs.String("save-mode", "", "persistence cadence: manual | interval:<duration> (e.g. interval:30s)")
	_ = stdout
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

	var (
		loop         stepper
		simProvider  webui.SimProvider
		yamlWebAddr  string
		yamlSaveMode string
		attachOnSims []*sim.Sim
		simForWorld  func(name string) *sim.Sim
	)
	switch {
	case *configPath != "":
		embedder, err := buildEmbedder(*embedderSpec)
		if err != nil {
			return fmt.Errorf("build embedder: %w", err)
		}
		u, cfg, err := loadUniverse(ctx, *configPath, embedder, st)
		if err != nil {
			return err
		}
		defer func() { _ = u.Close() }()
		w = u.Focused().World
		loop = u
		simProvider = func() *sim.Sim { return u.Focused() }
		attachOnSims = u.Sims()
		simForWorld = simByName(u.Sims())
		if dbStore, ok := st.(*store.Store); ok {
			for _, s := range u.Sims() {
				restoreRootMemory(ctx, dbStore, s)
			}
		}
		if cfg != nil {
			yamlWebAddr = cfg.Web.Addr
			yamlSaveMode = cfg.Save.Mode
		}
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
		loop = s
		simProvider = func() *sim.Sim { return s }
		attachOnSims = []*sim.Sim{s}
		simForWorld = simByName([]*sim.Sim{s})
		if dbStore, ok := st.(*store.Store); ok {
			restoreRootMemory(ctx, dbStore, s)
		}
	default:
		return errors.New("serve: --brain=none is not supported (web viewer needs a sim to render)")
	}

	if dbStore, ok := st.(*store.Store); ok && simForWorld != nil {
		st = &fullSaver{store: dbStore, simForWorld: simForWorld}
	}

	resolvedWebAddr := *webAddr
	if resolvedWebAddr == "" {
		resolvedWebAddr = yamlWebAddr
	}
	if resolvedWebAddr == "" {
		return errors.New("serve: --web-addr is required (set the flag or YAML 'web.addr')")
	}
	stop, err := startWebUI(ctx, resolvedWebAddr, simProvider, attachOnSims, stderr)
	if err != nil {
		return err
	}
	defer stop()

	resolvedSaveMode := *saveMode
	if resolvedSaveMode == "" {
		resolvedSaveMode = yamlSaveMode
	}
	mode, err := store.ParseSaveMode(resolvedSaveMode)
	if err != nil {
		return err
	}
	if mode.Kind == "interval" {
		concreteStore, ok := st.(store.Saver)
		if !ok || concreteStore == nil {
			return fmt.Errorf("save-mode interval requires --db; no store attached")
		}
		go func() {
			err := store.RunAutosave(ctx, concreteStore, w, mode.Interval, func(saveErr error) {
				fmt.Fprintln(stderr, "autosave:", saveErr)
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "autosave:", err)
			}
		}()
	}

	ticker := time.NewTicker(*tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Best-effort final save so the work that's accumulated
			// since the last autosave persists. Use a fresh context
			// because the signal context is already done.
			if st != nil {
				saveCtx, cancelSave := context.WithTimeout(context.Background(), 5*time.Second)
				if err := st.Save(saveCtx, w); err != nil {
					fmt.Fprintln(stderr, "final save:", err)
				}
				cancelSave()
			}
			return nil
		case <-ticker.C:
			if _, err := loop.Step(ctx); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "step:", err)
			}
		}
	}
}

func writeHumanSummary(w io.Writer, s stepSummary) error {
	lines := []string{
		fmt.Sprintf("world:                %s", s.World),
		fmt.Sprintf("ticks:                %d", s.Ticks),
		fmt.Sprintf("steps:                %d", s.Steps),
		fmt.Sprintf("steps_skipped:        %d", s.StepsSkipped),
		fmt.Sprintf("entities_alive:       %d", s.EntitiesAlive),
		fmt.Sprintf("relationships_alive:  %d", s.RelationshipsAlive),
		fmt.Sprintf("agents:               %d", s.Agents),
		fmt.Sprintf("events:               %d", s.Events),
		fmt.Sprintf("tool_errors:          %d", s.ToolErrors),
		fmt.Sprintf("elapsed_ms:           %d", s.ElapsedMS),
	}
	if len(s.ToolCounts) > 0 {
		lines = append(lines, "tool_counts:")
		names := make([]string, 0, len(s.ToolCounts))
		for n := range s.ToolCounts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			lines = append(lines, fmt.Sprintf("  %-16s %d", n+":", s.ToolCounts[n]))
		}
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
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

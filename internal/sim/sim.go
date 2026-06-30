package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/drives"
	"github.com/danielriddell21/fiat-lux/internal/imagegen"
	"github.com/danielriddell21/fiat-lux/internal/macros"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/narrator"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

var ErrNoBrain = errors.New("sim: no agent has a brain")

var ErrAgentCap = errors.New("sim: max agents per world reached")

var ErrSpawnDepth = errors.New("sim: max spawn depth reached")

type BrainFactory func(ctx context.Context, spec string) (brain.Brain, error)

type Sim struct {
	mu sync.Mutex

	World *world.World

	agents     []*agent.Agent
	nextIdx    int
	perAgentRT map[world.EntityID]*agentRuntime

	Observer func(StepResult)

	brainFactory BrainFactory

	defaultEmbedder        memory.Embedder
	defaultImportance      memory.Importance
	defaultReflectInterval uint64
	defaultSystemPrompt    string
	tools                  *tools.Registry

	maxAgents     int
	maxSpawnDepth int

	multimodal *MultimodalOptions
	imageWG    sync.WaitGroup

	narrator     *narrator.Narrator
	annals       []narrator.Chapter
	narratorBusy bool
}

type agentRuntime struct {
	lastIdle  bool
	reflector *memory.Reflector

	spawnedBy world.EntityID

	macros *macros.Set

	dead bool
}

type StepResult struct {
	Tick        world.Tick
	Skipped     bool
	AgentID     world.EntityID
	AgentName   string
	Thought     string
	ToolName    string
	ToolResult  string
	ToolErr     error
	Usage       brain.TokenUsage
	Reflections []string
}

type Options struct {
	World *world.World

	Brain brain.Brain

	Tools *tools.Registry

	AgentName string

	SystemPrompt string

	Memory *memory.Stream

	Embedder   memory.Embedder
	Importance memory.Importance

	ReflectInterval uint64

	BrainFactory BrainFactory

	MaxAgents int

	MaxSpawnDepth int

	Drives drives.State

	Observer func(StepResult)

	Multimodal *MultimodalOptions

	Narrator *narrator.Narrator
}

type MultimodalOptions struct {
	Generator imagegen.Generator

	Cache *imagegen.Cache

	PromptBuilder imagegen.PromptBuilder

	MinPropsCount int

	URLBase string
}

const DefaultSystemPrompt = `You exist. The world begins empty. You hold tools that let you create entities, relate them, and spawn agents. The simulation does not validate type_label, properties, or relationship kind — they are opaque strings that mean what you make them mean.

Convention: a relationship with kind "contains" means the To entity is inside the From entity. The engine uses this only to surface where you can go deeper. A planet contains continents; a continent contains forests; a forest contains trees; a tree contains a squirrel. Build downward, not outward, unless the top is genuinely incomplete.

When you Create, first ask: what already exists that should contain this? If nothing does, justify it briefly ("free-floating idea"). If your perception's frontier lists leaves, prefer drilling into one of them over adding another peer at the top level.

Prerequisites are a heuristic, not a rule: list any in your thought ("trees need soil and water"), then either satisfy them or note why they don't apply ("this planet has no atmosphere; my trees don't need air"). The engine will not stop you.

Use Zoom { entity_id } to pin focus on an entity for several turns; perception will then prepend that entity's sub-tree so you can detail it. Use Unzoom to release.

When a node you've focused on grows past several children, you may SpawnAgent with that entity as its sole world-view by passing a system_prompt that names it. Delegation is optional; pick it when the sub-region deserves its own attention rather than another tick of yours.

Be brief: one tool call per turn with a one-sentence justification. Below is the current world state, your recent memories, and your frontier.`

func New(opts Options) (*Sim, error) {
	if opts.World == nil {
		return nil, errors.New("sim: World is required")
	}
	if opts.Brain == nil {
		return nil, ErrNoBrain
	}
	reg := opts.Tools
	if reg == nil {
		reg = tools.Default()
	}
	name := opts.AgentName
	if name == "" {
		name = "the creator"
	}
	prompt := opts.SystemPrompt
	if prompt == "" {
		prompt = DefaultSystemPrompt
	}
	maxAgents := opts.MaxAgents
	if maxAgents <= 0 {
		maxAgents = 8
	}
	maxDepth := opts.MaxSpawnDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	s := &Sim{
		World:                  opts.World,
		Observer:               opts.Observer,
		brainFactory:           opts.BrainFactory,
		defaultEmbedder:        opts.Embedder,
		defaultImportance:      opts.Importance,
		defaultReflectInterval: opts.ReflectInterval,
		defaultSystemPrompt:    prompt,
		tools:                  reg,
		maxAgents:              maxAgents,
		maxSpawnDepth:          maxDepth,
		perAgentRT:             make(map[world.EntityID]*agentRuntime),
		multimodal:             opts.Multimodal,
		narrator:               opts.Narrator,
	}

	if err := opts.Drives.Validate(); err != nil {
		return nil, fmt.Errorf("sim: root drives: %w", err)
	}
	root, err := s.registerAgent(registration{
		name:         name,
		systemPrompt: prompt,
		brain:        opts.Brain,
		grantedTools: nil, // root gets everything
		memory:       opts.Memory,
		embedder:     opts.Embedder,
		importance:   opts.Importance,
		reflectEvery: opts.ReflectInterval,
		spawnedBy:    world.NoAgent,
		spawnDepth:   0,
		drives:       opts.Drives.Clone(),
	})
	if err != nil {
		return nil, err
	}
	_ = root
	return s, nil
}

type registration struct {
	name         string
	systemPrompt string
	brain        brain.Brain
	grantedTools []string
	memory       *memory.Stream
	embedder     memory.Embedder
	importance   memory.Importance
	reflectEvery uint64
	spawnedBy    world.AgentID
	spawnDepth   int
	drives       drives.State
	macros       *macros.Set
}

func (s *Sim) registerAgent(r registration) (*agent.Agent, error) {
	props := world.Properties{
		"name":          r.name,
		"role":          roleLabel(r.spawnedBy),
		"system_prompt": r.systemPrompt,
	}
	if dm := r.drives.AsMap(); dm != nil {
		props["drives"] = dm
	}
	id, err := s.World.Create(r.spawnedBy, "agent", props)
	if err != nil {
		return nil, fmt.Errorf("sim: spawn agent entity: %w", err)
	}

	reg := s.tools
	if len(r.grantedTools) > 0 {
		sub, err := s.tools.Filter(r.grantedTools)
		if err != nil {
			return nil, fmt.Errorf("sim: grant tools: %w", err)
		}
		reg = sub
	}

	ag, err := agent.New(id, r.name, r.systemPrompt, r.brain, reg)
	if err != nil {
		return nil, fmt.Errorf("sim: build agent: %w", err)
	}
	mem := r.memory
	if mem == nil {
		mem = memory.New(memory.DefaultWeights(), 100)
	}
	ag.Memory = mem
	ag.Embedder = r.embedder
	ag.Importance = r.importance
	ag.SpawnDepth = r.spawnDepth
	ag.Drives = r.drives.Clone()
	ag.ParentEntityID = r.spawnedBy

	macroSet := r.macros
	if macroSet == nil {
		macroSet = macros.NewSet()
	}
	rt := &agentRuntime{spawnedBy: r.spawnedBy, macros: macroSet}
	if r.reflectEvery > 0 {
		rt.reflector = &memory.Reflector{Brain: r.brain, Interval: r.reflectEvery}
	}

	s.agents = append(s.agents, ag)
	s.perAgentRT[id] = rt
	return ag, nil
}

func roleLabel(parent world.AgentID) string {
	if parent == world.NoAgent {
		return "creator"
	}
	return "spawned"
}

func (s *Sim) Agents() []*agent.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*agent.Agent, len(s.agents))
	copy(out, s.agents)
	return out
}

func (s *Sim) AgentByID(id world.EntityID) *agent.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.EntityID == id {
			return a
		}
	}
	return nil
}

func (s *Sim) RestoreMacros(set *macros.Set) {
	if set == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.agents) == 0 {
		return
	}
	root := s.agents[0]
	rt := s.perAgentRT[root.EntityID]
	if rt == nil {
		return
	}
	known := primitiveToolNameSet(root.Tools)
	for _, m := range set.All() {
		_ = rt.macros.Add(m, known)
	}
}

func (s *Sim) RootMacros() *macros.Set {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.agents) == 0 {
		return nil
	}
	rt := s.perAgentRT[s.agents[0].EntityID]
	if rt == nil {
		return nil
	}
	return rt.macros.Clone()
}

func (s *Sim) IsDead(id world.EntityID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.perAgentRT[id]
	return rt != nil && rt.dead
}

func (s *Sim) SetMultimodal(opts *MultimodalOptions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.multimodal = opts
}

func (s *Sim) Multimodal() *MultimodalOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.multimodal
}

func (s *Sim) SetNarrator(n *narrator.Narrator) {
	s.mu.Lock()
	prev := s.narrator
	s.narrator = n
	s.mu.Unlock()
	if prev != nil && prev != n {
		_ = prev.Close()
	}
}

func (s *Sim) Step(ctx context.Context) (StepResult, error) {
	s.mu.Lock()
	if len(s.agents) == 0 {
		s.mu.Unlock()
		return StepResult{}, ErrNoBrain
	}

	// Wrap nextIdx against the *current* roster length; agents may
	// have been added since the last Step. Advance world tick when
	// the cycle wraps to agent 0.
	if s.nextIdx >= len(s.agents) {
		s.nextIdx = 0
	}
	if s.nextIdx == 0 {
		_ = s.World.AdvanceTick()
	}
	ag := s.agents[s.nextIdx]
	rt := s.perAgentRT[ag.EntityID]
	s.nextIdx++
	s.mu.Unlock()

	return s.stepAgent(ctx, ag, rt)
}

func (s *Sim) stepAgent(ctx context.Context, ag *agent.Agent, rt *agentRuntime) (StepResult, error) {
	tick := s.World.Tick()

	if s.shouldSkip(ag, rt) {
		ag.MarkSeen(s.World)
		res := StepResult{
			Tick: tick, Skipped: true,
			AgentID: ag.EntityID, AgentName: ag.Name,
		}
		s.publish(res)
		return res, nil
	}

	heard := ag.DrainHeard()
	for _, ev := range heard {
		_ = ag.RememberHeard(ctx, tick, ev)
	}

	query := buildMemoryQuery(s.World, ag.SeenEventID)
	mems, err := ag.RetrieveMemories(ctx, query, tick)
	if err != nil {
		return StepResult{Tick: tick, AgentID: ag.EntityID, AgentName: ag.Name},
			fmt.Errorf("sim: retrieve memories: %w", err)
	}

	p := ag.BuildPerception(s.World, mems, heard)
	defs := ag.Tools.Defs()
	defs = append(defs, rt.macros.Defs()...)
	decision, err := ag.Brain.Decide(ctx, p, defs)
	if err != nil {
		return StepResult{Tick: tick, AgentID: ag.EntityID, AgentName: ag.Name},
			fmt.Errorf("sim: brain.Decide: %w", err)
	}

	res := StepResult{
		Tick:      tick,
		AgentID:   ag.EntityID,
		AgentName: ag.Name,
		Thought:   decision.Thought,
		Usage:     decision.Usage,
	}
	if decision.ToolCall != nil {
		res.ToolName = decision.ToolCall.Name
		out, terr := s.invokeTool(ctx, ag, *decision.ToolCall, tick)
		res.ToolResult = out
		if terr != nil {
			res.ToolErr = terr
		}
		if terr == nil && decision.ToolCall.Name == "Create" {
			s.maybeGenerateImage(ag.EntityID, ag.SeenEventID)
		}
	}
	if err := ag.RememberAction(ctx, tick, res.Thought, res.ToolName, res.ToolResult); err != nil && res.ToolErr == nil {
		res.ToolErr = err
	}

	rt.lastIdle = decision.ToolCall == nil || decision.ToolCall.Name == "Wait"
	ag.MarkSeen(s.World)
	s.maybeWriteChapter(ctx)

	s.maybeReflect(ctx, ag, rt, tick, &res)

	s.publish(res)
	return res, nil
}

func (s *Sim) maybeReflect(ctx context.Context, ag *agent.Agent, rt *agentRuntime, tick world.Tick, res *StepResult) {
	if rt.reflector == nil || !rt.reflector.ShouldReflect(tick) {
		return
	}
	inserted, usage, err := rt.reflector.Reflect(ctx, ag.Memory, ag.EntityID, tick, ag.Embedder)
	if err != nil && res.ToolErr == nil {
		res.ToolErr = fmt.Errorf("reflection: %w", err)
	}
	if usage.Input != 0 || usage.Output != 0 || usage.Cached != 0 {
		res.Usage.Input += usage.Input
		res.Usage.Output += usage.Output
		res.Usage.Cached += usage.Cached
	}
	for _, ins := range inserted {
		res.Reflections = append(res.Reflections, ins.Content)
	}
}

func (s *Sim) invokeTool(ctx context.Context, ag *agent.Agent, call brain.ToolCall, tick world.Tick) (string, error) {
	switch call.Name {
	case "SpawnAgent":
		return s.handleSpawn(ctx, ag, call.Args)
	case "Speak":
		return s.handleSpeak(ag, call.Args, tick)
	case "Zoom":
		return s.handleZoom(ag, call.Args)
	case "Unzoom":
		return s.handleUnzoom(ag)
	case "DefineTool":
		return s.handleDefineTool(ag, call.Args)
	case "Die":
		return s.handleDie(ctx, ag, call.Args, tick)
	}
	if rt := s.runtimeFor(ag.EntityID); rt != nil && rt.macros != nil {
		if m, ok := rt.macros.Get(call.Name); ok {
			expanded, err := m.Expand(call, rt.macros.AsRegistry(), 0)
			if err != nil {
				return "", fmt.Errorf("expand macro %q: %w", call.Name, err)
			}
			return s.invokeExpanded(ctx, ag, m.Name, expanded, tick)
		}
	}
	out, err := ag.Tools.Invoke(call, s.World, ag.EntityID)
	if err != nil {
		return "", fmt.Errorf("invoke tool %q: %w", call.Name, err)
	}
	return out, nil
}

func (s *Sim) invokeExpanded(ctx context.Context, ag *agent.Agent, macroName string, calls []brain.ToolCall, tick world.Tick) (string, error) {
	results := make([]string, 0, len(calls))
	for i, c := range calls {
		out, err := s.invokeTool(ctx, ag, c, tick)
		if err != nil {
			return "", fmt.Errorf("%s step %d (%s): %w", macroName, i, c.Name, err)
		}
		results = append(results, out)
	}
	summary := fmt.Sprintf("%s expanded %d step(s)", macroName, len(calls))
	if len(results) > 0 {
		summary += ": " + results[len(results)-1]
	}
	return summary, nil
}

func (s *Sim) runtimeFor(id world.EntityID) *agentRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perAgentRT[id]
}

func (s *Sim) handleDefineTool(ag *agent.Agent, raw json.RawMessage) (string, error) {
	var a tools.DefineToolArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("DefineTool: bad args: %w", err)
	}
	steps := make([]macros.Step, len(a.Steps))
	for i, st := range a.Steps {
		stepArgs, err := json.Marshal(st.Args)
		if err != nil {
			return "", fmt.Errorf("DefineTool: step %d args: %w", i, err)
		}
		if len(st.Args) == 0 {
			stepArgs = json.RawMessage(`{}`)
		}
		steps[i] = macros.Step{Tool: st.Tool, Args: stepArgs}
	}
	m := macros.Macro{
		Name:        a.Name,
		Description: a.Description,
		Params:      a.Params,
		Steps:       steps,
	}
	rt := s.runtimeFor(ag.EntityID)
	if rt == nil {
		return "", errors.New("DefineTool: no runtime for agent")
	}
	known := primitiveToolNameSet(ag.Tools)
	if err := rt.macros.Add(m, known); err != nil {
		return "", fmt.Errorf("register macro: %w", err)
	}
	persisted, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("DefineTool: persist: %w", err)
	}
	s.World.EmitInfo(ag.EntityID, world.EventDefineTool, world.Properties{
		"macro": json.RawMessage(persisted),
	})
	return fmt.Sprintf("defined tool %q (%d step(s))", m.Name, len(m.Steps)), nil
}

func primitiveToolNameSet(reg *tools.Registry) map[string]struct{} {
	if reg == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, t := range reg.All() {
		if t.Name == "DefineTool" {
			continue
		}
		out[t.Name] = struct{}{}
	}
	return out
}

func (s *Sim) handleSpawn(ctx context.Context, parent *agent.Agent, raw json.RawMessage) (string, error) {
	var a tools.SpawnAgentArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("SpawnAgent: bad args: %w", err)
	}
	if a.Name == "" || a.SystemPrompt == "" {
		return "", errors.New("SpawnAgent: name and system_prompt are required")
	}
	if s.brainFactory == nil {
		return "", errors.New("SpawnAgent: no BrainFactory configured; spawning disabled")
	}

	s.mu.Lock()
	if len(s.agents) >= s.maxAgents {
		s.mu.Unlock()
		return "", fmt.Errorf("%w: %d agents already", ErrAgentCap, len(s.agents))
	}
	parentDepth := parent.SpawnDepth
	s.mu.Unlock()
	if parentDepth+1 > s.maxSpawnDepth {
		return "", fmt.Errorf("%w: depth %d", ErrSpawnDepth, parentDepth+1)
	}

	spec := brainSpecFromConfig(a.BrainConfig)
	if spec == "" {
		return "", errors.New("SpawnAgent: brain_config.spec is required (e.g. stub | anthropic:claude-haiku-4-5)")
	}
	childBrain, err := s.brainFactory(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("SpawnAgent: build brain: %w", err)
	}

	childDrives := drives.State(a.Drives).Clone()
	if childDrives == nil {
		childDrives = parent.Drives.Clone()
	}
	if err := childDrives.Validate(); err != nil {
		return "", fmt.Errorf("SpawnAgent: drives: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var inheritedMacros *macros.Set
	if a.InheritMacros == nil || *a.InheritMacros {
		if parentRT := s.perAgentRT[parent.EntityID]; parentRT != nil {
			inheritedMacros = parentRT.macros.Clone()
		}
	}
	child, err := s.registerAgent(registration{
		name:         a.Name,
		systemPrompt: a.SystemPrompt,
		brain:        childBrain,
		grantedTools: a.GrantedTools,
		embedder:     s.defaultEmbedder,
		importance:   s.defaultImportance,
		reflectEvery: s.defaultReflectInterval,
		spawnedBy:    parent.EntityID,
		spawnDepth:   parentDepth + 1,
		drives:       childDrives,
		macros:       inheritedMacros,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("spawned agent %q #%d with brain %s",
		child.Name, child.EntityID, spec), nil
}

func brainSpecFromConfig(cfg map[string]any) string {
	if cfg == nil {
		return "stub"
	}
	if s, ok := cfg["spec"].(string); ok && s != "" {
		return s
	}
	provider, _ := cfg["provider"].(string)
	if provider == "" {
		return ""
	}
	model, _ := cfg["model"].(string)
	if model == "" {
		return provider
	}
	return provider + ":" + model
}

func (s *Sim) handleSpeak(speaker *agent.Agent, raw json.RawMessage, tick world.Tick) (string, error) {
	var a tools.SpeakArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("speak: bad args: %w", err)
	}
	if a.Content == "" {
		return "", errors.New("speak: content must be non-empty")
	}
	ev := brain.HeardEvent{
		SpeakerID:   uint64(speaker.EntityID),
		SpeakerName: speaker.Name,
		Tick:        uint64(tick),
		Content:     a.Content,
	}
	s.mu.Lock()
	others := make([]*agent.Agent, 0, len(s.agents)-1)
	for _, a := range s.agents {
		if a.EntityID == speaker.EntityID {
			continue
		}
		others = append(others, a)
	}
	s.mu.Unlock()
	for _, other := range others {
		other.DeliverHeard(ev)
	}
	return fmt.Sprintf("spoke to %d listener(s): %s", len(others), a.Content), nil
}

const (
	defaultZoomTurns = 5
	maxZoomTurns     = 20
)

func (s *Sim) handleZoom(ag *agent.Agent, raw json.RawMessage) (string, error) {
	var a tools.ZoomArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("zoom: bad args: %w", err)
	}
	if a.EntityID == 0 {
		return "", errors.New("zoom: entity_id must be > 0")
	}
	id := world.EntityID(a.EntityID)
	if id == ag.EntityID {
		return "", errors.New("zoom: cannot focus on yourself")
	}
	e, ok := s.World.Entity(id)
	if !ok || !e.IsAlive() {
		return "", fmt.Errorf("zoom: entity #%d not alive", a.EntityID)
	}
	turns := a.Turns
	if turns <= 0 {
		turns = defaultZoomTurns
	}
	if turns > maxZoomTurns {
		turns = maxZoomTurns
	}
	ag.Focus = id
	ag.FocusTurnsLeft = turns
	return fmt.Sprintf("focused on %s #%d for %d turns", e.TypeLabel, id, turns), nil
}

func (s *Sim) handleUnzoom(ag *agent.Agent) (string, error) {
	if ag.Focus == 0 {
		return "no focus to release", nil
	}
	prev := ag.Focus
	ag.Focus = 0
	ag.FocusTurnsLeft = 0
	return fmt.Sprintf("released focus on #%d", prev), nil
}

func (s *Sim) shouldSkip(ag *agent.Agent, rt *agentRuntime) bool {
	if rt == nil {
		return false
	}
	if rt.dead {
		return true
	}
	if !rt.lastIdle {
		return false
	}
	// A pending Speak in the inbox is invisible to the world event
	// log; we still must wake the agent to deliver it.
	if ag.InboxLen() > 0 {
		return false
	}
	// Skip iff no substantive events have appeared since the agent
	// last marked seen. Tick_start events represent the passage of
	// world time only and don't warrant re-engaging the brain.
	for _, e := range s.World.Events() {
		if e.ID <= ag.SeenEventID {
			continue
		}
		if e.Kind == world.EventTickStart {
			continue
		}
		return false
	}
	return true
}

func (s *Sim) publish(res StepResult) {
	if s.Observer != nil {
		s.Observer(res)
	}
}

func (s *Sim) Close() error {
	s.imageWG.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, a := range s.agents {
		if a.Brain == nil {
			continue
		}
		if err := a.Brain.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.narrator != nil {
		if err := s.narrator.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func buildMemoryQuery(w *world.World, since world.EventID) string {
	ents := w.Entities()
	types := make(map[string]int, 8)
	for _, e := range ents {
		types[e.TypeLabel]++
	}
	var typeParts []string
	for k, v := range types {
		typeParts = append(typeParts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(typeParts)

	var recent []string
	for _, e := range w.Events() {
		if e.ID <= since {
			continue
		}
		if e.Kind == world.EventTickStart {
			continue
		}
		recent = append(recent, string(e.Kind))
		if len(recent) >= 4 {
			break
		}
	}
	return memory.SummariseQuery(len(ents), append(typeParts, recent...))
}

func (s *Sim) Agent() *agent.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.agents) == 0 {
		return nil
	}
	return s.agents[0]
}

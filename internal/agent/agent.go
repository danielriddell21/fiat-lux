package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Tunables for the containment-aware perception. The frontier and
// spawn-suggestion logic compute these every tick; keep the caps
// small enough that the brain's prompt stays bounded on deep worlds.
const (
	frontierLeafCap              = 12
	focusSubtreeCap              = 64
	frontierChildSpawnThreshold  = 6
	suggestionCooldownTicks      = 10
	suggestionMemoryHorizonTicks = 100
	maxSuggestionsPerTick        = 3
)

// Agent is the runtime brain attached to an agent-entity. It owns a
// pointer to its Brain, the tool subset its creator granted it, and
// its private memory stream.
type Agent struct {
	// EntityID is the agent's own entry in the world registry.
	EntityID world.EntityID

	// Name is the agent's display name; pulled from its entity's
	// properties at spawn time.
	Name string

	// SystemPrompt is the static instruction prepended to every
	// brain call.
	SystemPrompt string

	// Brain is the LLM-shaped decision-maker.
	Brain brain.Brain

	// Tools is the registry the agent may act through. May be a
	// subset of the global catalogue per the creator's grant.
	Tools *tools.Registry

	// Memory is the Smallville-style memory stream. Records are
	// appended after every step; top-K are retrieved into the
	// next perception.
	Memory *memory.Stream

	// Embedder hashes text into embedding vectors for relevance
	// retrieval. May be nil (zero-vector fallback).
	Embedder memory.Embedder

	// Importance scorer overrides the heuristic default. May be
	// nil; memory.Add falls back to memory.HeuristicScorer.
	Importance memory.Importance

	// RetrieveK is how many records are pulled into each
	// perception. Defaults to 6 when zero.
	RetrieveK int

	// SeenEventID tracks the highest event ID this agent has
	// perceived. The next perception's RecentEvents include only
	// events with ID > SeenEventID. Updated by the sim after each
	// step.
	SeenEventID world.EventID

	// SpawnDepth is the number of ancestors between this agent and
	// the root creator. Zero for the root agent; one for its direct
	// children; etc. Used to enforce MaxSpawnDepth.
	SpawnDepth int

	// Focus is the EntityID the agent has pinned for drill-down via
	// the Zoom tool, or zero when no focus is set. Cleared when the
	// focused entity is destroyed or FocusTurnsLeft reaches zero.
	Focus world.EntityID

	// FocusTurnsLeft is the number of upcoming turns Focus remains
	// pinned. Decremented at the end of each BuildPerception call;
	// when it hits zero, Focus is cleared.
	FocusTurnsLeft int

	// suggestionsLastTick is keyed by EntityID and holds the tick at
	// which a SpawnSuggestion was last emitted for that entity, used
	// to dedupe nudges across consecutive BuildPerception calls.
	suggestionsLastTick map[world.EntityID]uint64

	// inbox holds Heard events delivered by Speak from other
	// agents in the same world. Drained into Perception.Heard by
	// the sim on each step.
	inbox   []brain.HeardEvent
	inboxMu sync.Mutex
}

// New constructs an Agent. brain and tools must be non-nil.
func New(entityID world.EntityID, name, systemPrompt string, br brain.Brain, reg *tools.Registry) (*Agent, error) {
	if br == nil {
		return nil, fmt.Errorf("agent: brain is required")
	}
	if reg == nil {
		return nil, fmt.Errorf("agent: tools registry is required")
	}
	return &Agent{
		EntityID:     entityID,
		Name:         name,
		SystemPrompt: systemPrompt,
		Brain:        br,
		Tools:        reg,
		RetrieveK:    6,
	}, nil
}

// RememberAction records a memory pair for the just-completed step:
// the agent's tool choice and the tool's result. Either may be
// empty (e.g. the brain returned no tool call). Returns the IDs
// inserted; errors are logged via the returned error so the sim
// can decide whether to surface them.
func (a *Agent) RememberAction(ctx context.Context, tick world.Tick, thought, toolName, toolResult string) error {
	if a.Memory == nil {
		return nil
	}
	opts := memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance}
	if thought != "" {
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindThought, thought, tick, opts); err != nil {
			return fmt.Errorf("agent: remember thought: %w", err)
		}
	}
	if toolName != "" {
		actionContent := "I chose " + toolName
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindAction, actionContent, tick, opts); err != nil {
			return fmt.Errorf("agent: remember action: %w", err)
		}
	}
	if toolResult != "" {
		if _, err := a.Memory.Add(ctx, a.EntityID, memory.KindOutcome, toolResult, tick, opts); err != nil {
			return fmt.Errorf("agent: remember outcome: %w", err)
		}
	}
	return nil
}

// RememberObservation records an observation of an external event.
func (a *Agent) RememberObservation(ctx context.Context, tick world.Tick, content string) error {
	if a.Memory == nil || content == "" {
		return nil
	}
	_, err := a.Memory.Add(ctx, a.EntityID, memory.KindObservation, content, tick,
		memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance})
	if err != nil {
		return fmt.Errorf("agent: remember observation: %w", err)
	}
	return nil
}

// DeliverHeard enqueues a HeardEvent into this agent's inbox. The
// sim's broadcast machinery calls this on every other agent in the
// world when one of them issues Speak. Thread-safe.
func (a *Agent) DeliverHeard(ev brain.HeardEvent) {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	a.inbox = append(a.inbox, ev)
}

// DrainHeard returns and clears the inbox. Called by the sim just
// before BuildPerception so the agent perceives broadcasts on the
// tick they arrived. Thread-safe.
func (a *Agent) DrainHeard() []brain.HeardEvent {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	if len(a.inbox) == 0 {
		return nil
	}
	out := a.inbox
	a.inbox = nil
	return out
}

// InboxLen returns the number of pending Heard events without
// draining. Used by the sim's skip-tick decision: an agent that
// has been spoken to since its last turn must wake up.
func (a *Agent) InboxLen() int {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	return len(a.inbox)
}

// RememberHeard records a heard broadcast as an observation in the
// agent's memory stream so it persists past the next tick.
func (a *Agent) RememberHeard(ctx context.Context, tick world.Tick, ev brain.HeardEvent) error {
	if a.Memory == nil {
		return nil
	}
	content := fmt.Sprintf("I heard %s say: %s", ev.SpeakerName, ev.Content)
	_, err := a.Memory.Add(ctx, a.EntityID, memory.KindObservation, content, tick,
		memory.AddOptions{Embedder: a.Embedder, Scorer: a.Importance})
	if err != nil {
		return fmt.Errorf("agent: remember heard: %w", err)
	}
	return nil
}

// RetrieveMemories returns the top-K records relevant to the
// current perception query.
func (a *Agent) RetrieveMemories(ctx context.Context, query string, tick world.Tick) ([]memory.Record, error) {
	if a.Memory == nil {
		return nil, nil
	}
	k := a.RetrieveK
	if k <= 0 {
		k = 6
	}
	return a.Memory.Retrieve(ctx, query, tick, k, a.Embedder)
}

// BuildPerception assembles a Perception snapshot from the world.
// memories are the retrieved records to inject; may be nil.
// heard contains broadcasts drained from this agent's inbox; may
// be nil. As a side effect, BuildPerception clears focus when the
// focused entity is dead and decrements the focus turn counter,
// clearing it when it reaches zero.
func (a *Agent) BuildPerception(w *world.World, memories []memory.Record, heard []brain.HeardEvent) brain.Perception {
	ents := w.Entities()
	rels := w.Relationships()
	allEvents := w.Events()
	tick := w.Tick()

	// Lifecycle: a focus pinned on an entity that no longer exists
	// must be cleared before we build any focus view.
	if a.Focus != 0 {
		alive := false
		for _, e := range ents {
			if e.ID == a.Focus {
				alive = true
				break
			}
		}
		if !alive {
			a.Focus = 0
			a.FocusTurnsLeft = 0
		}
	}

	pe := make([]brain.EntityView, len(ents))
	entByID := make(map[world.EntityID]brain.EntityView, len(ents))
	for i, e := range ents {
		pe[i] = brain.EntityView{
			ID:         uint64(e.ID),
			TypeLabel:  e.TypeLabel,
			Properties: e.Properties,
		}
		entByID[e.ID] = pe[i]
	}
	pr := make([]brain.RelationshipView, len(rels))
	for i, r := range rels {
		pr[i] = brain.RelationshipView{
			ID:   uint64(r.ID),
			From: uint64(r.From),
			To:   uint64(r.To),
			Kind: r.Kind,
		}
	}
	// RecentEvents: events strictly newer than the agent's high-water
	// mark, in order.
	var recent []brain.EventView
	for _, e := range allEvents {
		if e.ID <= a.SeenEventID {
			continue
		}
		recent = append(recent, brain.EventView{
			ID:       uint64(e.ID),
			Tick:     uint64(e.Tick),
			Kind:     string(e.Kind),
			Agent:    uint64(e.Agent),
			EntityID: uint64(e.EntityID),
			RelID:    uint64(e.RelID),
			Summary:  summariseEvent(e),
		})
	}

	children, parents := buildContainmentGraph(rels)
	depth := assignDepth(ents, children, parents)

	p := brain.Perception{
		Tick:               uint64(tick),
		EntityCount:        w.EntityCount(),
		AliveEntities:      pe,
		AliveRelationships: pr,
		RecentEvents:       recent,
		Memories:           memory.PerceptionViews(memories),
		Heard:              heard,
		Frontier:           buildFrontier(ents, children, parents, depth),
		Suggestions:        a.collectSuggestions(ents, children, tick),
	}

	if a.Focus != 0 {
		p.Focus = buildFocusView(a.Focus, a.FocusTurnsLeft, entByID, children, rels)
		// If the focus entity vanished between the alive-check above
		// and the lookup (shouldn't happen, but be defensive), nil out.
		if p.Focus == nil {
			a.Focus = 0
			a.FocusTurnsLeft = 0
		} else if a.FocusTurnsLeft > 0 {
			a.FocusTurnsLeft--
			if a.FocusTurnsLeft == 0 {
				a.Focus = 0
			}
		}
	}

	return p
}

// MarkSeen advances the agent's SeenEventID to the highest event ID
// currently in the world. Called by the sim after each step so the
// next perception only carries the deltas.
func (a *Agent) MarkSeen(w *world.World) {
	events := w.Events()
	if len(events) == 0 {
		return
	}
	high := events[len(events)-1].ID
	if high > a.SeenEventID {
		a.SeenEventID = high
	}
}

// containsKind reports whether a relationship kind designates the
// soft "contains" hierarchy. Case-insensitive: the engine never
// interprets kinds, but "Contains" or "CONTAINS" should still feed
// the frontier so the LLM gets a forgiving affordance.
func containsKind(k string) bool { return strings.EqualFold(k, "contains") }

// buildContainmentGraph projects relationships into outgoing and
// incoming "contains" adjacency maps. Soft-deleted relationships are
// already filtered upstream (w.Relationships() returns live only).
func buildContainmentGraph(rels []world.Relationship) (children, parents map[world.EntityID][]world.EntityID) {
	children = map[world.EntityID][]world.EntityID{}
	parents = map[world.EntityID][]world.EntityID{}
	for _, r := range rels {
		if !containsKind(r.Kind) {
			continue
		}
		children[r.From] = append(children[r.From], r.To)
		parents[r.To] = append(parents[r.To], r.From)
	}
	return children, parents
}

// assignDepth runs a BFS from each containment root (alive non-agent
// entity with no incoming "contains" edge) and returns the shortest
// distance in edges from any root.
func assignDepth(
	ents []world.Entity,
	children, parents map[world.EntityID][]world.EntityID,
) map[world.EntityID]int {
	depth := map[world.EntityID]int{}
	queue := make([]world.EntityID, 0, len(ents))
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			continue
		}
		if len(parents[e.ID]) == 0 {
			depth[e.ID] = 0
			queue = append(queue, e.ID)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		d := depth[n]
		for _, c := range children[n] {
			if existing, ok := depth[c]; !ok || d+1 < existing {
				depth[c] = d + 1
				queue = append(queue, c)
			}
		}
	}
	return depth
}

// buildFrontier collects the deepest leaf entities and one chain
// from a root down to the deepest leaf. Returns the zero value when
// no "contains" edges exist. A leaf must be parented (have an
// incoming "contains" edge) and childless; orphans and roots are
// not leaves.
func buildFrontier(
	ents []world.Entity,
	children, parents map[world.EntityID][]world.EntityID,
	depth map[world.EntityID]int,
) brain.FrontierView {
	leaves := make([]brain.FrontierLeaf, 0, len(ents))
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			continue
		}
		if len(parents[e.ID]) == 0 {
			continue
		}
		if len(children[e.ID]) > 0 {
			continue
		}
		d, ok := depth[e.ID]
		if !ok {
			continue
		}
		leaves = append(leaves, brain.FrontierLeaf{
			EntityID:  uint64(e.ID),
			TypeLabel: e.TypeLabel,
			Depth:     d,
		})
	}
	sort.Slice(leaves, func(i, j int) bool {
		if leaves[i].Depth != leaves[j].Depth {
			return leaves[i].Depth > leaves[j].Depth
		}
		return leaves[i].EntityID < leaves[j].EntityID
	})
	if len(leaves) > frontierLeafCap {
		leaves = leaves[:frontierLeafCap]
	}

	deepest := longestContainmentPath(ents, children, depth)

	return brain.FrontierView{
		Leaves:      leaves,
		DeepestPath: deepest,
	}
}

// longestContainmentPath does a DFS from each containment root and
// returns the longest chain in nodes (path length in edges + 1).
// Ties are broken by smallest IDs along the chain so the result is
// deterministic.
func longestContainmentPath(
	ents []world.Entity,
	children map[world.EntityID][]world.EntityID,
	depth map[world.EntityID]int,
) []brain.FrontierStep {
	// Roots are entities at depth 0 with at least one child; an
	// entity with no edges at all is an orphan, not the root of a
	// containment chain.
	roots := make([]world.EntityID, 0)
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			continue
		}
		if d, ok := depth[e.ID]; ok && d == 0 && len(children[e.ID]) > 0 {
			roots = append(roots, e.ID)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })

	entByID := make(map[world.EntityID]world.Entity, len(ents))
	for _, e := range ents {
		entByID[e.ID] = e
	}

	var best []world.EntityID
	var dfs func(n world.EntityID, path []world.EntityID)
	dfs = func(n world.EntityID, path []world.EntityID) {
		path = append(path, n)
		kids := children[n]
		if len(kids) == 0 {
			if len(path) > len(best) || (len(path) == len(best) && idsLess(path, best)) {
				best = append(best[:0:0], path...)
			}
			return
		}
		// Sort kids for deterministic tie-breaking.
		sorted := append([]world.EntityID{}, kids...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		for _, c := range sorted {
			dfs(c, path)
		}
	}
	for _, r := range roots {
		dfs(r, nil)
	}
	if len(best) == 0 {
		return nil
	}
	out := make([]brain.FrontierStep, len(best))
	for i, id := range best {
		e := entByID[id]
		out[i] = brain.FrontierStep{
			EntityID:  uint64(id),
			TypeLabel: e.TypeLabel,
		}
	}
	return out
}

// idsLess returns true when a is lexicographically smaller than b
// across EntityIDs. Used to break ties when two paths share length.
func idsLess(a, b []world.EntityID) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// buildFocusView walks the "contains" sub-tree rooted at focus,
// capped at focusSubtreeCap nodes. Returns nil when the focus
// entity is not in entByID (caller already cleared lifecycle state).
func buildFocusView(
	focus world.EntityID,
	turnsLeft int,
	entByID map[world.EntityID]brain.EntityView,
	children map[world.EntityID][]world.EntityID,
	rels []world.Relationship,
) *brain.FocusView {
	root, ok := entByID[focus]
	if !ok {
		return nil
	}
	visited := map[world.EntityID]bool{focus: true}
	subtree := make([]brain.EntityView, 0)
	type frame struct {
		id    world.EntityID
		depth int
	}
	stack := []frame{{focus, 0}}
	depthBelow := 0
	for len(stack) > 0 && len(subtree) < focusSubtreeCap {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		// Children sorted for deterministic ordering.
		kids := append([]world.EntityID{}, children[f.id]...)
		sort.Slice(kids, func(i, j int) bool { return kids[i] < kids[j] })
		for _, c := range kids {
			if visited[c] {
				continue
			}
			visited[c] = true
			view, ok := entByID[c]
			if !ok {
				continue
			}
			subtree = append(subtree, view)
			if f.depth+1 > depthBelow {
				depthBelow = f.depth + 1
			}
			if len(subtree) >= focusSubtreeCap {
				break
			}
			stack = append(stack, frame{c, f.depth + 1})
		}
	}

	subRels := make([]brain.RelationshipView, 0)
	for _, r := range rels {
		if visited[r.From] && visited[r.To] {
			subRels = append(subRels, brain.RelationshipView{
				ID:   uint64(r.ID),
				From: uint64(r.From),
				To:   uint64(r.To),
				Kind: r.Kind,
			})
		}
	}

	return &brain.FocusView{
		EntityID:   uint64(focus),
		TypeLabel:  root.TypeLabel,
		TurnsLeft:  turnsLeft,
		Subtree:    subtree,
		SubRels:    subRels,
		DepthBelow: depthBelow,
	}
}

// collectSuggestions returns place-scoped sub-agent nudges for
// entities that have grown past frontierChildSpawnThreshold children
// since the last suggestion was emitted. It mutates the agent's
// dedupe cache so the same entity isn't surfaced every tick.
func (a *Agent) collectSuggestions(
	ents []world.Entity,
	children map[world.EntityID][]world.EntityID,
	tick world.Tick,
) []brain.SpawnSuggestion {
	if a.suggestionsLastTick == nil {
		a.suggestionsLastTick = make(map[world.EntityID]uint64)
	}
	// Bound the dedupe map: drop entries older than the horizon.
	horizon := uint64(0)
	if uint64(tick) > suggestionMemoryHorizonTicks {
		horizon = uint64(tick) - suggestionMemoryHorizonTicks
	}
	for id, lastTick := range a.suggestionsLastTick {
		if lastTick < horizon {
			delete(a.suggestionsLastTick, id)
		}
	}

	cooldown := uint64(suggestionCooldownTicks)
	suggestions := make([]brain.SpawnSuggestion, 0)
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			continue
		}
		count := len(children[e.ID])
		if count < frontierChildSpawnThreshold {
			continue
		}
		last, seen := a.suggestionsLastTick[e.ID]
		if seen && uint64(tick) < last+cooldown {
			continue
		}
		suggestions = append(suggestions, brain.SpawnSuggestion{
			EntityID:   uint64(e.ID),
			TypeLabel:  e.TypeLabel,
			ChildCount: count,
			Reason: fmt.Sprintf(
				"%d children; consider SpawnAgent scoped here",
				count,
			),
		})
	}
	if len(suggestions) == 0 {
		return nil
	}
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].ChildCount != suggestions[j].ChildCount {
			return suggestions[i].ChildCount > suggestions[j].ChildCount
		}
		return suggestions[i].EntityID < suggestions[j].EntityID
	})
	if len(suggestions) > maxSuggestionsPerTick {
		suggestions = suggestions[:maxSuggestionsPerTick]
	}
	for _, s := range suggestions {
		a.suggestionsLastTick[world.EntityID(s.EntityID)] = uint64(tick)
	}
	return suggestions
}

func summariseEvent(e world.Event) string {
	switch e.Kind {
	case world.EventCreate:
		if e.TypeLabel != "" {
			return fmt.Sprintf("create %s #%d", e.TypeLabel, e.EntityID)
		}
		return fmt.Sprintf("create #%d", e.EntityID)
	case world.EventModify:
		return fmt.Sprintf("modify #%d", e.EntityID)
	case world.EventDestroy:
		return fmt.Sprintf("destroy #%d", e.EntityID)
	case world.EventRelate:
		return fmt.Sprintf("relate #%d -[%s]-> #%d", e.From, e.RelKind, e.To)
	case world.EventUnrelate:
		if e.Cascade {
			return fmt.Sprintf("unrelate #%d (cascade)", e.RelID)
		}
		return fmt.Sprintf("unrelate #%d", e.RelID)
	case world.EventTickStart:
		return ""
	default:
		return string(e.Kind)
	}
}

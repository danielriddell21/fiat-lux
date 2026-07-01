package agent

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/drives"
	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

const (
	frontierLeafCap              = 12
	focusSubtreeCap              = 64
	frontierChildSpawnThreshold  = 6
	suggestionCooldownTicks      = 10
	suggestionMemoryHorizonTicks = 100
	maxSuggestionsPerTick        = 3
)

type Agent struct {
	EntityID world.EntityID

	Name string

	SystemPrompt string

	Brain brain.Brain

	Tools *tools.Registry

	Memory *memory.Stream

	Embedder memory.Embedder

	Importance memory.Importance

	RetrieveK int

	SeenEventID world.EventID

	SpawnDepth int

	Focus world.EntityID

	FocusTurnsLeft int

	suggestionsLastTick map[world.EntityID]uint64

	Drives drives.State

	ParentEntityID world.EntityID

	inbox   []brain.HeardEvent
	inboxMu sync.Mutex
}

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

func (a *Agent) DeliverHeard(ev brain.HeardEvent) {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	a.inbox = append(a.inbox, ev)
}

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

func (a *Agent) InboxLen() int {
	a.inboxMu.Lock()
	defer a.inboxMu.Unlock()
	return len(a.inbox)
}

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

func (a *Agent) RetrieveMemories(ctx context.Context, query string, tick world.Tick) ([]memory.Record, error) {
	if a.Memory == nil {
		return nil, nil
	}
	k := a.RetrieveK
	if k <= 0 {
		k = 6
	}
	recs, err := a.Memory.Retrieve(ctx, query, tick, k, a.Embedder)
	if err != nil {
		return nil, fmt.Errorf("retrieve memories: %w", err)
	}
	return recs, nil
}

func (a *Agent) BuildPerception(w *world.World, memories []memory.Record, heard []brain.HeardEvent) brain.Perception {
	ents := w.Entities()
	rels := w.Relationships()
	allEvents := w.Events()
	tick := w.Tick()

	// Lifecycle: a focus pinned on an entity that no longer exists
	// must be cleared before we build any focus view.
	if a.Focus != 0 {
		alive := slices.ContainsFunc(ents, func(e world.Entity) bool {
			return e.ID == a.Focus
		})
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
		Drives:             a.Drives.Clone(),
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

func containsKind(k string) bool { return strings.EqualFold(k, "contains") }

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
	slices.SortFunc(leaves, func(a, b brain.FrontierLeaf) int {
		return cmp.Or(
			cmp.Compare(b.Depth, a.Depth),
			cmp.Compare(a.EntityID, b.EntityID),
		)
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
	slices.Sort(roots)

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
			if len(path) > len(best) || (len(path) == len(best) && slices.Compare(path, best) < 0) {
				best = slices.Clone(path)
			}
			return
		}
		// Sort kids for deterministic tie-breaking.
		sorted := slices.Clone(kids)
		slices.Sort(sorted)
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
		kids := slices.Clone(children[f.id])
		slices.Sort(kids)
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
			depthBelow = max(depthBelow, f.depth+1)
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

func (a *Agent) collectSuggestions(
	ents []world.Entity,
	children map[world.EntityID][]world.EntityID,
	tick world.Tick,
) []brain.SpawnSuggestion {
	if a.suggestionsLastTick == nil {
		a.suggestionsLastTick = make(map[world.EntityID]uint64)
	}
	// Bound the dedupe map: drop entries older than the horizon.
	var horizon uint64
	if uint64(tick) > suggestionMemoryHorizonTicks {
		horizon = uint64(tick) - suggestionMemoryHorizonTicks
	}
	maps.DeleteFunc(a.suggestionsLastTick, func(_ world.EntityID, lastTick uint64) bool {
		return lastTick < horizon
	})

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
		if last, seen := a.suggestionsLastTick[e.ID]; seen && uint64(tick) < last+cooldown {
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
	slices.SortFunc(suggestions, func(a, b brain.SpawnSuggestion) int {
		return cmp.Or(
			cmp.Compare(b.ChildCount, a.ChildCount),
			cmp.Compare(a.EntityID, b.EntityID),
		)
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

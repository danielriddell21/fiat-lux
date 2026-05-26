// Package narrator implements a read-only meta-agent that watches a
// world and writes short narrative "chapters" to an Annals log. It
// uses its own brain (potentially a different model than the kosmos
// agents) and never calls tools - its only output is prose.
package narrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// DefaultSystemPrompt is the canonical narrator instruction. Callers
// can override via Options.SystemPrompt.
const DefaultSystemPrompt = `You are the chronicler of an emergent kosmos. ` +
	`You do not act; you observe. Each turn you write a brief chapter (3-6 ` +
	`sentences) summarising what just happened. Be evocative and concise. ` +
	`Refer to entities by their type and name. Do not invent events.`

// Defaults for the chapter cadence.
const (
	DefaultChapterIntervalTicks = uint64(20)
	DefaultMinEventsPerChapter  = 5
	DefaultMaxChapterLength     = 700
)

// Chapter is one entry in the Annals.
type Chapter struct {
	WorldName string    `json:"world_name"`
	Tick      uint64    `json:"tick"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Options configures a Narrator.
type Options struct {
	// Brain is required: the meta-agent's decision maker. The
	// narrator uses Brain.Decide with an empty tool list and reads
	// Decision.Thought as the chapter prose.
	Brain brain.Brain

	// SystemPrompt overrides DefaultSystemPrompt when non-empty.
	SystemPrompt string

	// IntervalTicks is the minimum number of ticks between chapters.
	// Zero uses DefaultChapterIntervalTicks.
	IntervalTicks uint64

	// MinEvents is the minimum number of events since the last
	// chapter required to write a new one - keeps the narrator
	// silent during long Wait runs.
	MinEvents int

	// MaxChapterLength clips the brain's Thought at this rune count.
	// Zero uses DefaultMaxChapterLength.
	MaxChapterLength int
}

// Narrator chronicles a world. Safe for serial use; callers must
// not invoke Tick from multiple goroutines concurrently.
type Narrator struct {
	brain            brain.Brain
	systemPrompt     string
	intervalTicks    uint64
	minEvents        int
	maxChapterLength int

	lastChapterTick world.Tick
	lastEventID     world.EventID
}

// New constructs a Narrator. Brain is required.
func New(opts Options) (*Narrator, error) {
	if opts.Brain == nil {
		return nil, errors.New("narrator: Brain is required")
	}
	n := &Narrator{
		brain:            opts.Brain,
		systemPrompt:     opts.SystemPrompt,
		intervalTicks:    opts.IntervalTicks,
		minEvents:        opts.MinEvents,
		maxChapterLength: opts.MaxChapterLength,
	}
	if n.systemPrompt == "" {
		n.systemPrompt = DefaultSystemPrompt
	}
	if n.intervalTicks == 0 {
		n.intervalTicks = DefaultChapterIntervalTicks
	}
	if n.minEvents == 0 {
		n.minEvents = DefaultMinEventsPerChapter
	}
	if n.maxChapterLength == 0 {
		n.maxChapterLength = DefaultMaxChapterLength
	}
	return n, nil
}

// ShouldChapter reports whether the narrator wants to write at this
// tick. The decision honours IntervalTicks and MinEvents.
func (n *Narrator) ShouldChapter(w *world.World) bool {
	if w == nil {
		return false
	}
	tick := w.Tick()
	if uint64(tick) < uint64(n.lastChapterTick)+n.intervalTicks {
		return false
	}
	var newEvents int
	for _, ev := range w.Events() {
		if ev.ID > n.lastEventID {
			newEvents++
		}
	}
	return newEvents >= n.minEvents
}

// WriteChapter calls the narrator's brain with a synthetic Perception
// describing the world and returns a fresh Chapter. ShouldChapter
// must have returned true; the caller is responsible for storing the
// chapter in the Annals.
func (n *Narrator) WriteChapter(ctx context.Context, w *world.World, agents []*agent.Agent) (Chapter, error) {
	if w == nil {
		return Chapter{}, errors.New("narrator: nil world")
	}
	tick := w.Tick()
	perception := n.buildPerception(w, agents)
	decision, err := n.brain.Decide(ctx, perception, nil)
	if err != nil {
		return Chapter{}, fmt.Errorf("narrator: brain: %w", err)
	}
	content := strings.TrimSpace(decision.Thought)
	if len(content) > n.maxChapterLength {
		runes := []rune(content)
		content = string(runes[:n.maxChapterLength])
	}
	n.lastChapterTick = tick
	if events := w.Events(); len(events) > 0 {
		n.lastEventID = events[len(events)-1].ID
	}
	return Chapter{
		WorldName: w.Name(),
		Tick:      uint64(tick),
		Content:   content,
		CreatedAt: time.Now(),
	}, nil
}

// SystemPrompt returns the static instruction the narrator uses.
// Exposed so callers (e.g. the sim wiring) can pass it as Brain
// configuration if the provider requires it.
func (n *Narrator) SystemPrompt() string { return n.systemPrompt }

// Close releases the underlying brain.
func (n *Narrator) Close() error {
	if n == nil || n.brain == nil {
		return nil
	}
	return n.brain.Close()
}

// buildPerception flattens the world into the brain's Perception
// shape. We include AliveEntities, AliveRelationships, and recent
// events so the chronicler has the same view as a regular agent.
func (n *Narrator) buildPerception(w *world.World, agents []*agent.Agent) brain.Perception {
	ents := w.Entities()
	rels := w.Relationships()
	allEvents := w.Events()

	pe := make([]brain.EntityView, len(ents))
	for i, e := range ents {
		pe[i] = brain.EntityView{
			ID:         uint64(e.ID),
			TypeLabel:  e.TypeLabel,
			Properties: e.Properties,
		}
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
	var recent []brain.EventView
	for _, e := range allEvents {
		if e.ID <= n.lastEventID {
			continue
		}
		recent = append(recent, brain.EventView{
			ID:       uint64(e.ID),
			Tick:     uint64(e.Tick),
			Kind:     string(e.Kind),
			Agent:    uint64(e.Agent),
			EntityID: uint64(e.EntityID),
			RelID:    uint64(e.RelID),
		})
	}
	// agents is informational only for prompt context; the narrator
	// already sees their entity properties via pe.
	_ = agents
	return brain.Perception{
		Tick:               uint64(w.Tick()),
		EntityCount:        w.EntityCount(),
		AliveEntities:      pe,
		AliveRelationships: pr,
		RecentEvents:       recent,
	}
}

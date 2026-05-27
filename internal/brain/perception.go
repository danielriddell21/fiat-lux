package brain

// Perception is the per-tick snapshot delivered to a Brain. It is
// deliberately decoupled from internal/world types so the brain
// package can be the dependency root for provider adapters.
type Perception struct {
	// Tick is the simulation tick at which the perception was built.
	Tick uint64

	// EntityCount is the number of live entities in the agent's
	// world.
	EntityCount int

	// AliveEntities lists every live entity, sorted by ID ascending.
	// For very large worlds the agent package may truncate or
	// summarise.
	AliveEntities []EntityView

	// AliveRelationships lists every live relationship, sorted by ID
	// ascending.
	AliveRelationships []RelationshipView

	// RecentEvents lists world events since the agent's previous
	// decision. Lets the agent perceive what other actors did while
	// it was idle.
	RecentEvents []EventView

	// Memories holds retrieved memory snippets.
	Memories []MemoryView

	// Heard lists Speak broadcasts from other agents that arrived
	// since this agent's previous tick. Empty for single-agent
	// worlds.
	Heard []HeardEvent

	// Focus is the entity the agent has pinned via Zoom, or nil when
	// no focus is set. Carries a DFS of the focused entity's "contains"
	// sub-tree so the brain can drill into it without re-walking the
	// full entity list.
	Focus *FocusView

	// Frontier surfaces where the world can be deepened: leaves of
	// the "contains" hierarchy and one deepest containment path. Zero
	// value is meaningful (empty world / no containment edges yet).
	Frontier FrontierView

	// Suggestions are place-scoped sub-agent nudges raised when an
	// entity has accumulated enough children that delegating its
	// further detailing to a SpawnAgent seems useful. Engine never
	// spawns; the LLM decides whether to act on a suggestion.
	Suggestions []SpawnSuggestion
}

// FocusView is the agent's current pinned-focus snapshot.
type FocusView struct {
	// EntityID of the focused entity.
	EntityID uint64

	// TypeLabel of the focused entity (for the brain's convenience).
	TypeLabel string

	// TurnsLeft is the number of upcoming turns the focus will remain
	// pinned before auto-expiring. Includes the current turn.
	TurnsLeft int

	// Subtree is a depth-first walk of entities reachable from the
	// focused entity via "contains" edges (excluding the focus itself).
	// Capped to keep the prompt bounded.
	Subtree []EntityView

	// SubRels are the live relationships entirely contained within
	// the Subtree (both endpoints present, including the focus).
	SubRels []RelationshipView

	// DepthBelow is the longest "contains" chain rooted at the focus,
	// measured in edges.
	DepthBelow int
}

// FrontierView summarises where the agent could drill deeper.
type FrontierView struct {
	// Leaves are entities that are contained by something but contain
	// nothing themselves. The set is capped and sorted by descending
	// depth then ascending ID for stability.
	Leaves []FrontierLeaf

	// DeepestPath is one chain from a containment root down to the
	// deepest leaf, longest first by edge count. Empty when no
	// "contains" edges exist.
	DeepestPath []FrontierStep
}

// FrontierLeaf is one leaf entity in the containment hierarchy.
type FrontierLeaf struct {
	EntityID  uint64
	TypeLabel string
	Depth     int
}

// FrontierStep is one node along a containment chain.
type FrontierStep struct {
	EntityID  uint64
	TypeLabel string
}

// SpawnSuggestion is a perception-time nudge: this entity has grown
// large enough to deserve its own sub-agent. The agent decides
// whether to act on it via SpawnAgent.
type SpawnSuggestion struct {
	EntityID   uint64
	TypeLabel  string
	ChildCount int
	Reason     string
}

// EntityView is the perception-side projection of a world.Entity.
type EntityView struct {
	ID         uint64
	TypeLabel  string
	Properties map[string]any
}

// RelationshipView is the perception-side projection of a
// world.Relationship.
type RelationshipView struct {
	ID   uint64
	From uint64
	To   uint64
	Kind string
}

// EventView is the perception-side projection of a world.Event. The
// fields are flattened for ease of consumption by both brains and
// memory.
type EventView struct {
	ID       uint64
	Tick     uint64
	Kind     string
	Agent    uint64
	EntityID uint64
	RelID    uint64
	Summary  string
}

// MemoryView is the perception-side projection of a memory record.
type MemoryView struct {
	Content    string
	Tick       uint64
	Importance float64
}

// HeardEvent is one Speak broadcast delivered to this agent. The
// sim drains an agent's inbox into Perception.Heard on each step.
type HeardEvent struct {
	// SpeakerID is the entity ID of the agent that spoke.
	SpeakerID uint64

	// SpeakerName is the speaker's display name pulled from its
	// entity properties.
	SpeakerName string

	// Tick is the simulation tick at which the Speak happened.
	Tick uint64

	// Content is what was said.
	Content string
}

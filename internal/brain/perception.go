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

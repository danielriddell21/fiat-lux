package world

// Entity is a single thing in the world. type_label and properties
// are agent-chosen and the simulation never interprets them.
type Entity struct {
	// ID is the world-local identifier.
	ID EntityID `json:"id"`

	// TypeLabel is the agent-chosen kind of this entity ("planet",
	// "organism", "agent", "cheese"). Free-form string.
	TypeLabel string `json:"type"`

	// Properties is the agent-authored property bag.
	Properties Properties `json:"properties,omitempty"`

	// CreatedBy is the AgentID of the agent that created this entity.
	// NoAgent (zero) means no recorded creator.
	CreatedBy AgentID `json:"created_by,omitempty"`

	// CreatedAt is the tick at which this entity was created.
	CreatedAt Tick `json:"created_at"`

	// DestroyedAt is the tick at which this entity was soft-deleted,
	// or nil while it is alive. Destroyed entities remain in the
	// store so a run can be replayed exactly.
	DestroyedAt *Tick `json:"destroyed_at,omitempty"`
}

// IsAlive reports whether the entity has not yet been destroyed.
func (e Entity) IsAlive() bool { return e.DestroyedAt == nil }

// Clone returns a deep copy. Callers receive clones from the World
// so they cannot mutate registry state by retaining references.
func (e Entity) Clone() Entity {
	out := e
	out.Properties = e.Properties.Clone()
	if e.DestroyedAt != nil {
		t := *e.DestroyedAt
		out.DestroyedAt = &t
	}
	return out
}

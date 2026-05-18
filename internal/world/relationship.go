package world

// Relationship connects two entities with an agent-chosen kind. The
// kind is a free-form string; the simulation never interprets it.
type Relationship struct {
	// ID is the world-local identifier.
	ID RelationshipID

	// From is the EntityID at the tail of the relationship.
	From EntityID

	// To is the EntityID at the head of the relationship.
	To EntityID

	// Kind is the agent-chosen relationship label ("lives in",
	// "child of", "contains").
	Kind string

	// CreatedBy is the AgentID of the agent that declared the
	// relationship.
	CreatedBy AgentID

	// CreatedAt is the tick at which the relationship was declared.
	CreatedAt Tick

	// DestroyedAt is the tick at which the relationship was
	// soft-deleted, or nil while it is live. Relationships are
	// cascade-destroyed when either endpoint is destroyed.
	DestroyedAt *Tick
}

// IsAlive reports whether the relationship has not yet been
// soft-deleted.
func (r Relationship) IsAlive() bool { return r.DestroyedAt == nil }

// Clone returns a deep copy.
func (r Relationship) Clone() Relationship {
	out := r
	if r.DestroyedAt != nil {
		t := *r.DestroyedAt
		out.DestroyedAt = &t
	}
	return out
}

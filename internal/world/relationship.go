package world

// Relationship connects two entities with an agent-chosen kind. The
// kind is a free-form string; the simulation never interprets it.
type Relationship struct {
	// ID is the world-local identifier.
	ID RelationshipID `json:"id"`

	// From is the EntityID at the tail of the relationship.
	From EntityID `json:"from"`

	// To is the EntityID at the head of the relationship.
	To EntityID `json:"to"`

	// Kind is the agent-chosen relationship label ("lives in",
	// "child of", "contains").
	Kind string `json:"kind"`

	// CreatedBy is the AgentID of the agent that declared the
	// relationship.
	CreatedBy AgentID `json:"created_by,omitempty"`

	// CreatedAt is the tick at which the relationship was declared.
	CreatedAt Tick `json:"created_at"`

	// DestroyedAt is the tick at which the relationship was
	// soft-deleted, or nil while it is live. Relationships are
	// cascade-destroyed when either endpoint is destroyed.
	DestroyedAt *Tick `json:"destroyed_at,omitempty"`
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

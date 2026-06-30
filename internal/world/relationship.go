package world

type Relationship struct {
	ID RelationshipID `json:"id"`

	From EntityID `json:"from"`

	To EntityID `json:"to"`

	Kind string `json:"kind"`

	CreatedBy AgentID `json:"created_by,omitempty"`

	CreatedAt Tick `json:"created_at"`

	DestroyedAt *Tick `json:"destroyed_at,omitempty"`
}

func (r Relationship) IsAlive() bool { return r.DestroyedAt == nil }

func (r Relationship) Clone() Relationship {
	out := r
	if r.DestroyedAt != nil {
		t := *r.DestroyedAt
		out.DestroyedAt = &t
	}
	return out
}

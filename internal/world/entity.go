package world

type Entity struct {
	ID EntityID `json:"id"`

	TypeLabel string `json:"type"`

	Properties Properties `json:"properties,omitempty"`

	CreatedBy AgentID `json:"created_by,omitempty"`

	CreatedAt Tick `json:"created_at"`

	DestroyedAt *Tick `json:"destroyed_at,omitempty"`
}

func (e Entity) IsAlive() bool { return e.DestroyedAt == nil }

func (e Entity) Clone() Entity {
	out := e
	out.Properties = e.Properties.Clone()
	if e.DestroyedAt != nil {
		t := *e.DestroyedAt
		out.DestroyedAt = &t
	}
	return out
}

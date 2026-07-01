package world

type EventKind string

const (
	EventCreate    EventKind = "create"
	EventModify    EventKind = "modify"
	EventDestroy   EventKind = "destroy"
	EventRelate    EventKind = "relate"
	EventUnrelate  EventKind = "unrelate"
	EventTickStart EventKind = "tick_start"

	EventDefineTool EventKind = "define_tool"

	EventDie EventKind = "die"
)

type Event struct {
	ID EventID `json:"id"`

	Tick Tick `json:"tick"`

	Kind EventKind `json:"kind"`

	Agent AgentID `json:"agent,omitempty"`

	EntityID EntityID `json:"entity_id,omitempty"`

	RelID RelationshipID `json:"rel_id,omitempty"`

	TypeLabel string `json:"type_label,omitempty"`

	Props Properties `json:"props,omitempty"`

	From EntityID `json:"from,omitempty"`
	To   EntityID `json:"to,omitempty"`

	RelKind string `json:"rel_kind,omitempty"`

	Cascade bool `json:"cascade,omitempty"`
}

func (e Event) Clone() Event {
	out := e
	out.Props = e.Props.Clone()
	return out
}

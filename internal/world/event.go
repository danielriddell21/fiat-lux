package world

// EventKind enumerates the kinds of mutation events the World can
// record. Free-text agent monologue is not an event - it goes to the
// memory stream, not to the world.
type EventKind string

// The complete set of event kinds the World emits.
const (
	EventCreate    EventKind = "create"
	EventModify    EventKind = "modify"
	EventDestroy   EventKind = "destroy"
	EventRelate    EventKind = "relate"
	EventUnrelate  EventKind = "unrelate"
	EventTickStart EventKind = "tick_start"

	// EventDefineTool records an agent's runtime macro definition.
	// The macro JSON is carried in Props["macro"]; the world treats
	// it as opaque - the sim layer reads these events on load to
	// rebuild each agent's macro map.
	EventDefineTool EventKind = "define_tool"

	// EventDie records an agent calling the Die tool. The agent's
	// entity is soft-destroyed and removed from the runtime roster;
	// any children inherit a memory digest before the event lands.
	EventDie EventKind = "die"
)

// Event is an immutable record of a single mutation. The full event
// log makes a run exactly replayable. Fields not relevant to a given
// Kind hold their zero values.
type Event struct {
	// ID is the monotonically assigned event number within this
	// World, starting at 1.
	ID EventID `json:"id"`

	// Tick is the simulation tick at which the event was emitted.
	Tick Tick `json:"tick"`

	// Kind discriminates the event.
	Kind EventKind `json:"kind"`

	// Agent is the AgentID responsible. NoAgent (zero) is allowed
	// for tick_start events and seeded test data.
	Agent AgentID `json:"agent,omitempty"`

	// EntityID is the affected entity for Create / Modify /
	// Destroy. Zero for Relate / Unrelate / TickStart.
	EntityID EntityID `json:"entity_id,omitempty"`

	// RelID is the affected relationship for Relate / Unrelate.
	// Zero for Create / Modify / Destroy / TickStart.
	RelID RelationshipID `json:"rel_id,omitempty"`

	// TypeLabel is the entity's agent-chosen type. Set only on
	// Create.
	TypeLabel string `json:"type_label,omitempty"`

	// Props carries Create's initial properties or Modify's patch.
	Props Properties `json:"props,omitempty"`

	// From / To are the endpoint EntityIDs of a Relate event.
	From EntityID `json:"from,omitempty"`
	To   EntityID `json:"to,omitempty"`

	// RelKind is the agent-chosen relationship label on Relate.
	RelKind string `json:"rel_kind,omitempty"`

	// Cascade is true on Destroy / Unrelate events that were
	// triggered by cascade (currently: unrelate cascading from a
	// destroyed entity).
	Cascade bool `json:"cascade,omitempty"`
}

// Clone returns a deep copy of the event, including its Props.
func (e Event) Clone() Event {
	out := e
	out.Props = e.Props.Clone()
	return out
}

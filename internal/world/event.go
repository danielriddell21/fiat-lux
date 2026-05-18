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
)

// Event is an immutable record of a single mutation. The full event
// log makes a run exactly replayable. Fields not relevant to a given
// Kind hold their zero values.
type Event struct {
	// ID is the monotonically assigned event number within this
	// World, starting at 1.
	ID EventID

	// Tick is the simulation tick at which the event was emitted.
	Tick Tick

	// Kind discriminates the event.
	Kind EventKind

	// Agent is the AgentID responsible. NoAgent (zero) is allowed
	// for tick_start events and seeded test data.
	Agent AgentID

	// EntityID is the affected entity for Create / Modify /
	// Destroy. Zero for Relate / Unrelate / TickStart.
	EntityID EntityID

	// RelID is the affected relationship for Relate / Unrelate.
	// Zero for Create / Modify / Destroy / TickStart.
	RelID RelationshipID

	// TypeLabel is the entity's agent-chosen type. Set only on
	// Create.
	TypeLabel string

	// Props carries Create's initial properties or Modify's patch.
	Props Properties

	// From / To are the endpoint EntityIDs of a Relate event.
	From EntityID
	To   EntityID

	// RelKind is the agent-chosen relationship label on Relate.
	RelKind string

	// Cascade is true on Destroy / Unrelate events that were
	// triggered by cascade (currently: unrelate cascading from a
	// destroyed entity).
	Cascade bool
}

// Clone returns a deep copy of the event, including its Props.
func (e Event) Clone() Event {
	out := e
	out.Props = e.Props.Clone()
	return out
}

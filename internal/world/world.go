package world

import (
	"fmt"
	"sort"
	"sync"
)

// World is the entity registry for one fiat-lux universe. It starts
// empty: zero entities, zero relationships, tick 0, no events.
// Mutations append to an event log that makes the run replayable.
//
// World is safe for concurrent use. Read operations take a read lock;
// mutations take a write lock. Per-world serialisation lives here.
type World struct {
	mu sync.RWMutex

	name string
	tick Tick

	entities      map[EntityID]*Entity
	relationships map[RelationshipID]*Relationship
	events        []Event

	nextEntity EntityID
	nextRel    RelationshipID
	nextEvent  EventID
}

// New constructs a new empty World with the given name. The name
// must be non-empty; it identifies the world in storage and the TUI.
func New(name string) (*World, error) {
	if name == "" {
		return nil, ErrEmptyName
	}
	return &World{
		name:          name,
		entities:      make(map[EntityID]*Entity),
		relationships: make(map[RelationshipID]*Relationship),
		nextEntity:    1,
		nextRel:       1,
		nextEvent:     1,
	}, nil
}

// Name returns the world's name.
func (w *World) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.name
}

// Tick returns the current simulation tick.
func (w *World) Tick() Tick {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tick
}

// AdvanceTick increments the simulation tick by one, emits a
// tick_start event, and returns the new tick value.
func (w *World) AdvanceTick() Tick {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tick++
	w.appendEvent(Event{
		Kind: EventTickStart,
	})
	return w.tick
}

// EntityCount returns the number of live (non-destroyed) entities.
func (w *World) EntityCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	n := 0
	for _, e := range w.entities {
		if e.IsAlive() {
			n++
		}
	}
	return n
}

// RelationshipCount returns the number of live (non-destroyed)
// relationships.
func (w *World) RelationshipCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	n := 0
	for _, r := range w.relationships {
		if r.IsAlive() {
			n++
		}
	}
	return n
}

// Entity returns a deep copy of the entity with the given ID. The
// second return value is false if no such entity exists (alive or
// destroyed).
func (w *World) Entity(id EntityID) (Entity, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	e, ok := w.entities[id]
	if !ok {
		return Entity{}, false
	}
	return e.Clone(), true
}

// Entities returns deep copies of all live entities, ordered by ID
// ascending.
func (w *World) Entities() []Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Entity, 0, len(w.entities))
	for _, e := range w.entities {
		if e.IsAlive() {
			out = append(out, e.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// EntitiesAll returns deep copies of every entity ever created,
// including destroyed ones, ordered by ID ascending.
func (w *World) EntitiesAll() []Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Entity, 0, len(w.entities))
	for _, e := range w.entities {
		out = append(out, e.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Relationship returns a deep copy of the relationship with the
// given ID, or false if none exists.
func (w *World) Relationship(id RelationshipID) (Relationship, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	r, ok := w.relationships[id]
	if !ok {
		return Relationship{}, false
	}
	return r.Clone(), true
}

// Relationships returns deep copies of all live relationships,
// ordered by ID ascending.
func (w *World) Relationships() []Relationship {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Relationship, 0, len(w.relationships))
	for _, r := range w.relationships {
		if r.IsAlive() {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RelationshipsAll returns deep copies of every relationship ever
// declared, including soft-deleted ones.
func (w *World) RelationshipsAll() []Relationship {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Relationship, 0, len(w.relationships))
	for _, r := range w.relationships {
		out = append(out, r.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Events returns deep copies of every event in this world's history,
// in the order they were emitted.
func (w *World) Events() []Event {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Event, len(w.events))
	for i, e := range w.events {
		out[i] = e.Clone()
	}
	return out
}

// Create brings a new entity into being. type_label must be
// non-empty; properties may be nil. Returns the new EntityID.
func (w *World) Create(by AgentID, typeLabel string, props Properties) (EntityID, error) {
	if typeLabel == "" {
		return 0, ErrEmptyTypeLabel
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntity
	w.nextEntity++

	w.entities[id] = &Entity{
		ID:         id,
		TypeLabel:  typeLabel,
		Properties: props.Clone(),
		CreatedBy:  by,
		CreatedAt:  w.tick,
	}
	w.appendEvent(Event{
		Kind:      EventCreate,
		Agent:     by,
		EntityID:  id,
		TypeLabel: typeLabel,
		Props:     props.Clone(),
	})
	return id, nil
}

// Modify applies an RFC 7396 merge patch to an entity's properties.
// Returns ErrEntityNotFound or ErrEntityDestroyed on failure.
func (w *World) Modify(by AgentID, id EntityID, patch Properties) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[id]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrEntityNotFound, id)
	}
	if !e.IsAlive() {
		return fmt.Errorf("%w: id=%d", ErrEntityDestroyed, id)
	}

	e.Properties = e.Properties.ApplyMergePatch(patch)
	w.appendEvent(Event{
		Kind:     EventModify,
		Agent:    by,
		EntityID: id,
		Props:    patch.Clone(),
	})
	return nil
}

// Destroy soft-deletes an entity. The entity remains in the registry
// (for replay) but is excluded from live queries. All live
// relationships referencing the entity are cascade-soft-deleted,
// each emitting its own Unrelate event with Cascade=true.
func (w *World) Destroy(by AgentID, id EntityID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[id]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrEntityNotFound, id)
	}
	if !e.IsAlive() {
		return fmt.Errorf("%w: id=%d", ErrEntityDestroyed, id)
	}

	t := w.tick
	e.DestroyedAt = &t
	w.appendEvent(Event{
		Kind:     EventDestroy,
		Agent:    by,
		EntityID: id,
	})

	for _, r := range w.relationships {
		if !r.IsAlive() {
			continue
		}
		if r.From == id || r.To == id {
			r.DestroyedAt = &t
			w.appendEvent(Event{
				Kind:    EventUnrelate,
				Agent:   by,
				RelID:   r.ID,
				Cascade: true,
			})
		}
	}
	return nil
}

// Relate declares a relationship between two live entities. The
// from and to endpoints may be the same entity. Returns the new
// RelationshipID.
func (w *World) Relate(by AgentID, from, to EntityID, kind string) (RelationshipID, error) {
	if kind == "" {
		return 0, ErrEmptyKind
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.requireLiveEntityLocked(from); err != nil {
		return 0, err
	}
	if err := w.requireLiveEntityLocked(to); err != nil {
		return 0, err
	}

	id := w.nextRel
	w.nextRel++

	w.relationships[id] = &Relationship{
		ID:        id,
		From:      from,
		To:        to,
		Kind:      kind,
		CreatedBy: by,
		CreatedAt: w.tick,
	}
	w.appendEvent(Event{
		Kind:    EventRelate,
		Agent:   by,
		RelID:   id,
		From:    from,
		To:      to,
		RelKind: kind,
	})
	return id, nil
}

// Unrelate soft-deletes a relationship.
func (w *World) Unrelate(by AgentID, id RelationshipID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	r, ok := w.relationships[id]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrRelationshipNotFound, id)
	}
	if !r.IsAlive() {
		return fmt.Errorf("%w: id=%d", ErrRelationshipDestroyed, id)
	}

	t := w.tick
	r.DestroyedAt = &t
	w.appendEvent(Event{
		Kind:  EventUnrelate,
		Agent: by,
		RelID: id,
	})
	return nil
}

// EmitInfo appends a non-mutating event kind to the log. It is the
// hook the sim layer uses to persist state that lives above the
// world layer (e.g. runtime-defined macros, agent death) while
// keeping the event log the single source of truth. Returns the
// assigned event ID.
//
// The world treats info events as opaque: ApplyEventForLoad records
// them but does not interpret Props (with the exception of EventDie,
// which soft-destroys the agent's entity to mirror live behaviour).
// EntityID is set to the calling agent so EventDie's replay path
// can find the entity to soft-destroy without re-encoding it in
// Props.
func (w *World) EmitInfo(by AgentID, kind EventKind, props Properties) EventID {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.appendEvent(Event{
		Kind:     kind,
		Agent:    by,
		EntityID: by,
		Props:    props.Clone(),
	})
	return w.nextEvent - 1
}

// NextEntityID returns the EntityID that would be assigned by the
// next call to Create. Exposed for the store's load path; not part
// of the simulation contract.
func (w *World) NextEntityID() EntityID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.nextEntity
}

// NextRelationshipID returns the RelationshipID that would be
// assigned by the next call to Relate.
func (w *World) NextRelationshipID() RelationshipID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.nextRel
}

// requireLiveEntityLocked must be called with w.mu held for writing.
func (w *World) requireLiveEntityLocked(id EntityID) error {
	e, ok := w.entities[id]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrEntityNotFound, id)
	}
	if !e.IsAlive() {
		return fmt.Errorf("%w: id=%d", ErrEntityDestroyed, id)
	}
	return nil
}

// appendEvent must be called with w.mu held for writing. It stamps
// the event with the next ID and the current tick and appends it to
// the log.
func (w *World) appendEvent(e Event) {
	e.ID = w.nextEvent
	e.Tick = w.tick
	w.nextEvent++
	w.events = append(w.events, e)
}

// ApplyEventForLoad replays a persisted event into this World. It
// is intended only for the store's load path: the World must be
// freshly constructed and not yet exposed to concurrent users. The
// event's ID, Tick, and any agent-assigned identifiers are honoured
// exactly so that loaded state is byte-identical to the saved state.
//
// On unknown event kinds or events that violate invariants (e.g. a
// modify against an absent entity) this returns an error and leaves
// the World in an undefined state.
func (w *World) ApplyEventForLoad(e Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e.ID == 0 {
		return fmt.Errorf("world: event has zero ID")
	}
	w.tick = e.Tick

	if err := w.applyEventKind(e); err != nil {
		return err
	}

	w.events = append(w.events, e.Clone())
	if e.ID >= w.nextEvent {
		w.nextEvent = e.ID + 1
	}
	if e.EntityID >= w.nextEntity {
		w.nextEntity = e.EntityID + 1
	}
	if e.RelID >= w.nextRel {
		w.nextRel = e.RelID + 1
	}
	return nil
}

// applyEventKind dispatches a persisted event to the handler for its
// kind. Tick has already been set by the caller. Must be called with
// w.mu held for writing.
func (w *World) applyEventKind(e Event) error {
	switch e.Kind {
	case EventTickStart, EventDefineTool:
		// EventTickStart needs only the tick update done by the caller;
		// EventDefineTool is sim-layer state the world records but does
		// not interpret.
		return nil
	case EventCreate:
		return w.applyCreate(e)
	case EventModify:
		return w.applyModify(e)
	case EventDestroy:
		return w.markDestroyed(e, "destroy")
	case EventDie:
		return w.markDestroyed(e, "die")
	case EventRelate:
		return w.applyRelate(e)
	case EventUnrelate:
		return w.applyUnrelate(e)
	default:
		return fmt.Errorf("world: unknown event kind %q", e.Kind)
	}
}

// applyCreate replays an entity creation event.
func (w *World) applyCreate(e Event) error {
	if e.EntityID == 0 {
		return fmt.Errorf("world: create event has zero EntityID")
	}
	if _, exists := w.entities[e.EntityID]; exists {
		return fmt.Errorf("world: create event %d collides with existing entity %d", e.ID, e.EntityID)
	}
	w.entities[e.EntityID] = &Entity{
		ID:         e.EntityID,
		TypeLabel:  e.TypeLabel,
		Properties: e.Props.Clone(),
		CreatedBy:  e.Agent,
		CreatedAt:  e.Tick,
	}
	return nil
}

// applyModify replays a merge-patch event against an existing entity.
func (w *World) applyModify(e Event) error {
	ent, ok := w.entities[e.EntityID]
	if !ok {
		return fmt.Errorf("world: modify event %d targets unknown entity %d", e.ID, e.EntityID)
	}
	ent.Properties = ent.Properties.ApplyMergePatch(e.Props)
	return nil
}

// markDestroyed soft-destroys the event's target entity. kind labels
// the event ("destroy" or "die") for error messages.
func (w *World) markDestroyed(e Event, kind string) error {
	ent, ok := w.entities[e.EntityID]
	if !ok {
		return fmt.Errorf("world: %s event %d targets unknown entity %d", kind, e.ID, e.EntityID)
	}
	t := e.Tick
	ent.DestroyedAt = &t
	return nil
}

// applyRelate replays a relationship creation event.
func (w *World) applyRelate(e Event) error {
	if e.RelID == 0 {
		return fmt.Errorf("world: relate event has zero RelID")
	}
	if _, exists := w.relationships[e.RelID]; exists {
		return fmt.Errorf("world: relate event %d collides with existing relationship %d", e.ID, e.RelID)
	}
	w.relationships[e.RelID] = &Relationship{
		ID:        e.RelID,
		From:      e.From,
		To:        e.To,
		Kind:      e.RelKind,
		CreatedBy: e.Agent,
		CreatedAt: e.Tick,
	}
	return nil
}

// applyUnrelate replays a relationship soft-delete event.
func (w *World) applyUnrelate(e Event) error {
	rel, ok := w.relationships[e.RelID]
	if !ok {
		return fmt.Errorf("world: unrelate event %d targets unknown relationship %d", e.ID, e.RelID)
	}
	t := e.Tick
	rel.DestroyedAt = &t
	return nil
}

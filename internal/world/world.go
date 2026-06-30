package world

import (
	"fmt"
	"sort"
	"sync"
)

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

func (w *World) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.name
}

func (w *World) Tick() Tick {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tick
}

func (w *World) AdvanceTick() Tick {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tick++
	w.appendEvent(Event{
		Kind: EventTickStart,
	})
	return w.tick
}

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

func (w *World) Entity(id EntityID) (Entity, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	e, ok := w.entities[id]
	if !ok {
		return Entity{}, false
	}
	return e.Clone(), true
}

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

func (w *World) Relationship(id RelationshipID) (Relationship, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	r, ok := w.relationships[id]
	if !ok {
		return Relationship{}, false
	}
	return r.Clone(), true
}

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

func (w *World) Events() []Event {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Event, len(w.events))
	for i, e := range w.events {
		out[i] = e.Clone()
	}
	return out
}

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

func (w *World) NextEntityID() EntityID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.nextEntity
}

func (w *World) NextRelationshipID() RelationshipID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.nextRel
}

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

func (w *World) appendEvent(e Event) {
	e.ID = w.nextEvent
	e.Tick = w.tick
	w.nextEvent++
	w.events = append(w.events, e)
}

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

func (w *World) applyModify(e Event) error {
	ent, ok := w.entities[e.EntityID]
	if !ok {
		return fmt.Errorf("world: modify event %d targets unknown entity %d", e.ID, e.EntityID)
	}
	ent.Properties = ent.Properties.ApplyMergePatch(e.Props)
	return nil
}

func (w *World) markDestroyed(e Event, kind string) error {
	ent, ok := w.entities[e.EntityID]
	if !ok {
		return fmt.Errorf("world: %s event %d targets unknown entity %d", kind, e.ID, e.EntityID)
	}
	t := e.Tick
	ent.DestroyedAt = &t
	return nil
}

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

func (w *World) applyUnrelate(e Event) error {
	rel, ok := w.relationships[e.RelID]
	if !ok {
		return fmt.Errorf("world: unrelate event %d targets unknown relationship %d", e.ID, e.RelID)
	}
	t := e.Tick
	rel.DestroyedAt = &t
	return nil
}

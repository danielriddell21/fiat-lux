package world

import "errors"

// Sentinel errors returned by World mutation methods. Callers may
// use errors.Is to discriminate.
var (
	// ErrEntityNotFound is returned when an EntityID does not refer
	// to any entity in this World.
	ErrEntityNotFound = errors.New("world: entity not found")

	// ErrEntityDestroyed is returned when a mutation targets an
	// entity that has already been soft-deleted.
	ErrEntityDestroyed = errors.New("world: entity already destroyed")

	// ErrRelationshipNotFound is returned when a RelationshipID does
	// not refer to any relationship in this World.
	ErrRelationshipNotFound = errors.New("world: relationship not found")

	// ErrRelationshipDestroyed is returned when a mutation targets a
	// relationship that has already been soft-deleted.
	ErrRelationshipDestroyed = errors.New("world: relationship already destroyed")

	// ErrEmptyTypeLabel is returned when Create is called with an
	// empty type_label.
	ErrEmptyTypeLabel = errors.New("world: type_label must be non-empty")

	// ErrEmptyKind is returned when Relate is called with an empty
	// kind string.
	ErrEmptyKind = errors.New("world: relationship kind must be non-empty")

	// ErrEmptyName is returned when NewWorld is called with an
	// empty name.
	ErrEmptyName = errors.New("world: name must be non-empty")
)

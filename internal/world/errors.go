package world

import "errors"

var (
	ErrEntityNotFound = errors.New("world: entity not found")

	ErrEntityDestroyed = errors.New("world: entity already destroyed")

	ErrRelationshipNotFound = errors.New("world: relationship not found")

	ErrRelationshipDestroyed = errors.New("world: relationship already destroyed")

	ErrEmptyTypeLabel = errors.New("world: type_label must be non-empty")

	ErrEmptyKind = errors.New("world: relationship kind must be non-empty")

	ErrEmptyName = errors.New("world: name must be non-empty")
)

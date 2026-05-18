package world

import "strconv"

// EntityID is the stable identifier of an entity within a single World.
// IDs are assigned monotonically starting at 1; the zero value is
// reserved to mean "none" (e.g. the absent creator of seeded test
// data, or an event that does not refer to an entity).
type EntityID uint64

// String renders the ID as a decimal number for logs and event payloads.
func (id EntityID) String() string { return strconv.FormatUint(uint64(id), 10) }

// RelationshipID is the stable identifier of a relationship within a
// single World. IDs are assigned monotonically starting at 1.
type RelationshipID uint64

// String renders the ID as a decimal number.
func (id RelationshipID) String() string { return strconv.FormatUint(uint64(id), 10) }

// EventID is the stable identifier of an event within a single
// World's history. IDs are assigned monotonically starting at 1.
type EventID uint64

// String renders the ID as a decimal number.
func (id EventID) String() string { return strconv.FormatUint(uint64(id), 10) }

// Tick is the discrete simulation step at which something happened.
// The World starts at tick 0 and is advanced by the sim via
// AdvanceTick.
type Tick uint64

// AgentID identifies the agent responsible for an action. Agents are
// entities, so an AgentID is just an EntityID. The alias makes API
// call sites self-documenting.
type AgentID = EntityID

// NoAgent is the sentinel AgentID used when an action has no recorded
// agent - currently only seeded test data and the special "advance
// tick" record.
const NoAgent AgentID = 0

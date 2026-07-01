package world

import (
	"math"
	"strconv"
)

type EntityID uint64

func (id EntityID) String() string { return strconv.FormatUint(uint64(id), 10) }

type RelationshipID uint64

func (id RelationshipID) String() string { return strconv.FormatUint(uint64(id), 10) }

type EventID uint64

func (id EventID) String() string { return strconv.FormatUint(uint64(id), 10) }

type Tick uint64

type AgentID = EntityID

const NoAgent AgentID = 0

const AgentIntervener AgentID = math.MaxInt64

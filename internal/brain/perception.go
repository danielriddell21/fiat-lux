package brain

type Perception struct {
	Tick uint64

	EntityCount int

	AliveEntities []EntityView

	AliveRelationships []RelationshipView

	RecentEvents []EventView

	Memories []MemoryView

	Heard []HeardEvent

	Focus *FocusView

	Frontier FrontierView

	Suggestions []SpawnSuggestion

	Drives map[string]float64
}

type FocusView struct {
	EntityID uint64

	TypeLabel string

	TurnsLeft int

	Subtree []EntityView

	SubRels []RelationshipView

	DepthBelow int
}

type FrontierView struct {
	Leaves []FrontierLeaf

	DeepestPath []FrontierStep
}

type FrontierLeaf struct {
	EntityID  uint64
	TypeLabel string
	Depth     int
}

type FrontierStep struct {
	EntityID  uint64
	TypeLabel string
}

type SpawnSuggestion struct {
	EntityID   uint64
	TypeLabel  string
	ChildCount int
	Reason     string
}

type EntityView struct {
	ID         uint64
	TypeLabel  string
	Properties map[string]any
}

type RelationshipView struct {
	ID   uint64
	From uint64
	To   uint64
	Kind string
}

type EventView struct {
	ID       uint64
	Tick     uint64
	Kind     string
	Agent    uint64
	EntityID uint64
	RelID    uint64
	Summary  string
}

type MemoryView struct {
	Content    string
	Tick       uint64
	Importance float64
}

type HeardEvent struct {
	SpeakerID uint64

	SpeakerName string

	Tick uint64

	Content string
}

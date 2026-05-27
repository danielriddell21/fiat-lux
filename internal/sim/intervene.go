package sim

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// InterventionOp is the discriminator for sandbox interventions.
type InterventionOp string

// Supported intervention operations. Each maps to the corresponding
// world mutator but is stamped with world.AgentIntervener instead of
// an agent's EntityID so the event log records who acted.
const (
	InterveneCreate   InterventionOp = "create"
	InterveneModify   InterventionOp = "modify"
	InterveneDestroy  InterventionOp = "destroy"
	InterveneRelate   InterventionOp = "relate"
	InterveneUnrelate InterventionOp = "unrelate"
	InterveneSpeak    InterventionOp = "speak"
)

// Intervention describes one external perturbation. Only the fields
// relevant to the chosen Op need be set; the sim validates the shape
// before applying.
type Intervention struct {
	Op InterventionOp `json:"op"`

	// Create.
	TypeLabel  string         `json:"type_label,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`

	// Modify, Destroy.
	EntityID uint64 `json:"entity_id,omitempty"`

	// Modify's merge patch shares Properties.
	// Relate.
	FromID  uint64 `json:"from_id,omitempty"`
	ToID    uint64 `json:"to_id,omitempty"`
	RelKind string `json:"rel_kind,omitempty"`

	// Unrelate.
	RelID uint64 `json:"rel_id,omitempty"`

	// Speak (broadcast as if from a divine narrator).
	Content string `json:"content,omitempty"`
}

// Intervene applies the given perturbation under the AgentIntervener
// sentinel and broadcasts an observation to every alive agent so the
// kosmos reacts to the miracle. Returns an error on invalid ops.
func (s *Sim) Intervene(ctx context.Context, op Intervention) error {
	if s == nil || s.World == nil {
		return errors.New("sim: nil receiver or world")
	}
	tick := s.World.Tick()
	summary, err := s.applyIntervention(op)
	if err != nil {
		return err
	}
	s.broadcastIntervention(ctx, summary, tick)
	return nil
}

func (s *Sim) applyIntervention(op Intervention) (string, error) {
	by := world.AgentIntervener
	switch op.Op {
	case InterveneCreate:
		if op.TypeLabel == "" {
			return "", errors.New("intervene: type_label required for create")
		}
		id, err := s.World.Create(by, op.TypeLabel, world.Properties(op.Properties))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("the void willed %s #%d into being", op.TypeLabel, id), nil
	case InterveneModify:
		if op.EntityID == 0 {
			return "", errors.New("intervene: entity_id required for modify")
		}
		if err := s.World.Modify(by, world.EntityID(op.EntityID), world.Properties(op.Properties)); err != nil {
			return "", err
		}
		return fmt.Sprintf("the void reshaped #%d", op.EntityID), nil
	case InterveneDestroy:
		if op.EntityID == 0 {
			return "", errors.New("intervene: entity_id required for destroy")
		}
		if err := s.World.Destroy(by, world.EntityID(op.EntityID)); err != nil {
			return "", err
		}
		return fmt.Sprintf("the void unmade #%d", op.EntityID), nil
	case InterveneRelate:
		if op.FromID == 0 || op.ToID == 0 || op.RelKind == "" {
			return "", errors.New("intervene: from_id, to_id, and rel_kind required for relate")
		}
		id, err := s.World.Relate(by, world.EntityID(op.FromID), world.EntityID(op.ToID), op.RelKind)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("the void bound #%d -[%s]-> #%d (relation #%d)", op.FromID, op.RelKind, op.ToID, id), nil
	case InterveneUnrelate:
		if op.RelID == 0 {
			return "", errors.New("intervene: rel_id required for unrelate")
		}
		if err := s.World.Unrelate(by, world.RelationshipID(op.RelID)); err != nil {
			return "", err
		}
		return fmt.Sprintf("the void severed relation #%d", op.RelID), nil
	case InterveneSpeak:
		if op.Content == "" {
			return "", errors.New("intervene: content required for speak")
		}
		return "the void whispers: " + op.Content, nil
	default:
		return "", fmt.Errorf("intervene: unknown op %q", op.Op)
	}
}

// broadcastIntervention delivers a HeardEvent describing the miracle
// to every alive agent so the perturbation is perceived explicitly,
// not just inferred from RecentEvents.
func (s *Sim) broadcastIntervention(_ context.Context, summary string, tick world.Tick) {
	s.mu.Lock()
	roster := make([]*agent.Agent, len(s.agents))
	copy(roster, s.agents)
	s.mu.Unlock()
	ev := brain.HeardEvent{
		SpeakerID:   uint64(world.AgentIntervener),
		SpeakerName: "the void",
		Tick:        uint64(tick),
		Content:     summary,
	}
	for _, ag := range roster {
		ag.DeliverHeard(ev)
	}
}

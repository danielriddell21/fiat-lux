package sim

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielriddell21/fiat-lux/internal/agent"
	"github.com/danielriddell21/fiat-lux/internal/brain"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

type InterventionOp string

const (
	InterveneCreate   InterventionOp = "create"
	InterveneModify   InterventionOp = "modify"
	InterveneDestroy  InterventionOp = "destroy"
	InterveneRelate   InterventionOp = "relate"
	InterveneUnrelate InterventionOp = "unrelate"
	InterveneSpeak    InterventionOp = "speak"
)

type Intervention struct {
	Op InterventionOp `json:"op"`

	TypeLabel  string         `json:"type_label,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`

	EntityID uint64 `json:"entity_id,omitempty"`

	FromID  uint64 `json:"from_id,omitempty"`
	ToID    uint64 `json:"to_id,omitempty"`
	RelKind string `json:"rel_kind,omitempty"`

	RelID uint64 `json:"rel_id,omitempty"`

	Content string `json:"content,omitempty"`
}

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
		return s.interveneCreate(by, op)
	case InterveneModify:
		return s.interveneModify(by, op)
	case InterveneDestroy:
		return s.interveneDestroy(by, op)
	case InterveneRelate:
		return s.interveneRelate(by, op)
	case InterveneUnrelate:
		return s.interveneUnrelate(by, op)
	case InterveneSpeak:
		if op.Content == "" {
			return "", errors.New("intervene: content required for speak")
		}
		return "the void whispers: " + op.Content, nil
	default:
		return "", fmt.Errorf("intervene: unknown op %q", op.Op)
	}
}

func (s *Sim) interveneCreate(by world.AgentID, op Intervention) (string, error) {
	if op.TypeLabel == "" {
		return "", errors.New("intervene: type_label required for create")
	}
	id, err := s.World.Create(by, op.TypeLabel, world.Properties(op.Properties))
	if err != nil {
		return "", fmt.Errorf("intervene create: %w", err)
	}
	return fmt.Sprintf("the void willed %s #%d into being", op.TypeLabel, id), nil
}

func (s *Sim) interveneModify(by world.AgentID, op Intervention) (string, error) {
	if op.EntityID == 0 {
		return "", errors.New("intervene: entity_id required for modify")
	}
	if err := s.World.Modify(by, world.EntityID(op.EntityID), world.Properties(op.Properties)); err != nil {
		return "", fmt.Errorf("intervene modify: %w", err)
	}
	return fmt.Sprintf("the void reshaped #%d", op.EntityID), nil
}

func (s *Sim) interveneDestroy(by world.AgentID, op Intervention) (string, error) {
	if op.EntityID == 0 {
		return "", errors.New("intervene: entity_id required for destroy")
	}
	if err := s.World.Destroy(by, world.EntityID(op.EntityID)); err != nil {
		return "", fmt.Errorf("intervene destroy: %w", err)
	}
	return fmt.Sprintf("the void unmade #%d", op.EntityID), nil
}

func (s *Sim) interveneRelate(by world.AgentID, op Intervention) (string, error) {
	if op.FromID == 0 || op.ToID == 0 || op.RelKind == "" {
		return "", errors.New("intervene: from_id, to_id, and rel_kind required for relate")
	}
	id, err := s.World.Relate(by, world.EntityID(op.FromID), world.EntityID(op.ToID), op.RelKind)
	if err != nil {
		return "", fmt.Errorf("intervene relate: %w", err)
	}
	return fmt.Sprintf("the void bound #%d -[%s]-> #%d (relation #%d)", op.FromID, op.RelKind, op.ToID, id), nil
}

func (s *Sim) interveneUnrelate(by world.AgentID, op Intervention) (string, error) {
	if op.RelID == 0 {
		return "", errors.New("intervene: rel_id required for unrelate")
	}
	if err := s.World.Unrelate(by, world.RelationshipID(op.RelID)); err != nil {
		return "", fmt.Errorf("intervene unrelate: %w", err)
	}
	return fmt.Sprintf("the void severed relation #%d", op.RelID), nil
}

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

package brain

import (
	"encoding/json"
	"fmt"
)

func RenderPerceptionJSON(p Perception) string {
	type rendered struct {
		Tick          uint64              `json:"tick"`
		EntityCount   int                 `json:"entity_count"`
		Drives        map[string]float64  `json:"drives,omitempty"`
		Entities      []entitySummary     `json:"entities,omitempty"`
		Relationships []relSummary        `json:"relationships,omitempty"`
		RecentEvents  []eventSummary      `json:"recent_events,omitempty"`
		Memories      []memorySummary     `json:"memories,omitempty"`
		Focus         *focusSummary       `json:"focus,omitempty"`
		Frontier      *frontierSummary    `json:"frontier,omitempty"`
		Suggestions   []suggestionSummary `json:"spawn_suggestions,omitempty"`
	}
	r := rendered{
		Tick:        p.Tick,
		EntityCount: p.EntityCount,
		Drives:      p.Drives,
	}
	for _, e := range p.AliveEntities {
		r.Entities = append(r.Entities, entitySummary{
			ID: e.ID, Type: e.TypeLabel, Props: e.Properties,
		})
	}
	for _, rel := range p.AliveRelationships {
		r.Relationships = append(r.Relationships, relSummary(rel))
	}
	for _, ev := range p.RecentEvents {
		s := ev.Summary
		if s == "" {
			s = ev.Kind
		}
		r.RecentEvents = append(r.RecentEvents, eventSummary{
			Tick: ev.Tick, Summary: s,
		})
	}
	for _, m := range p.Memories {
		r.Memories = append(r.Memories, memorySummary{
			Tick: m.Tick, Content: m.Content,
		})
	}
	r.Focus = renderFocus(p.Focus)
	r.Frontier = renderFrontier(p.Frontier)
	for _, s := range p.Suggestions {
		r.Suggestions = append(r.Suggestions, suggestionSummary{
			EntityID: s.EntityID, Type: s.TypeLabel,
			ChildCount: s.ChildCount, Reason: s.Reason,
		})
	}

	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("(perception render failed: %v)", err)
	}
	return string(b)
}

func renderFocus(f *FocusView) *focusSummary {
	if f == nil {
		return nil
	}
	out := &focusSummary{
		EntityID:   f.EntityID,
		Type:       f.TypeLabel,
		TurnsLeft:  f.TurnsLeft,
		DepthBelow: f.DepthBelow,
	}
	for _, e := range f.Subtree {
		out.Subtree = append(out.Subtree, entitySummary{
			ID: e.ID, Type: e.TypeLabel, Props: e.Properties,
		})
	}
	for _, r := range f.SubRels {
		out.SubRels = append(out.SubRels, relSummary(r))
	}
	return out
}

func renderFrontier(f FrontierView) *frontierSummary {
	if len(f.Leaves) == 0 && len(f.DeepestPath) == 0 {
		return nil
	}
	out := &frontierSummary{}
	for _, l := range f.Leaves {
		out.Leaves = append(out.Leaves, leafSummary{ID: l.EntityID, Type: l.TypeLabel, Depth: l.Depth})
	}
	for _, s := range f.DeepestPath {
		out.DeepestPath = append(out.DeepestPath, stepSummary{ID: s.EntityID, Type: s.TypeLabel})
	}
	return out
}

type entitySummary struct {
	ID    uint64         `json:"id"`
	Type  string         `json:"type"`
	Props map[string]any `json:"properties,omitempty"`
}

type relSummary struct {
	ID   uint64 `json:"id"`
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
	Kind string `json:"kind"`
}

type eventSummary struct {
	Tick    uint64 `json:"tick"`
	Summary string `json:"event"`
}

type memorySummary struct {
	Tick    uint64 `json:"tick"`
	Content string `json:"content"`
}

type focusSummary struct {
	EntityID   uint64          `json:"entity_id"`
	Type       string          `json:"type"`
	TurnsLeft  int             `json:"turns_left"`
	DepthBelow int             `json:"depth_below"`
	Subtree    []entitySummary `json:"subtree,omitempty"`
	SubRels    []relSummary    `json:"subtree_relationships,omitempty"`
}

type frontierSummary struct {
	Leaves      []leafSummary `json:"leaves,omitempty"`
	DeepestPath []stepSummary `json:"deepest_path,omitempty"`
}

type leafSummary struct {
	ID    uint64 `json:"id"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
}

type stepSummary struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`
}

type suggestionSummary struct {
	EntityID   uint64 `json:"entity_id"`
	Type       string `json:"type"`
	ChildCount int    `json:"child_count"`
	Reason     string `json:"reason"`
}

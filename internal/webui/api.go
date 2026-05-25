package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/danielriddell21/fiat-lux/internal/sim"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// recentEventsCap bounds /api/state's RecentEvents tail; the SSE
// stream is the source of truth for live updates.
const recentEventsCap = 50

// StateResponse is the snapshot returned by GET /api/state.
type StateResponse struct {
	World         string               `json:"world"`
	Tick          uint64               `json:"tick"`
	Entities      []world.Entity       `json:"entities"`
	Relationships []world.Relationship `json:"relationships"`
	Agents        []AgentSummary       `json:"agents"`
	RecentEvents  []world.Event        `json:"recent_events"`
}

// AgentSummary is a light projection of agent.Agent for the side rail.
type AgentSummary struct {
	ID         uint64 `json:"id"`
	Name       string `json:"name"`
	IsCreator  bool   `json:"is_creator"`
	SpawnDepth int    `json:"spawn_depth"`
}

// StepEvent is the payload pushed over SSE on every Sim.Step.
type StepEvent struct {
	Tick       uint64 `json:"tick"`
	AgentID    uint64 `json:"agent_id,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	Thought    string `json:"thought,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
	ToolErr    string `json:"tool_err,omitempty"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	sm := s.provider()
	if sm == nil {
		http.Error(w, "no sim attached", http.StatusServiceUnavailable)
		return
	}
	resp := buildState(sm)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logf("encode state: %v", err)
	}
}

func buildState(sm *sim.Sim) StateResponse {
	wd := sm.World
	entities := wd.Entities()
	rels := wd.Relationships()
	events := wd.Events()
	if len(events) > recentEventsCap {
		events = events[len(events)-recentEventsCap:]
	}
	roster := sm.Agents()
	agents := make([]AgentSummary, len(roster))
	for i, ag := range roster {
		agents[i] = AgentSummary{
			ID:         uint64(ag.EntityID),
			Name:       ag.Name,
			IsCreator:  ag.SpawnDepth == 0,
			SpawnDepth: ag.SpawnDepth,
		}
	}
	return StateResponse{
		World:         wd.Name(),
		Tick:          uint64(wd.Tick()),
		Entities:      entities,
		Relationships: rels,
		Agents:        agents,
		RecentEvents:  events,
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// CORS: localhost-only by default, but if exposed externally a
	// browser at a different origin still needs explicit allowance.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch, unsubscribe := s.broker.subscribe()
	defer unsubscribe()

	// Hello byte so EventSource's open event fires promptly.
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case res, ok := <-ch:
			if !ok {
				return
			}
			ev := StepEvent{
				Tick:       uint64(res.Tick),
				AgentID:    uint64(res.AgentID),
				AgentName:  res.AgentName,
				Skipped:    res.Skipped,
				Thought:    res.Thought,
				ToolName:   res.ToolName,
				ToolResult: res.ToolResult,
				ToolErr:    errString(res.ToolErr),
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			name := "step"
			if res.Skipped {
				name = "tick"
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

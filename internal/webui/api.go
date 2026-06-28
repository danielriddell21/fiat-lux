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
	ID         uint64             `json:"id"`
	Name       string             `json:"name"`
	IsCreator  bool               `json:"is_creator"`
	SpawnDepth int                `json:"spawn_depth"`
	Drives     map[string]float64 `json:"drives,omitempty"`
	ParentID   uint64             `json:"parent_id,omitempty"`
	Dead       bool               `json:"dead,omitempty"`
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
			Drives:     ag.Drives.Clone(),
			ParentID:   uint64(ag.ParentEntityID),
			Dead:       sm.IsDead(ag.EntityID),
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

// handleImage serves bytes from the multimodal image cache at
// /api/image/<hash>.png. The hash is whatever imagegen.Hash produced
// for the prompt when the image was written.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	if s.imageCache == nil {
		http.Error(w, "image cache not configured", http.StatusNotFound)
		return
	}
	const prefix = "/api/image/"
	if len(r.URL.Path) <= len(prefix) {
		http.NotFound(w, r)
		return
	}
	name := r.URL.Path[len(prefix):]
	// Strip extension; the cache file is stored as <hash>.bin.
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			name = name[:i]
			break
		}
	}
	data, err := s.imageCache.GetByHash(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data) //nolint:gosec // cached PNG bytes served with an image/png content-type, not HTML
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// handleIntervene applies a sandbox intervention to the currently
// focused world. POST only; body is sim.Intervention as JSON.
func (s *Server) handleIntervene(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sm := s.provider()
	if sm == nil {
		http.Error(w, "no sim attached", http.StatusServiceUnavailable)
		return
	}
	var op sim.Intervention
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := sm.Intervene(r.Context(), op); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAnnals returns the narrator's chapter log for the currently
// focused world. The response is JSON: { "chapters": [...] }.
func (s *Server) handleAnnals(w http.ResponseWriter, r *http.Request) {
	sm := s.provider()
	if sm == nil {
		http.Error(w, "no sim attached", http.StatusServiceUnavailable)
		return
	}
	chapters := sm.Annals()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"chapters": chapters,
	})
}

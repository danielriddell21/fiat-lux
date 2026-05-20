package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/brain/stub"
	"github.com/danielriddell21/fiat-lux/internal/sim"
	"github.com/danielriddell21/fiat-lux/internal/tools"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

func newTestSim(t *testing.T) *sim.Sim {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	br := stub.New(1, 2, tools.Default())
	s, err := sim.New(sim.Options{
		World: w,
		Brain: br,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedWorld populates a sim with a small known world so the JSON
// shape can be asserted deterministically.
func seedWorld(t *testing.T, s *sim.Sim) (world.EntityID, world.EntityID) {
	t.Helper()
	a, err := s.World.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.World.Create(world.NoAgent, "ocean", world.Properties{"depth": 5.0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.World.Relate(world.NoAgent, b, a, "part of"); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func newTestServer(t *testing.T, s *sim.Sim) *Server {
	t.Helper()
	srv, err := New(Options{
		Addr:     "127.0.0.1:0",
		Provider: func() *sim.Sim { return s },
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestState_ServesSnapshotWithJSONTags(t *testing.T) {
	t.Parallel()
	s := newTestSim(t)
	planet, ocean := seedWorld(t, s)

	srv := newTestServer(t, s)
	hs := httptest.NewServer(srv.srv.Handler)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got StateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.World != "kosmos" {
		t.Errorf("World = %q, want kosmos", got.World)
	}
	// 3 entities: the auto-spawned creator + planet + ocean.
	if len(got.Entities) != 3 {
		t.Fatalf("entities = %d, want 3 (creator + planet + ocean)", len(got.Entities))
	}
	byID := map[world.EntityID]world.Entity{}
	for _, e := range got.Entities {
		byID[e.ID] = e
	}
	if e := byID[planet]; e.TypeLabel != "planet" {
		t.Errorf("planet entity = %+v", e)
	}
	if e := byID[ocean]; e.TypeLabel != "ocean" {
		t.Errorf("ocean entity = %+v", e)
	}
	if len(got.Relationships) != 1 || got.Relationships[0].Kind != "part of" {
		t.Errorf("relationships = %+v", got.Relationships)
	}
	// The tree has the creator + the planet as roots (the ocean is
	// nested under planet via "part of").
	if len(got.Tree.Roots) != 2 {
		t.Errorf("tree.Roots = %d, want 2 (creator + planet)", len(got.Tree.Roots))
	}
	if len(got.Agents) != 1 || !got.Agents[0].IsCreator {
		t.Errorf("agents = %+v", got.Agents)
	}
}

func TestState_JSONFieldNamesUseLowerSnake(t *testing.T) {
	t.Parallel()
	s := newTestSim(t)
	seedWorld(t, s)

	srv := newTestServer(t, s)
	hs := httptest.NewServer(srv.srv.Handler)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf, _ := io.ReadAll(resp.Body)
	body := string(buf)
	for _, needle := range []string{`"id":`, `"type":`, `"properties":`, `"created_at":`, `"from":`, `"to":`, `"kind":`} {
		if !strings.Contains(body, needle) {
			t.Errorf("response missing %q\n%s", needle, body)
		}
	}
	// "TypeLabel" (PascalCase) should not appear: JSON tags must apply.
	if strings.Contains(body, "TypeLabel") {
		t.Errorf("response leaked Go field name (TypeLabel) - JSON tags not applied:\n%s", body)
	}
}

func TestEvents_StreamsStepResults(t *testing.T) {
	t.Parallel()
	s := newTestSim(t)
	srv := newTestServer(t, s)
	hs := httptest.NewServer(srv.srv.Handler)
	defer hs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hs.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	reader := bufio.NewReader(resp.Body)
	// Drain the initial ": connected" comment.
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_, _ = reader.ReadString('\n')

	// Give the handler a moment to register its subscriber, then
	// publish a couple of events.
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(50 * time.Millisecond)
		srv.Publish(sim.StepResult{
			Tick: 7, AgentID: 1, AgentName: "the creator",
			Thought: "let there be light", ToolName: "Create",
		})
		srv.Publish(sim.StepResult{Tick: 8, Skipped: true})
	}()

	// First event should be a "step".
	got, err := readSSEEvent(reader)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}
	if got.event != "step" {
		t.Errorf("event = %q, want step", got.event)
	}
	var payload StepEvent
	if err := json.Unmarshal([]byte(got.data), &payload); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if payload.Tick != 7 || payload.ToolName != "Create" {
		t.Errorf("payload = %+v", payload)
	}

	got, err = readSSEEvent(reader)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}
	if got.event != "tick" {
		t.Errorf("event = %q, want tick", got.event)
	}
	<-done
}

type sseFrame struct {
	event string
	data  string
}

// readSSEEvent reads one event: / data: pair from the SSE stream.
func readSSEEvent(r *bufio.Reader) (sseFrame, error) {
	var f sseFrame
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return f, err
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			if f.data != "" {
				return f, nil
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			f.event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			f.data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func TestIndex_ServesEmbeddedHTML(t *testing.T) {
	t.Parallel()
	s := newTestSim(t)
	srv := newTestServer(t, s)
	hs := httptest.NewServer(srv.srv.Handler)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html prefix", ct)
	}
	buf, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(buf), "<title>fiat-lux</title>") {
		t.Errorf("index missing fiat-lux title: %s", string(buf)[:200])
	}
}

func TestAssets_ServesEmbeddedCSS(t *testing.T) {
	t.Parallel()
	s := newTestSim(t)
	srv := newTestServer(t, s)
	hs := httptest.NewServer(srv.srv.Handler)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("css status = %d", resp.StatusCode)
	}
}

func TestBroker_DropsSlowSubscriber(t *testing.T) {
	t.Parallel()
	b := newBroker()
	ch, unsub := b.subscribe()
	defer unsub()

	// Saturate the buffered channel without draining; then publish a
	// flood. Each publish should return immediately, dropping any
	// overflow rather than blocking.
	for i := 0; i < 200; i++ {
		b.publish(sim.StepResult{Tick: world.Tick(i)})
	}
	// First few should be in the buffer; reading them shouldn't block.
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected at least one buffered event")
	}
}

func TestIsLocalOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
	}
	for _, c := range cases {
		s := &Server{addr: c.addr}
		if got := s.isLocalOnly(); got != c.want {
			t.Errorf("isLocalOnly(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

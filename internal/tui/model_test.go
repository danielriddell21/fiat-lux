package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

func newModel(t *testing.T, st Storer) Model {
	t.Helper()
	w, err := world.New("kosmos")
	if err != nil {
		t.Fatal(err)
	}
	return NewModel(w, st, nil)
}

func sendKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, _ := m.handleKey(key)
	out, ok := next.(Model)
	if !ok {
		t.Fatalf("handleKey did not return Model, got %T", next)
	}
	return out
}

func TestNewModel_StartsPausedWithEmptyWorld(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	if !m.IsPaused() {
		t.Errorf("expected paused at start")
	}
	if m.World().EntityCount() != 0 {
		t.Errorf("entity count = %d, want 0", m.World().EntityCount())
	}
}

func TestQuitKeysReturnTeaQuit(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"q", "ctrl+c", "esc"} {
		m := newModel(t, nil)
		_, cmd := m.handleKey(k)
		if cmd == nil {
			t.Fatalf("key %q produced no command; expected tea.Quit", k)
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("key %q -> %T, want tea.QuitMsg", k, msg)
		}
	}
}

func TestTogglePause(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	if !m.IsPaused() {
		t.Fatal("expected paused at start")
	}
	m = sendKey(t, m, "space")
	if m.IsPaused() {
		t.Errorf("space should have unpaused")
	}
	m = sendKey(t, m, "space")
	if !m.IsPaused() {
		t.Errorf("second space should have re-paused")
	}
}

func TestToggleHelp(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	if m.HelpVisible() {
		t.Fatal("help should start hidden")
	}
	m = sendKey(t, m, "?")
	if !m.HelpVisible() {
		t.Errorf("? did not show help")
	}
	m = sendKey(t, m, "?")
	if m.HelpVisible() {
		t.Errorf("? did not hide help on second press")
	}
}

func TestDebugCreate_AddsEntity(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	if m.World().EntityCount() != 0 {
		t.Fatal("expected empty world at start")
	}
	m = sendKey(t, m, "n")
	if m.World().EntityCount() != 1 {
		t.Errorf("after debug-create, EntityCount = %d, want 1", m.World().EntityCount())
	}
	if !strings.Contains(m.Message(), "created") {
		t.Errorf("flash message missing 'created': %q", m.Message())
	}
}

func TestDebugCreate_CyclesTypes(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	for i := 0; i < 3; i++ {
		m = sendKey(t, m, "n")
	}
	ents := m.World().Entities()
	if len(ents) != 3 {
		t.Fatalf("entities = %d, want 3", len(ents))
	}
	seen := map[string]bool{}
	for _, e := range ents {
		seen[e.TypeLabel] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct type_labels, got %d: %v", len(seen), seen)
	}
}

func TestDebugDestroy_NoEntitiesReportsMessage(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	m = sendKey(t, m, "x")
	if m.World().EntityCount() != 0 {
		t.Errorf("EntityCount = %d, want 0", m.World().EntityCount())
	}
	if !strings.Contains(m.Message(), "nothing to destroy") {
		t.Errorf("flash missing expected message: %q", m.Message())
	}
}

func TestDebugDestroy_SoftDeletesMostRecent(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	m = sendKey(t, m, "n")
	m = sendKey(t, m, "n")
	m = sendKey(t, m, "x")
	if got := m.World().EntityCount(); got != 1 {
		t.Errorf("EntityCount after destroy = %d, want 1", got)
	}
}

func TestDebugRelate_NeedsTwoEntities(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	m = sendKey(t, m, "n")
	m = sendKey(t, m, "r")
	if m.World().RelationshipCount() != 0 {
		t.Errorf("relationship created with only 1 entity")
	}
	m = sendKey(t, m, "n")
	m = sendKey(t, m, "r")
	if m.World().RelationshipCount() != 1 {
		t.Errorf("RelationshipCount = %d, want 1", m.World().RelationshipCount())
	}
}

func TestDebugAdvance_BumpsTick(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	m = sendKey(t, m, "t")
	if got := m.World().Tick(); got != 1 {
		t.Errorf("tick = %d, want 1", got)
	}
}

type fakeStore struct {
	saved int
	err   error
}

func (f *fakeStore) Save(_ context.Context, _ *world.World) error {
	f.saved++
	return f.err
}

func TestSave_NoStoreReportsMessage(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	m = sendKey(t, m, "s")
	if !strings.Contains(m.Message(), "no store configured") {
		t.Errorf("flash missing expected message: %q", m.Message())
	}
}

func TestSave_CallsStorer(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{}
	m := newModel(t, fs)
	m = sendKey(t, m, "s")
	if fs.saved != 1 {
		t.Errorf("Save called %d times, want 1", fs.saved)
	}
	if !strings.Contains(m.Message(), "saved") {
		t.Errorf("flash missing 'saved': %q", m.Message())
	}
}

func TestSave_PropagatesError(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{err: errors.New("boom")}
	m := newModel(t, fs)
	m = sendKey(t, m, "s")
	if !strings.Contains(m.Message(), "save failed") {
		t.Errorf("flash missing 'save failed': %q", m.Message())
	}
}

func TestRender_WithWindowSizeProducesOutput(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	_, _ = w.Create(world.NoAgent, "planet", world.Properties{"name": "Erith"})
	m := NewModel(w, nil, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := next.(Model).Render()

	for _, want := range []string{"fiat-lux", "kosmos", "planet", "Erith", "Creation Tree", "Event Log"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

type fakeStepper struct {
	calls  int
	result StepSummary
	err    error
}

func (f *fakeStepper) Step(_ context.Context) (StepSummary, error) {
	f.calls++
	return f.result, f.err
}

func TestSimTick_SkipsWhilePaused(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &fakeStepper{}
	m := NewModel(w, nil, fs)
	if !m.IsPaused() {
		t.Fatal("expected paused at start")
	}
	next, _ := m.Update(simTickMsg{})
	if fs.calls != 0 {
		t.Errorf("Step called %d times while paused", fs.calls)
	}
	if !next.(Model).IsPaused() {
		t.Error("paused state lost on tick")
	}
}

func TestSimTick_DispatchesAsyncStep(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &fakeStepper{result: StepSummary{Tick: 7, ToolName: "Create", Thought: "let there be"}}
	m := NewModel(w, nil, fs)
	m = sendKey(t, m, "space") // unpause
	next, cmd := m.Update(simTickMsg{})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected a tea.Cmd from handleSimTick")
	}
	if !m.stepInFlight {
		t.Errorf("expected stepInFlight = true after dispatch")
	}
}

func TestStepCompleted_UpdatesLastStep(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &fakeStepper{}
	m := NewModel(w, nil, fs)
	m.stepInFlight = true
	res := StepSummary{Tick: 9, ToolName: "Create", ToolResult: "x"}
	next, _ := m.Update(stepCompletedMsg{Summary: res})
	got := next.(Model)
	if got.stepInFlight {
		t.Errorf("stepInFlight should clear on completion")
	}
	if got.LastStep().Tick != 9 || got.LastStep().ToolName != "Create" {
		t.Errorf("LastStep = %+v", got.LastStep())
	}
}

func TestSimTick_Backpressure(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &fakeStepper{}
	m := NewModel(w, nil, fs)
	m = sendKey(t, m, "space") // unpause
	m.stepInFlight = true
	next, _ := m.Update(simTickMsg{})
	m = next.(Model)
	if fs.calls != 0 {
		t.Errorf("backpressure violated: Step called while a step was in flight (%d calls)", fs.calls)
	}
	if m.skippedTicks != 1 {
		t.Errorf("skippedTicks = %d, want 1", m.skippedTicks)
	}
}

func TestRender_ReasoningPaneShowsLastStep(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &fakeStepper{result: StepSummary{
		Tick: 42, Thought: "I will make a planet", ToolName: "Create", ToolResult: "created planet #2",
	}}
	m := NewModel(w, nil, fs)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	// Inject a completed step directly so we don't need to drive the
	// async dispatch from a test.
	next, _ = m.Update(stepCompletedMsg{Summary: fs.result})
	m = next.(Model)

	out := m.Render()
	for _, want := range []string{"tick 42", "I will make a planet", "Create", "created planet #2"} {
		if !strings.Contains(out, want) {
			t.Errorf("reasoning pane missing %q\n%s", want, out)
		}
	}
}

type memoryFakeStepper struct {
	*fakeStepper
	snap MemorySnapshot
}

func (m *memoryFakeStepper) MemorySnapshot() MemorySnapshot { return m.snap }

func TestSpeedKeys_AdjustTickInterval(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	start := m.tickInterval
	m = sendKey(t, m, "+")
	if !(m.tickInterval < start) {
		t.Errorf("'+' tick interval = %s, want < %s", m.tickInterval, start)
	}
	mFast := m
	m = sendKey(t, m, "-")
	if !(m.tickInterval > mFast.tickInterval) {
		t.Errorf("'-' tick interval = %s, want > %s", m.tickInterval, mFast.tickInterval)
	}
	// Hammer '+' until clamped, then check the floor holds.
	for i := 0; i < 50; i++ {
		m = sendKey(t, m, "+")
	}
	if m.tickInterval < 100*1e6 {
		t.Errorf("speed up unclamped: %s", m.tickInterval)
	}
}

func TestMemoryToggle(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	if m.MemoryVisible() {
		t.Fatal("memory overlay should start hidden")
	}
	m = sendKey(t, m, "m")
	if !m.MemoryVisible() {
		t.Errorf("'m' did not show memory overlay")
	}
	m = sendKey(t, m, "m")
	if m.MemoryVisible() {
		t.Errorf("second 'm' did not hide memory overlay")
	}
}

func TestRender_MemoryOverlay(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &memoryFakeStepper{
		fakeStepper: &fakeStepper{},
		snap: MemorySnapshot{
			AgentName: "the creator",
			Records: []MemoryRecord{
				{ID: 1, Kind: "action", Content: "I made a planet", Tick: 1, Importance: 5.0},
				{ID: 2, Kind: "reflection", Content: "the world has shape now", Tick: 5, Importance: 8.5},
			},
		},
	}
	m := NewModel(w, nil, fs)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m = sendKey(t, m, "m")
	out := m.Render()
	for _, want := range []string{"memory stream", "the creator", "I made a planet", "reflection", "the world has shape"} {
		if !strings.Contains(out, want) {
			t.Errorf("memory overlay missing %q\n%s", want, out)
		}
	}
}

type multiAgentStepper struct {
	*fakeStepper
	agents  []AgentInfo
	focused uint64
}

func (m *multiAgentStepper) Agents() []AgentInfo    { return m.agents }
func (m *multiAgentStepper) FocusedAgentID() uint64 { return m.focused }
func (m *multiAgentStepper) SetFocusedAgentID(id uint64) {
	m.focused = id
}

func TestFocusCycling(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &multiAgentStepper{
		fakeStepper: &fakeStepper{},
		agents: []AgentInfo{
			{ID: 1, Name: "the creator", IsCreator: true},
			{ID: 4, Name: "observer"},
			{ID: 7, Name: "scribe"},
		},
		focused: 1,
	}
	m := NewModel(w, nil, fs)

	// tab moves forwards: 1 -> 4
	m = sendKey(t, m, "tab")
	if fs.focused != 4 {
		t.Errorf("after tab focused = %d, want 4", fs.focused)
	}
	// tab again: 4 -> 7
	m = sendKey(t, m, "tab")
	if fs.focused != 7 {
		t.Errorf("after second tab focused = %d, want 7", fs.focused)
	}
	// shift+tab: 7 -> 4
	m = sendKey(t, m, "shift+tab")
	if fs.focused != 4 {
		t.Errorf("after shift+tab focused = %d, want 4", fs.focused)
	}
	// tab wraps: 4 -> 7 -> 1
	m = sendKey(t, m, "tab")
	m = sendKey(t, m, "tab")
	if fs.focused != 1 {
		t.Errorf("after wrap focused = %d, want 1", fs.focused)
	}
	_ = m
}

func TestRender_AgentsStrip(t *testing.T) {
	t.Parallel()
	w, _ := world.New("kosmos")
	fs := &multiAgentStepper{
		fakeStepper: &fakeStepper{},
		agents: []AgentInfo{
			{ID: 1, Name: "the creator", IsCreator: true},
			{ID: 2, Name: "observer"},
		},
		focused: 2,
	}
	m := NewModel(w, nil, fs)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	out := m.Render()
	for _, want := range []string{"the creator", "observer", "#1", "#2"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents strip missing %q\n%s", want, out)
		}
	}
}

func TestRender_HelpOverlay(t *testing.T) {
	t.Parallel()
	m := newModel(t, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m = sendKey(t, m, "?")
	out := m.Render()
	if !strings.Contains(out, "key bindings") {
		t.Errorf("help overlay not rendered:\n%s", out)
	}
}

package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Storer is the persistence contract the Model needs. The store
// package's *Store satisfies it; tests substitute fakes.
type Storer interface {
	Save(ctx context.Context, w *world.World) error
}

// Stepper is the contract for whatever drives the sim. The sim
// package's *Sim satisfies it via an adapter in cmd/fiatlux. The
// Model calls Step from its tick command when unpaused.
type Stepper interface {
	Step(ctx context.Context) (StepSummary, error)
}

// MemoryAccessor is an optional supertype of Stepper used by the
// memory inspector overlay. The sim adapter satisfies it when a
// memory stream is attached.
type MemoryAccessor interface {
	MemorySnapshot() MemorySnapshot
}

// AgentInfo is the TUI's minimal projection of one agent.
type AgentInfo struct {
	ID        uint64
	Name      string
	IsCreator bool
	Drives    map[string]float64
}

// AgentLister is an optional Stepper supertype the TUI uses for
// focus cycling across multiple agents.
type AgentLister interface {
	Agents() []AgentInfo
	FocusedAgentID() uint64
	SetFocusedAgentID(id uint64)
}

// WorldInfo is the TUI's minimal projection of one world.
type WorldInfo struct {
	Index       int
	Name        string
	Tick        uint64
	EntityCount int
	AgentCount  int
}

// UniverseLister is an optional Stepper supertype the TUI uses for
// multi-world configurations. With ctrl-tab the user cycles which
// world is in focus; the world / agents / memory inspector all
// follow.
type UniverseLister interface {
	Worlds() []WorldInfo
	FocusedWorldIdx() int
	SetFocusedWorldIdx(i int)
	CycleFocusedWorld(delta int)
}

// WorldProvider supplies the focused world to the TUI. With a
// single-world model this is a static reference; with a
// Universe attached it returns the currently-focused world.
type WorldProvider interface {
	FocusedWorld() *world.World
}

// StepSummary is the TUI's minimal projection of sim.StepResult.
// Defined here to keep internal/tui independent of internal/sim;
// the wiring in main maps between them.
type StepSummary struct {
	Tick       uint64
	Skipped    bool
	Thought    string
	ToolName   string
	ToolResult string
	ToolErr    error
}

// Model is the Bubble Tea v2 model that drives the fiat-lux TUI.
type Model struct {
	world  *world.World
	store  Storer
	sim    Stepper
	styles Styles
	keys   KeyMap

	width  int
	height int

	paused        bool
	helpVisible   bool
	memoryVisible bool

	tickInterval time.Duration
	lastStep     StepSummary
	stepInFlight bool
	skippedTicks int

	message     string
	messageTime time.Time

	// dummyCursor cycles through type_label candidates so successive
	// debug-create presses produce varied output.
	dummyCursor int
}

// dummyTypes is the rotation used by the "n" debug keybind.
var dummyTypes = []string{"planet", "ocean", "continent", "mountain", "forest", "star", "creature", "idea"}

// flashTimeoutMsg is delivered by tea.Tick to clear a transient
// status message after a short delay.
type flashTimeoutMsg time.Time

// simTickMsg fires on every sim-tick interval. When the model is not
// paused it triggers a Stepper.Step.
type simTickMsg struct{}

// stepCompletedMsg is delivered by the async Step goroutine when
// the brain call returns.
type stepCompletedMsg struct {
	Summary StepSummary
	Err     error
}

// DefaultTickInterval is the gap between sim ticks. Conservative so
// the TUI stays readable even with a chatty stub.
const DefaultTickInterval = 1500 * time.Millisecond

// NewModel constructs a TUI model. The world must already be
// non-nil; store and sim may be nil to disable persistence and
// auto-stepping respectively.
func NewModel(w *world.World, st Storer, sm Stepper) Model {
	return Model{
		world:        w,
		store:        st,
		sim:          sm,
		styles:       DefaultStyles(),
		keys:         DefaultKeyMap(),
		paused:       true,
		tickInterval: DefaultTickInterval,
	}
}

// WithTickInterval returns a copy of the model with a custom tick
// interval. Useful for tests that want to step quickly.
func (m Model) WithTickInterval(d time.Duration) Model {
	if d <= 0 {
		d = DefaultTickInterval
	}
	m.tickInterval = d
	return m
}

// LastStep exposes the most recent Stepper result for tests.
func (m Model) LastStep() StepSummary { return m.lastStep }

// World returns the underlying world used as the default when no
// Universe is attached. Exposed mainly for tests.
func (m Model) World() *world.World { return m.world }

// focusedWorld is the world the TUI is currently rendering. With
// a Universe attached this follows ctrl-tab focus; otherwise it's
// the static world passed at NewModel.
func (m Model) focusedWorld() *world.World {
	if wp, ok := m.sim.(WorldProvider); ok && wp != nil {
		if w := wp.FocusedWorld(); w != nil {
			return w
		}
	}
	return m.world
}

// IsPaused reports the current pause state. Exposed for tests.
func (m Model) IsPaused() bool { return m.paused }

// HelpVisible reports whether the help overlay is shown. Exposed
// for tests.
func (m Model) HelpVisible() bool { return m.helpVisible }

// MemoryVisible reports whether the memory inspector overlay is
// shown.
func (m Model) MemoryVisible() bool { return m.memoryVisible }

// Message returns the current transient status line, if any.
func (m Model) Message() string { return m.message }

// Init satisfies tea.Model. When a sim is attached it schedules the
// first sim-tick. The model starts paused so the first tick is a
// no-op until the user presses space.
func (m Model) Init() tea.Cmd {
	if m.sim == nil {
		return nil
	}
	return m.scheduleTick()
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	case flashTimeoutMsg:
		if time.Time(msg).Equal(m.messageTime) {
			m.message = ""
			m.messageTime = time.Time{}
		}
		return m, nil
	case simTickMsg:
		return m.handleSimTick()
	case stepCompletedMsg:
		return m.handleStepCompleted(msg)
	}
	return m, nil
}

func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(m.tickInterval, func(_ time.Time) tea.Msg { return simTickMsg{} })
}

// cycleWorld advances the focused world by delta (typically +1 or
// -1). No-op when the sim does not expose UniverseLister or there
// is only one world.
func (m Model) cycleWorld(delta int) {
	lister, ok := m.sim.(UniverseLister)
	if !ok {
		return
	}
	if len(lister.Worlds()) < 2 {
		return
	}
	lister.CycleFocusedWorld(delta)
}

// cycleFocus advances the focused agent forwards (+1) or backwards
// (-1) through the AgentLister's list. No-op when the sim does not
// expose AgentLister or the list is shorter than 2.
func (m Model) cycleFocus(delta int) {
	lister, ok := m.sim.(AgentLister)
	if !ok {
		return
	}
	agents := lister.Agents()
	if len(agents) < 2 {
		return
	}
	current := lister.FocusedAgentID()
	idx := 0
	for i, a := range agents {
		if a.ID == current {
			idx = i
			break
		}
	}
	next := (idx + delta + len(agents)) % len(agents)
	lister.SetFocusedAgentID(agents[next].ID)
}

func (m Model) handleSimTick() (tea.Model, tea.Cmd) {
	if m.sim == nil {
		return m, nil
	}
	if m.paused {
		return m, m.scheduleTick()
	}
	if m.stepInFlight {
		// Backpressure: if a tick is due but the previous brain call
		// hasn't returned, skip - never queue infinitely. The TUI
		// counts skipped ticks so the operator can see when --tick
		// is set lower than the brain's latency.
		m.skippedTicks++
		return m, m.scheduleTick()
	}
	m.stepInFlight = true
	stepper := m.sim
	stepCmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := stepper.Step(ctx)
		return stepCompletedMsg{Summary: res, Err: err}
	}
	return m, tea.Batch(stepCmd, m.scheduleTick())
}

func (m Model) handleStepCompleted(msg stepCompletedMsg) (tea.Model, tea.Cmd) {
	m.stepInFlight = false
	if msg.Err != nil {
		return m.flashThenTick(fmt.Sprintf("sim error: %v", msg.Err))
	}
	m.lastStep = msg.Summary
	return m, nil
}

func (m Model) flashThenTick(text string) (tea.Model, tea.Cmd) {
	m.message = text
	m.messageTime = time.Now()
	at := m.messageTime
	clearMsg := tea.Tick(messageTTL, func(_ time.Time) tea.Msg { return flashTimeoutMsg(at) })
	return m, tea.Batch(clearMsg, m.scheduleTick())
}

const messageTTL = 3 * time.Second

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch {
	case Matches(key, m.keys.Quit):
		return m, tea.Quit
	case Matches(key, m.keys.Help):
		m.helpVisible = !m.helpVisible
		return m, nil
	case Matches(key, m.keys.MemoryToggle):
		m.memoryVisible = !m.memoryVisible
		return m, nil
	case Matches(key, m.keys.FocusNext):
		m.cycleFocus(+1)
		return m, nil
	case Matches(key, m.keys.FocusPrev):
		m.cycleFocus(-1)
		return m, nil
	case Matches(key, m.keys.WorldNext):
		m.cycleWorld(+1)
		return m, nil
	case Matches(key, m.keys.WorldPrev):
		m.cycleWorld(-1)
		return m, nil
	case Matches(key, m.keys.TogglePause):
		m.paused = !m.paused
		return m, nil
	case Matches(key, m.keys.SpeedUp):
		return m.adjustSpeed(2, 1)
	case Matches(key, m.keys.SpeedDown):
		return m.adjustSpeed(1, 2)
	case Matches(key, m.keys.Save):
		return m.handleSave()
	case Matches(key, m.keys.DebugCreate):
		return m.handleDebugCreate()
	case Matches(key, m.keys.DebugDestroy):
		return m.handleDebugDestroy()
	case Matches(key, m.keys.DebugRelate):
		return m.handleDebugRelate()
	case Matches(key, m.keys.DebugAdvance):
		_ = m.focusedWorld().AdvanceTick()
		return m.flash(fmt.Sprintf("tick advanced to %d", m.focusedWorld().Tick()))
	}
	return m, nil
}

func (m Model) handleDebugCreate() (tea.Model, tea.Cmd) {
	t := dummyTypes[m.dummyCursor%len(dummyTypes)]
	m.dummyCursor++
	props := world.Properties{"name": fmt.Sprintf("%s-%d", t, m.dummyCursor)}
	id, err := m.focusedWorld().Create(world.NoAgent, t, props)
	if err != nil {
		return m.flash(fmt.Sprintf("create failed: %v", err))
	}
	return m.flash(fmt.Sprintf("created %s #%d", t, id))
}

func (m Model) handleDebugDestroy() (tea.Model, tea.Cmd) {
	live := m.focusedWorld().Entities()
	if len(live) == 0 {
		return m.flash("nothing to destroy")
	}
	target := live[len(live)-1]
	if err := m.focusedWorld().Destroy(world.NoAgent, target.ID); err != nil {
		return m.flash(fmt.Sprintf("destroy failed: %v", err))
	}
	return m.flash(fmt.Sprintf("destroyed #%d", target.ID))
}

func (m Model) handleDebugRelate() (tea.Model, tea.Cmd) {
	live := m.focusedWorld().Entities()
	if len(live) < 2 {
		return m.flash("need at least 2 live entities to relate")
	}
	from := live[len(live)-1]
	to := live[len(live)-2]
	id, err := m.focusedWorld().Relate(world.NoAgent, from.ID, to.ID, "part of")
	if err != nil {
		return m.flash(fmt.Sprintf("relate failed: %v", err))
	}
	return m.flash(fmt.Sprintf("relation #%d: #%d part of #%d", id, from.ID, to.ID))
}

// adjustSpeed scales the tick interval by num/den (e.g. 2/1 to halve
// the interval = double the rate). Clamped so the user can't disappear
// off the fast end into a busy loop or off the slow end into nothing
// happening at all. The next scheduleTick() picks up the new value.
func (m Model) adjustSpeed(num, den int) (tea.Model, tea.Cmd) {
	const (
		minInterval = 100 * time.Millisecond
		maxInterval = 30 * time.Second
	)
	next := m.tickInterval * time.Duration(den) / time.Duration(num)
	if next < minInterval {
		next = minInterval
	}
	if next > maxInterval {
		next = maxInterval
	}
	m.tickInterval = next
	return m.flash(fmt.Sprintf("tick interval: %s", next))
}

func (m Model) handleSave() (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m.flash("no store configured - pass --db to enable save")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.store.Save(ctx, m.world); err != nil {
		return m.flash(fmt.Sprintf("save failed: %v", err))
	}
	return m.flash(fmt.Sprintf("saved %q (%d events)", m.focusedWorld().Name(), len(m.focusedWorld().Events())))
}

func (m Model) flash(text string) (tea.Model, tea.Cmd) {
	m.message = text
	m.messageTime = time.Now()
	at := m.messageTime
	return m, tea.Tick(messageTTL, func(_ time.Time) tea.Msg {
		return flashTimeoutMsg(at)
	})
}

// View satisfies tea.Model. The view runs in the alternate screen
// buffer so the TUI fully owns the terminal while running.
func (m Model) View() tea.View {
	v := tea.NewView(m.Render())
	v.AltScreen = true
	return v
}

// Render is the testable body of View - returns the raw string with
// no tea.View wrapping.
func (m Model) Render() string {
	w := m.width
	h := m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}

	if m.helpVisible {
		return m.renderHelpScreen(w, h)
	}
	if m.memoryVisible {
		return m.renderMemoryScreen(w, h)
	}

	info := StatusInfo{
		WorldName:   m.focusedWorld().Name(),
		Tick:        uint64(m.focusedWorld().Tick()),
		EntityCount: m.focusedWorld().EntityCount(),
		AgentCount:  countAgentEntities(m.focusedWorld().Entities()),
		Paused:      m.paused,
		Width:       w,
	}
	status := RenderStatus(info, m.styles)
	if ul, ok := m.sim.(UniverseLister); ok {
		ws := ul.Worlds()
		if len(ws) > 1 {
			status = status + "\n" + RenderWorldsStrip(ws, ul.FocusedWorldIdx(), m.styles)
		}
	}

	leftW := w * 2 / 5
	if leftW < 24 {
		leftW = 24
	}
	rightW := w - leftW - 2
	if rightW < 24 {
		rightW = 24
	}

	tv := BuildTreeView(m.focusedWorld().Entities(), m.focusedWorld().Relationships())
	leftBody := RenderTree(tv, m.styles)

	rightBody := RenderReasoning(m.lastStep, m.sim != nil, m.styles)
	// Prepend the agent roster strip if multi-agent.
	if lister, ok := m.sim.(AgentLister); ok {
		agents := lister.Agents()
		if len(agents) > 0 {
			focused := lister.FocusedAgentID()
			header := RenderAgentsStrip(agents, focused, m.styles)
			if drivesLine := RenderDrivesLine(agents, focused, m.styles); drivesLine != "" {
				header += "\n" + drivesLine
			}
			rightBody = header + "\n" + rightBody
		}
	}

	leftPane := boxed(m.styles, "Creation Tree", leftBody, leftW, paneInnerHeight(h))
	rightPane := boxed(m.styles, "Reasoning", rightBody, rightW, paneInnerHeight(h))
	top := lg.JoinHorizontal(lg.Top, leftPane, rightPane)

	logBody := RenderEventLog(m.focusedWorld().Events(), 6, m.styles)
	logPane := boxed(m.styles, "Event Log", logBody, w-2, 8)

	footer := RenderHelpFooter(m.styles)
	parts := []string{status, top, logPane}
	if m.message != "" {
		parts = append(parts, m.styles.Faint.Render(m.message))
	}
	parts = append(parts, footer)
	return lg.JoinVertical(lg.Left, parts...)
}

func countAgentEntities(ents []world.Entity) int {
	n := 0
	for _, e := range ents {
		if e.TypeLabel == "agent" {
			n++
		}
	}
	return n
}

func paneInnerHeight(total int) int {
	// total - status bar (1) - log pane (8) - footer (1) - message (1) - borders/margins (~3)
	h := total - 1 - 8 - 1 - 1 - 3
	if h < 8 {
		h = 8
	}
	return h
}

func boxed(s Styles, title, body string, width, height int) string {
	titleLine := s.PaneTitle.Render(title)
	content := titleLine + "\n" + body
	return s.PaneBorder.Width(width).Height(height).Render(content)
}

func (m Model) renderMemoryScreen(width, height int) string {
	var snap MemorySnapshot
	if accessor, ok := m.sim.(MemoryAccessor); ok && m.sim != nil {
		snap = accessor.MemorySnapshot()
	}
	body := RenderMemoryOverlay(snap, m.styles)
	w := width - 4
	if w > 80 {
		w = 80
	}
	box := m.styles.PaneBorder.Width(w).Render(body)
	return lg.Place(width, height, lg.Center, lg.Center, box)
}

func (m Model) renderHelpScreen(width, height int) string {
	overlay := RenderHelpOverlay(m.styles)
	w := width - 4
	if w > 60 {
		w = 60
	}
	box := m.styles.PaneBorder.Width(w).Render(overlay)
	return lg.Place(width, height, lg.Center, lg.Center, box)
}

// Compile-time assertion that *Model values returned by Update are
// still tea.Model implementations.
var _ tea.Model = Model{}

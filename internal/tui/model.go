package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

type Storer interface {
	Save(ctx context.Context, w *world.World) error
}

type Stepper interface {
	Step(ctx context.Context) (StepSummary, error)
}

type MemoryAccessor interface {
	MemorySnapshot() MemorySnapshot
}

type Interveneable interface {
	InterveneCreate(typeLabel string) (string, error)
	InterveneDestroy(entityID uint64) (string, error)
	InterveneSpeak(content string) (string, error)
}

type AnnalsAccessor interface {
	Annals() []ChapterSummary
}

type ChapterSummary struct {
	Tick    uint64
	Content string
}

type AgentInfo struct {
	ID        uint64
	Name      string
	IsCreator bool
	Drives    map[string]float64
	ParentID  uint64
	Dead      bool
}

type AgentLister interface {
	Agents() []AgentInfo
	FocusedAgentID() uint64
	SetFocusedAgentID(id uint64)
}

type WorldInfo struct {
	Index       int
	Name        string
	Tick        uint64
	EntityCount int
	AgentCount  int
}

type UniverseLister interface {
	Worlds() []WorldInfo
	FocusedWorldIdx() int
	SetFocusedWorldIdx(i int)
	CycleFocusedWorld(delta int)
}

type WorldProvider interface {
	FocusedWorld() *world.World
}

type StepSummary struct {
	Tick       uint64
	Skipped    bool
	Thought    string
	ToolName   string
	ToolResult string
	ToolErr    error
}

type Model struct {
	world  *world.World
	store  Storer
	sim    Stepper
	styles Styles
	keys   KeyMap

	width  int
	height int

	paused          bool
	helpVisible     bool
	memoryVisible   bool
	lineageVisible  bool
	annalsVisible   bool
	interveneActive bool

	tickInterval time.Duration
	lastStep     StepSummary
	stepInFlight bool
	skippedTicks int

	message     string
	messageTime time.Time

	dummyCursor int
}

var dummyTypes = []string{"planet", "ocean", "continent", "mountain", "forest", "star", "creature", "idea"}

type flashTimeoutMsg time.Time

type simTickMsg struct{}

type stepCompletedMsg struct {
	Summary StepSummary
	Err     error
}

const DefaultTickInterval = 1500 * time.Millisecond

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

func (m Model) WithTickInterval(d time.Duration) Model {
	if d <= 0 {
		d = DefaultTickInterval
	}
	m.tickInterval = d
	return m
}

func (m Model) LastStep() StepSummary { return m.lastStep }

func (m Model) World() *world.World { return m.world }

func (m Model) focusedWorld() *world.World {
	if wp, ok := m.sim.(WorldProvider); ok && wp != nil {
		if w := wp.FocusedWorld(); w != nil {
			return w
		}
	}
	return m.world
}

func (m Model) IsPaused() bool { return m.paused }

func (m Model) HelpVisible() bool { return m.helpVisible }

func (m Model) MemoryVisible() bool { return m.memoryVisible }

func (m Model) Message() string { return m.message }

func (m Model) Init() tea.Cmd {
	if m.sim == nil {
		return nil
	}
	return m.scheduleTick()
}

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
	if m.interveneActive {
		return m.handleInterveneChord(key)
	}
	if mm, cmd, ok := m.handleToggleKey(key); ok {
		return mm, cmd
	}
	return m.handleActionKey(key)
}

func (m Model) handleToggleKey(key string) (tea.Model, tea.Cmd, bool) {
	switch {
	case Matches(key, m.keys.Quit):
		return m, tea.Quit, true
	case Matches(key, m.keys.Help):
		m.helpVisible = !m.helpVisible
	case Matches(key, m.keys.MemoryToggle):
		m.memoryVisible = !m.memoryVisible
	case Matches(key, m.keys.LineageToggle):
		m.lineageVisible = !m.lineageVisible
	case Matches(key, m.keys.AnnalsToggle):
		m.annalsVisible = !m.annalsVisible
	case Matches(key, m.keys.FocusNext):
		m.cycleFocus(+1)
	case Matches(key, m.keys.FocusPrev):
		m.cycleFocus(-1)
	case Matches(key, m.keys.WorldNext):
		m.cycleWorld(+1)
	case Matches(key, m.keys.WorldPrev):
		m.cycleWorld(-1)
	case Matches(key, m.keys.TogglePause):
		m.paused = !m.paused
	default:
		return m, nil, false
	}
	return m, nil, true
}

func (m Model) handleActionKey(key string) (tea.Model, tea.Cmd) {
	switch {
	case Matches(key, m.keys.Intervene):
		m.interveneActive = true
		return m.flash("intervene: c=create / d=destroy / s=speak / esc=cancel")
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

func (m Model) handleInterveneChord(key string) (tea.Model, tea.Cmd) {
	m.interveneActive = false
	iv, ok := m.sim.(Interveneable)
	if !ok {
		return m.flash("intervene: sim does not support interventions")
	}
	switch key {
	case "esc":
		return m.flash("intervene: cancelled")
	case "c":
		t := dummyTypes[m.dummyCursor%len(dummyTypes)]
		m.dummyCursor++
		out, err := iv.InterveneCreate(t)
		if err != nil {
			return m.flash(fmt.Sprintf("intervene: %v", err))
		}
		return m.flash(out)
	case "d":
		live := m.focusedWorld().Entities()
		if len(live) == 0 {
			return m.flash("intervene: nothing to destroy")
		}
		// Don't destroy agents; pick the most recent non-agent.
		var targetID world.EntityID
		for i := len(live) - 1; i >= 0; i-- {
			if live[i].TypeLabel != "agent" {
				targetID = live[i].ID
				break
			}
		}
		if targetID == 0 {
			return m.flash("intervene: no non-agent targets")
		}
		out, err := iv.InterveneDestroy(uint64(targetID))
		if err != nil {
			return m.flash(fmt.Sprintf("intervene: %v", err))
		}
		return m.flash(out)
	case "s":
		out, err := iv.InterveneSpeak("the void stirs.")
		if err != nil {
			return m.flash(fmt.Sprintf("intervene: %v", err))
		}
		return m.flash(out)
	default:
		return m.flash("intervene: unknown action, press c/d/s/esc")
	}
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

func (m Model) View() tea.View {
	v := tea.NewView(m.Render())
	v.AltScreen = true
	return v
}

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
	if m.lineageVisible {
		return m.renderLineageScreen(w, h)
	}
	if m.annalsVisible {
		return m.renderAnnalsScreen(w, h)
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

func (m Model) renderLineageScreen(width, height int) string {
	var agents []AgentInfo
	if lister, ok := m.sim.(AgentLister); ok {
		agents = lister.Agents()
	}
	body := RenderLineage(agents, m.styles)
	w := width - 4
	if w > 80 {
		w = 80
	}
	box := m.styles.PaneBorder.Width(w).Render(body)
	return lg.Place(width, height, lg.Center, lg.Center, box)
}

func (m Model) renderAnnalsScreen(width, height int) string {
	var chapters []ChapterSummary
	if accessor, ok := m.sim.(AnnalsAccessor); ok {
		chapters = accessor.Annals()
	}
	body := RenderAnnals(chapters, m.styles)
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

var _ tea.Model = Model{}

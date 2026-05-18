package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// RunOptions configures a TUI session.
type RunOptions struct {
	// World is the world the TUI is attached to. Required.
	World *world.World

	// Store is optional; when nil the save keybind reports that no
	// store is configured.
	Store Storer

	// Sim is optional; when nil the TUI runs as a passive viewer.
	// When set, the model schedules sim ticks at the configured
	// interval and unpause runs the creator.
	Sim Stepper

	// TickInterval overrides the default sim tick interval. Zero
	// means use DefaultTickInterval.
	TickInterval time.Duration

	// Input and Output let tests inject a virtual terminal. When nil,
	// bubbletea defaults to os.Stdin / os.Stdout.
	Input  io.Reader
	Output io.Writer
}

// Run blocks while the TUI is on screen and returns once the user
// quits. ctx cancellation triggers a clean shutdown.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.World == nil {
		return fmt.Errorf("tui: World is required")
	}
	m := NewModel(opts.World, opts.Store, opts.Sim)
	if opts.TickInterval > 0 {
		m = m.WithTickInterval(opts.TickInterval)
	}
	var progOpts []tea.ProgramOption
	if opts.Input != nil {
		progOpts = append(progOpts, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		progOpts = append(progOpts, tea.WithOutput(opts.Output))
	}
	progOpts = append(progOpts, tea.WithContext(ctx))
	p := tea.NewProgram(m, progOpts...)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: run: %w", err)
	}
	return nil
}

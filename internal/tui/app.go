package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

type RunOptions struct {
	World *world.World

	Store Storer

	Sim Stepper

	TickInterval time.Duration

	Input  io.Reader
	Output io.Writer
}

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

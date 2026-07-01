package webui

import (
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/sim"
)

type broker struct {
	mu          sync.Mutex
	subscribers map[chan sim.StepResult]struct{}
}

func newBroker() *broker {
	return &broker{subscribers: make(map[chan sim.StepResult]struct{})}
}

func (b *broker) subscribe() (chan sim.StepResult, func()) {
	ch := make(chan sim.StepResult, 32)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subscribers[ch]; ok {
			delete(b.subscribers, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

func (b *broker) publish(res sim.StepResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- res:
		default:
			// drop; UI re-syncs via /api/state on next event
		}
	}
}

func (b *broker) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		delete(b.subscribers, ch)
		close(ch)
	}
}

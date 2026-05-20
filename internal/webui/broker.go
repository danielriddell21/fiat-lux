package webui

import (
	"sync"

	"github.com/danielriddell21/fiat-lux/internal/sim"
)

// broker fans out StepResult notifications to one channel per SSE
// subscriber. Sends are non-blocking: a slow subscriber drops events
// rather than back-pressuring the sim. Clients are expected to
// re-fetch /api/state on each event to recover from any drops.
type broker struct {
	mu          sync.Mutex
	subscribers map[chan sim.StepResult]struct{}
}

func newBroker() *broker {
	return &broker{subscribers: make(map[chan sim.StepResult]struct{})}
}

// subscribe registers a new SSE consumer. The returned channel is
// buffered; the cleanup func unsubscribes and closes the channel.
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

// publish is the Sim.Observer-shaped callback.
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

// closeAll boots every subscriber. Used on server shutdown so
// blocked handlers wake and return.
func (b *broker) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		delete(b.subscribers, ch)
		close(ch)
	}
}

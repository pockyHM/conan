package subagent

import (
	"context"
	"sync"

	"github.com/pockyHM/conan/pkg/models"
)

type Manager struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewManager() *Manager {
	return &Manager{running: map[string]context.CancelFunc{}}
}

func (m *Manager) Submit(ctx context.Context, runner Runner, req Request) (string, <-chan Event, <-chan Result, error) {
	id := req.ID
	if id == "" {
		id = models.NewID()
	}

	subCtx, cancel := context.WithCancel(ctx)
	events, results := runner.Run(subCtx, req)

	m.mu.Lock()
	m.running[id] = cancel
	m.mu.Unlock()

	// Drain the events channel so the cleanup is triggered when the runner
	// finishes (events and results are both closed by the same defers in
	// runner.Run). We deliberately do not consume from results: results is
	// a buffered channel of size 1 and the caller expects to receive the
	// single result value, so a competing receive here would race with the
	// caller and could deliver a zero-value Result to the caller.
	go func() {
		for range events {
		}
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	return id, events, results, nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	cancel, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	cancel()
	return nil
}

func (m *Manager) CancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.running))
	for _, c := range m.running {
		cancels = append(cancels, c)
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (m *Manager) Running() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for id := range m.running {
		out = append(out, id)
	}
	return out
}

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

// Submit starts a subagent and returns channels for the caller to observe
// its progress. The returned events/result channels are the same ones the
// runner writes to — there is no tee or forwarder. The Manager tracks a
// per-id cancel func so Cancel(id) can stop a specific in-flight runner.
// The m.running map is allowed to retain stale entries for runners that
// finish naturally: their cancel funcs are no-ops at that point, and
// Cancel/Submit use the map as a lookup, not a strict live-set. This is
// the simplest correct design and avoids the channel-consumption race
// that a cleanup goroutine would introduce.
func (m *Manager) Submit(ctx context.Context, runner Runner, req Request) (string, <-chan Event, <-chan Result, error) {
	id := req.ID
	if id == "" {
		id = models.NewID()
		req.ID = id
	}

	subCtx, cancel := context.WithCancel(ctx)
	events, results := runner.Run(subCtx, req)

	m.mu.Lock()
	m.running[id] = cancel
	m.mu.Unlock()

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

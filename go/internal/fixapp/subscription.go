package fixapp

import (
	"context"
	"sync"
)

type subscription struct {
	manager *Manager
	mu      sync.Mutex
	notify  <-chan struct{}
	closed  chan struct{}
	once    sync.Once
}

// Wait blocks for the next coalesced state-change notification. The next
// channel is captured before returning, so changes cannot be lost between a
// UI refresh and its following wait.
func (value *subscription) Wait(ctx context.Context) error {
	value.mu.Lock()
	notify := value.notify
	value.mu.Unlock()
	select {
	case <-notify:
		value.manager.notifyMu.Lock()
		next := value.manager.notify
		value.manager.notifyMu.Unlock()
		value.mu.Lock()
		value.notify = next
		value.mu.Unlock()
		return nil
	case <-value.closed:
		return ErrClosed
	case <-value.manager.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (value *subscription) Close() error {
	value.once.Do(func() { close(value.closed) })
	return nil
}

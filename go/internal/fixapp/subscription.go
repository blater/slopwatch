package fixapp

import (
	"context"
	"sync"
)

type subscription struct {
	manager *Manager
	closed  chan struct{}
	once    sync.Once
}

// Wait is level-triggered: it returns immediately when the current revision
// is newer, otherwise waits for a publication without a check/subscribe race.
func (value *subscription) Wait(ctx context.Context, after GlobalRevision) (GlobalRevision, error) {
	for {
		if value.manager.closed.Load() {
			return value.manager.Jobs(JobFilter{IncludeArchived: true}).Revision, ErrClosed
		}
		current := value.manager.Jobs(JobFilter{IncludeArchived: true}).Revision
		if current > after {
			return current, nil
		}
		value.manager.notifyMu.Lock()
		notify := value.manager.notify
		current = value.manager.current.Load().(JobListSnapshot).Revision
		value.manager.notifyMu.Unlock()
		if current > after {
			return current, nil
		}
		select {
		case <-notify:
			continue
		case <-value.closed:
			return current, ErrClosed
		case <-value.manager.done:
			return current, ErrClosed
		case <-ctx.Done():
			return current, ctx.Err()
		}
	}
}

func (value *subscription) Close() error {
	value.once.Do(func() { close(value.closed) })
	return nil
}

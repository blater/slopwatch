package workspace

import (
	"context"
	"time"
)

// Wake returns a coalesced notification channel. The channel is not an event
// log; callers must call Drain or WaitAndDrain to obtain authoritative paths.
func (m *Monitor) Wake() <-chan struct{} { return m.engine.events.wake }

// Drain atomically takes all currently pending work. It never waits.
func (m *Monitor) Drain() DirtyBatch { return m.engine.events.dirty.snapshot() }

// WaitAndDrain waits for a wake and then for the configured quiet period. It
// also handles a wake that was coalesced before the caller began waiting.
func (m *Monitor) WaitAndDrain(ctx context.Context) (DirtyBatch, error) {
	if m.engine.events.dirty.pending() {
		return m.engine.events.quietDrain(ctx)
	}
	select {
	case <-ctx.Done():
		return DirtyBatch{}, ctx.Err()
	case <-m.engine.events.done:
		return DirtyBatch{}, context.Canceled
	case <-m.engine.events.wake:
		return m.engine.events.quietDrain(ctx)
	}
}

func (m *eventManager) quietDrain(ctx context.Context) (DirtyBatch, error) {
	timer := time.NewTimer(m.debounce)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return DirtyBatch{}, ctx.Err()
		case <-m.done:
			return DirtyBatch{}, context.Canceled
		case <-m.wake:
			resetTimer(timer, m.debounce)
		case <-timer.C:
			if batch := m.dirty.snapshot(); !batch.Empty() {
				return batch, nil
			}
			if !m.dirty.pending() {
				return DirtyBatch{}, nil
			}
			timer.Reset(m.debounce)
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

// Close stops event processing and releases the backend. It is safe to call
// more than once. Pending dirty state remains drainable after Close.
func (m *Monitor) Close() error {
	var err error
	m.closeOnce.Do(func() {
		close(m.engine.events.done)
		err = m.engine.watch.backend.Close()
	})
	return err
}

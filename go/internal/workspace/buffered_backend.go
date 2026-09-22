package workspace

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

var ErrNotificationLoss = errors.New("filesystem notification details were lost")

type bufferedBackend struct {
	delivered chan struct{}
	Backend
	mu          sync.Mutex
	lossVersion atomic.Uint64
	pending     map[string]Event
	loss        bool
	wake        chan struct{}
	done        chan struct{}
	stopped     chan struct{}
	events      chan Event
	failures    chan error
	filter      func(Event) bool
	closeOnce   sync.Once
}

func bufferBackend(backend Backend, filter func(Event) bool) *bufferedBackend {
	b := &bufferedBackend{Backend: backend, pending: map[string]Event{}, wake: make(chan struct{}, 1), done: make(chan struct{}), stopped: make(chan struct{}), delivered: make(chan struct{}), events: make(chan Event), failures: make(chan error, 16), filter: filter}
	go b.collect()
	go b.deliver()
	return b
}
func (b *bufferedBackend) Events() <-chan Event { return b.events }
func (b *bufferedBackend) Errors() <-chan error { return b.failures }
func (b *bufferedBackend) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *bufferedBackend) admit(event Event) {
	if b.filter != nil && !b.filter(event) {
		return
	}
	b.mu.Lock()
	old, exists := b.pending[event.Name]
	if exists {
		event.Op |= old.Op
		event.IsDir = event.IsDir || old.IsDir
		event.Uncertain = event.Uncertain && old.Uncertain
	}
	b.pending[event.Name] = event
	b.mu.Unlock()
	b.signal()
}
func (b *bufferedBackend) collect() {
	defer close(b.stopped)
	events, failures := b.Backend.Events(), b.Backend.Errors()
	for events != nil || failures != nil {
		select {
		case <-b.done:
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			b.admit(event)
		case err, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) || errors.Is(err, ErrNotificationLoss) {
				b.mu.Lock()
				b.loss = true
				b.lossVersion.Add(1)
				b.mu.Unlock()
				b.signal()
				continue
			}
			select {
			case b.failures <- err:
			case <-b.done:
				return
			default:
				b.mu.Lock()
				b.loss = true
				b.lossVersion.Add(1)
				b.mu.Unlock()
				b.signal()
			}
		}
	}
	b.signal()
}
func (b *bufferedBackend) deliver() {
	defer close(b.events)
	defer close(b.delivered)
	for {
		b.mu.Lock()
		loss := b.loss
		if loss {
			b.loss = false
		}
		var event Event
		for path, next := range b.pending {
			event = next
			if !loss {
				delete(b.pending, path)
			}
			break
		}
		b.mu.Unlock()
		if loss {
			select {
			case b.failures <- ErrNotificationLoss:
			case <-b.done:
				return
			}
			continue
		}
		if event.Name != "" {
			select {
			case b.events <- event:
			case <-b.done:
				return
			}
			continue
		}
		select {
		case <-b.done:
			return
		case <-b.wake:
		case <-b.stopped:
			return
		}
	}
}
func (b *bufferedBackend) Close() (err error) {
	b.closeOnce.Do(func() { close(b.done); err = b.Backend.Close(); <-b.stopped; <-b.delivered })
	return err
}

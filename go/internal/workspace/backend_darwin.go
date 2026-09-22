//go:build darwin

package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
)

type nativeSubscription struct {
	physical string
	logical  string
	stream   *fsevents.EventStream
}

// FSEvents watches hierarchies without opening their individual files.
type fseventsBackend struct {
	mu            sync.Mutex
	registeredMu  sync.RWMutex
	registered    map[string]os.FileInfo
	subscriptions []nativeSubscription
	events        chan Event
	errors        chan error
	closing       chan struct{}
	done          chan struct{}
	workers       sync.WaitGroup
	closeOnce     sync.Once
	closed        bool
}

func newPlatformBackend() (Backend, error) {
	return &fseventsBackend{registered: map[string]os.FileInfo{}, events: make(chan Event), errors: make(chan error), closing: make(chan struct{}), done: make(chan struct{})}, nil
}

func (b *fseventsBackend) Events() <-chan Event { return b.events }
func (b *fseventsBackend) Errors() <-chan error { return b.errors }

func (b *fseventsBackend) Add(path string) error {
	logical := filepath.Clean(path)
	physical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return err
	}
	info, err := os.Stat(logical)
	if err != nil {
		return err
	}
	b.registeredMu.Lock()
	b.registered[logical] = info
	b.registeredMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return os.ErrClosed
	}
	for _, subscription := range b.subscriptions {
		rel, err := filepath.Rel(subscription.physical, physical)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && filepath.Join(subscription.logical, rel) == logical {
			return nil
		}
	}
	raw := make(chan []fsevents.Event)
	stream := &fsevents.EventStream{Paths: []string{physical}, Events: raw, Flags: fsevents.FileEvents | fsevents.NoDefer | fsevents.WatchRoot, Latency: 50 * time.Millisecond}
	subscription := nativeSubscription{physical: physical, logical: logical, stream: stream}
	// Receive before Start: the library's callback must never wait for inventory
	// traversal or analysis to begin draining its channel.
	failed := make(chan struct{})
	b.workers.Add(1)
	go b.forward(subscription, raw, failed)
	if err := stream.Start(); err != nil {
		close(failed)
		return err
	}
	b.subscriptions = append(b.subscriptions, subscription)
	return nil
}

func (b *fseventsBackend) forward(subscription nativeSubscription, raw <-chan []fsevents.Event, failed <-chan struct{}) {
	defer b.workers.Done()
	for {
		select {
		case <-b.done:
			return
		case <-failed:
			return
		case batch := <-raw:
			for _, native := range batch {
				if native.Flags&(fsevents.MustScanSubDirs|fsevents.UserDropped|fsevents.KernelDropped) != 0 {
					select {
					case b.errors <- ErrNotificationLoss:
					case <-b.closing:
					}
					continue
				}
				op := Op(0)
				if native.Flags&fsevents.ItemCreated != 0 {
					op |= OpCreate
				}
				if native.Flags&fsevents.ItemRemoved != 0 {
					op |= OpRemove
				}
				if native.Flags&(fsevents.ItemRenamed|fsevents.RootChanged) != 0 {
					op |= OpRename
				}
				if native.Flags&(fsevents.ItemModified|fsevents.ItemInodeMetaMod|fsevents.ItemChangeOwner|fsevents.ItemXattrMod) != 0 {
					op |= OpWrite
				}
				if op == 0 {
					continue
				}
				rel, err := filepath.Rel(subscription.physical, filepath.Clean(native.Path))
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					continue
				}
				event := Event{Name: filepath.Join(subscription.logical, rel), Op: op, IsDir: native.Flags&fsevents.ItemIsDir != 0, Uncertain: true}
				// FSEvents can combine old and new rename flags. Only treat a
				// still-present directory as replaced when its identity changed.
				// This preserves replacement handling without rescanning an
				// unchanged directory on every coalesced native notification.
				b.registeredMu.RLock()
				previous := b.registered[event.Name]
				b.registeredMu.RUnlock()
				if previous != nil {
					if current, err := os.Stat(event.Name); err == nil && !os.SameFile(previous, current) {
						event.Uncertain = false
						event.Op |= OpRename
					}
				}
				select {
				case b.events <- event:
				case <-b.closing:
				}
			}
		}
	}
}

// Child registrations are inventory bookkeeping; the native root subscription
// remains for the session and also observes later recreations.
func (b *fseventsBackend) Remove(path string) error {
	b.registeredMu.Lock()
	delete(b.registered, filepath.Clean(path))
	b.registeredMu.Unlock()
	return nil
}

func (b *fseventsBackend) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		close(b.closing)
		// Keep the receiver draining until native callbacks have stopped.
		for _, subscription := range b.subscriptions {
			subscription.stream.Stop()
		}
		close(b.done)
		b.mu.Unlock()
		b.workers.Wait()
		close(b.events)
		close(b.errors)
	})
	return nil
}

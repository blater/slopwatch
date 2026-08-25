package workspace

import "github.com/fsnotify/fsnotify"

func (b *fsnotifyBackend) Add(path string) error { return b.watcher.Add(path) }
func (b *fsnotifyBackend) Events() <-chan Event  { return b.events }
func (b *fsnotifyBackend) Errors() <-chan error  { return b.errors }

func (b *fsnotifyBackend) forward() {
	defer close(b.events)
	defer close(b.errors)
	for {
		select {
		case <-b.done:
			return
		case event, ok := <-b.watcher.Events:
			if !ok {
				return
			}
			if op := fsnotifyOperation(event.Op); op != 0 {
				select {
				case b.events <- Event{Name: event.Name, Op: op}:
				case <-b.done:
					return
				}
			}
		case err, ok := <-b.watcher.Errors:
			if !ok {
				return
			}
			select {
			case b.errors <- err:
			case <-b.done:
				return
			}
		}
	}
}

func fsnotifyOperation(operation fsnotify.Op) Op {
	var result Op
	if operation&fsnotify.Create != 0 {
		result |= OpCreate
	}
	if operation&fsnotify.Write != 0 {
		result |= OpWrite
	}
	if operation&fsnotify.Remove != 0 {
		result |= OpRemove
	}
	if operation&fsnotify.Rename != 0 {
		result |= OpRename
	}
	return result
}

func (b *fsnotifyBackend) Close() (err error) {
	b.closeOnce.Do(func() {
		close(b.done)
		err = b.watcher.Close()
	})
	return err
}

//go:build !darwin

package workspace

import "github.com/fsnotify/fsnotify"

func newPlatformBackend() (Backend, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	b := &fsnotifyBackend{watcher: w, events: make(chan Event), errors: make(chan error), done: make(chan struct{})}
	go b.forward()
	return b, nil
}

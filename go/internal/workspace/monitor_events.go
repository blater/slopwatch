package workspace

import (
	"errors"
	"fmt"
	"os"
)

func (m *Monitor) handle(event Event) { m.engine.events.handle(event) }
func (m *eventManager) handle(event Event) {
	if event.Name == "" || event.Op == 0 {
		return
	}
	path := m.paths.absolute(event.Name)
	reason := eventReason(event.Op)
	if reason == 0 {
		return
	}
	wasDirectory := m.watch.isWatched(path)
	info, err := m.watch.fs.Lstat(path)
	ignoredLink := err == nil && info.Mode()&os.ModeSymlink != 0 && !m.watch.followSymlinks && !m.paths.explicitTarget(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 && !ignoredLink {
		info, err = m.watch.fs.Stat(path)
	}
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.markError(err, false)
		return
	}
	isDirectory := exists && !ignoredLink && info.IsDir()
	replacement := event.Op&(OpRemove|OpRename) != 0
	if wasDirectory && (!isDirectory || replacement) {
		paths, removeErr := m.watch.removeTree(path)
		for _, old := range paths {
			m.markPath(old, reason|ReasonDirectory, false)
		}
		if removeErr != nil {
			m.markError(removeErr, false)
		}
	}
	if path == m.paths.root && wasDirectory && (!isDirectory || replacement) {
		m.markError(fmt.Errorf("workspace root was removed or replaced; restart required: %s", path), true)
		return
	}
	if ignoredLink {
		m.watch.mu.RLock()
		_, known := m.watch.known[path]
		m.watch.mu.RUnlock()
		if known {
			m.markPath(path, reason, false)
			m.watch.forget(path)
		}
		return
	}
	if isDirectory {
		m.watch.mu.RLock()
		_, wasFile := m.watch.known[path]
		m.watch.mu.RUnlock()
		if wasFile {
			m.markPath(path, reason, false)
			m.watch.forget(path)
		}
		// A root notification is never authority to rediscover the workspace.
		if path == m.paths.root {
			return
		}
		if event.Op&OpCreate != 0 || replacement || !wasDirectory {
			m.handleCreatedDirectory(path)
		}
		return
	}
	if wasDirectory && !exists {
		return
	}
	m.markPath(path, reason, false)
	if exists {
		m.watch.remember(path)
	} else {
		m.watch.forget(path)
	}
}
func eventReason(operation Op) Reason {
	var reason Reason
	if operation&OpWrite != 0 {
		reason |= ReasonWrite
	}
	if operation&OpCreate != 0 {
		reason |= ReasonCreate
	}
	if operation&OpRemove != 0 {
		reason |= ReasonRemove
	}
	if operation&OpRename != 0 {
		reason |= ReasonRename
	}
	return reason
}

func (m *pathPolicy) explicitTarget(path string) bool {
	for _, scope := range m.scopes {
		if scope.Path == path {
			return true
		}
	}
	for _, input := range m.inputs {
		if input.Path == path {
			return true
		}
	}
	return false
}

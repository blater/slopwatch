package workspace

import "os"

func (m *Monitor) handle(event Event) { m.engine.events.handle(event) }

func (m *eventManager) handle(event Event) {
	if event.Name == "" || event.Op == 0 {
		return
	}
	path := m.paths.absolute(event.Name)
	if m.handleDirectoryEvent(path, event) {
		return
	}
	reason := eventReason(event.Op)
	if reason == 0 {
		return
	}
	m.markPath(path, reason, false)
	if event.Op&(OpRemove|OpRename) != 0 {
		m.watch.mu.Lock()
		delete(m.watch.known, path)
		m.watch.mu.Unlock()
	} else {
		m.watch.remember(path)
	}
}

func (m *eventManager) handleDirectoryEvent(path string, event Event) bool {
	isDirectory := event.IsDir || m.watch.isWatched(path)
	if !isDirectory {
		if info, err := os.Stat(path); err == nil {
			isDirectory = info.IsDir()
		}
	}
	if !isDirectory {
		return false
	}
	if event.Op&(OpRemove|OpRename) != 0 {
		paths, err := m.watch.removeTree(path)
		for _, old := range paths {
			m.markPath(old, eventReason(event.Op)|ReasonDirectory, false)
		}
		if err != nil {
			m.markError(err, false)
		}
	} else if event.Op&OpCreate != 0 {
		m.handleCreatedDirectory(path)
	}
	return true
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

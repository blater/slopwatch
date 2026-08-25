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
}

func (m *eventManager) handleDirectoryEvent(path string, event Event) bool {
	if event.IsDir && event.Op&OpCreate != 0 {
		m.handleCreatedDirectory(path)
		return true
	}
	if event.IsDir || (event.Op&(OpRemove|OpRename) != 0 && m.watch.isWatched(path)) {
		m.markAll(eventReason(event.Op) | ReasonDirectory)
		return true
	}
	if event.Op&OpCreate == 0 {
		return false
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		m.handleCreatedDirectory(path)
		return true
	}
	return false
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

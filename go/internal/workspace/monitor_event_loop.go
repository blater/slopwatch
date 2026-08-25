package workspace

func (m *eventManager) run() {
	events := m.watch.backend.Events()
	errors := m.watch.backend.Errors()
	for {
		select {
		case <-m.done:
			return
		case event, ok := <-events:
			if !ok {
				m.markAll(ReasonWatcherError)
				return
			}
			m.handle(event)
		case _, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			m.markAll(ReasonWatcherError)
		}
	}
}

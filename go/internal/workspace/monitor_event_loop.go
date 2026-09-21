package workspace

import "io"

func (m *eventManager) run() {
	defer close(m.stopped)
	events := m.watch.backend.Events()
	errors := m.watch.backend.Errors()
	for {
		select {
		case <-m.done:
			return
		case event, ok := <-events:
			if !ok {
				m.markError(io.EOF, true)
				return
			}
			m.handle(event)
		case err, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			m.markError(err, false)
		}
	}
}

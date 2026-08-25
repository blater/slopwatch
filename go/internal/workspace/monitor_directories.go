package workspace

import (
	"path/filepath"
	"strings"
)

func (m *eventManager) handleCreatedDirectory(path string) {
	if m.paths.inScope(path) {
		// Register before marking: files created with the directory are covered
		// even when notifications are coalesced.
		if err := m.watch.addTree(path); err != nil {
			m.markAll(ReasonWatcherError | ReasonDirectory)
			return
		}
		m.markAll(ReasonCreate | ReasonDirectory)
		return
	}
	// A newly-created directory can be an intermediate component of an exact
	// input that did not exist at startup. Extend the watch chain without
	// treating the directory itself as an analysis change.
	if err := m.registerConfiguredTargetsBelow(path); err != nil {
		m.markAll(ReasonWatcherError | ReasonDirectory)
	}
}

func (m *eventManager) registerConfiguredTargetsBelow(directory string) error {
	for _, scope := range m.paths.scopes {
		if isStrictAncestor(directory, scope.Path) {
			if err := m.watch.registerTarget(scope.Path, scope.Recursive); err != nil {
				return err
			}
		}
	}
	for _, input := range m.paths.inputs {
		if isStrictAncestor(directory, input.Path) {
			if err := m.watch.registerTarget(input.Path, input.Recursive); err != nil {
				return err
			}
		}
	}
	return nil
}

func isStrictAncestor(directory, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

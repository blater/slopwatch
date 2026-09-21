package workspace

import (
	"path/filepath"
	"strings"
)

func (m *eventManager) handleCreatedDirectory(path string) {
	if m.watch.ignoreDir != nil && m.watch.ignoreDir(path, filepath.Base(path)) {
		return
	}
	// Registration doubles as discovery; do not scan the workspace again.
	var err error
	recursive := false
	for _, scope := range m.paths.scopes {
		if scope.Recursive && contains(scope, path) {
			recursive = true
		}
	}
	for _, input := range m.paths.inputs {
		if input.Recursive && contains(Scope{Path: input.Path, Recursive: true}, path) {
			recursive = true
		}
	}
	if recursive {
		err = m.watch.addTree(path)
	} else {
		err = m.registerConfiguredTargetsBelow(path)
	}
	m.watch.mu.RLock()
	var paths []string
	for known := range m.watch.known {
		if known == path || isStrictAncestor(path, known) {
			paths = append(paths, known)
		}
	}
	m.watch.mu.RUnlock()
	for _, known := range paths {
		m.markPath(known, ReasonCreate|ReasonDirectory, false)
	}
	if err != nil {
		m.markError(err, false)
	}
}

func (m *eventManager) registerConfiguredTargetsBelow(directory string) error {
	for _, scope := range m.paths.scopes {
		if directory == scope.Path || isStrictAncestor(directory, scope.Path) {
			if err := m.watch.registerTarget(scope.Path, scope.Recursive); err != nil {
				return err
			}
		}
	}
	for _, input := range m.paths.inputs {
		if directory == input.Path || isStrictAncestor(directory, input.Path) {
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

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
	var paths []string
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
		paths, err = m.watch.addTree(path)
	} else {
		paths, err = m.registerConfiguredTargetsBelow(path)
	}
	for _, known := range paths {
		m.markPath(known, ReasonCreate|ReasonDirectory, false)
	}
	if err != nil {
		m.markError(err, false)
	}
}

func (m *eventManager) registerConfiguredTargetsBelow(directory string) ([]string, error) {
	var paths []string
	for _, scope := range m.paths.scopes {
		if directory == scope.Path || isStrictAncestor(directory, scope.Path) {
			found, err := m.watch.registerTarget(scope.Path, scope.Recursive)
			paths = append(paths, found...)
			if err != nil {
				return paths, err
			}
		}
	}
	for _, input := range m.paths.inputs {
		if directory == input.Path || isStrictAncestor(directory, input.Path) {
			found, err := m.watch.registerTarget(input.Path, input.Recursive)
			paths = append(paths, found...)
			if err != nil {
				return paths, err
			}
		}
	}
	return paths, nil
}

func isStrictAncestor(directory, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

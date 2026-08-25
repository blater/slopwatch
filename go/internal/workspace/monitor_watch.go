package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Monitor) isWatched(path string) bool { return m.engine.watch.isWatched(path) }

func (m *watchManager) registerAll(scopes []Scope, inputs []Input) error {
	for _, scope := range scopes {
		if err := m.registerTarget(scope.Path, scope.Recursive); err != nil {
			return err
		}
	}
	for _, input := range inputs {
		if err := m.registerTarget(input.Path, input.Recursive); err != nil {
			return err
		}
	}
	return nil
}

func (m *watchManager) registerTarget(path string, recursive bool) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m.watchNearestExistingAncestor(path)
		}
		return err
	}
	if !info.IsDir() {
		return m.watch(filepath.Dir(path))
	}
	if recursive {
		return m.addTree(path)
	}
	return m.watch(path)
}

func (m *watchManager) watchNearestExistingAncestor(path string) error {
	for candidate := filepath.Dir(path); ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return m.watch(candidate)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return fmt.Errorf("no existing directory contains watch target %s", path)
		}
	}
}

func (m *watchManager) watch(path string) error {
	if m.isWatched(path) {
		return nil
	}
	if err := m.backend.Add(path); err != nil {
		return err
	}
	m.mu.Lock()
	m.watched[filepath.Clean(path)] = struct{}{}
	m.mu.Unlock()
	return nil
}

func (m *watchManager) addTree(root string) error {
	return m.addDirectoryTree(root, map[string]bool{})
}

func (m *watchManager) addDirectoryTree(directory string, visited map[string]bool) error {
	if m.followSymlinks {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return err
		}
		if visited[resolved] {
			return nil
		}
		visited[resolved] = true
	}
	if err := m.watch(directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		isDirectory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if !m.followSymlinks {
				continue
			}
			metadata, err := os.Stat(path)
			if err != nil {
				return err
			}
			isDirectory = metadata.IsDir()
		}
		if !isDirectory || m.ignoreDir != nil && m.ignoreDir(path, entry.Name()) {
			continue
		}
		if err := m.addDirectoryTree(path, visited); err != nil {
			return err
		}
	}
	return nil
}

func (m *watchManager) isWatched(path string) bool {
	m.mu.RLock()
	_, ok := m.watched[filepath.Clean(path)]
	m.mu.RUnlock()
	return ok
}

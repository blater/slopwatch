package workspace

import (
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
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
		m.remember(path)
		return m.watch(filepath.Dir(path))
	}
	if recursive {
		return m.addTree(path)
	}
	if err := m.watch(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			m.remember(filepath.Join(path, entry.Name()))
		}
	}
	return nil
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
	if m.ignoreDir != nil && m.ignoreDir(root, filepath.Base(root)) {
		return nil
	}
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
		if !isDirectory {
			m.remember(path)
			continue
		}
		if m.ignoreDir != nil && m.ignoreDir(path, entry.Name()) {
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

func (m *watchManager) remember(path string) {
	classification, ok := m.paths.classification(path, false)
	if !ok {
		return
	}
	m.mu.Lock()
	m.known[path] = classification
	m.mu.Unlock()
}

func (m *watchManager) removeTree(path string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var paths []string
	for known := range m.known {
		if known == path || isStrictAncestor(path, known) {
			paths = append(paths, known)
			delete(m.known, known)
		}
	}
	var failures error
	for watched := range m.watched {
		if watched == path || isStrictAncestor(path, watched) {
			if err := m.backend.Remove(watched); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) && !errors.Is(err, os.ErrNotExist) {
				failures = errors.Join(failures, err)
			}
			delete(m.watched, watched)
		}
	}
	return paths, failures
}

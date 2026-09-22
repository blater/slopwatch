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
		if _, err := m.registerTarget(scope.Path, scope.Recursive); err != nil {
			return err
		}
	}
	for _, input := range inputs {
		if _, err := m.registerTarget(input.Path, input.Recursive); err != nil {
			return err
		}
	}
	return nil
}
func (m *watchManager) registerTarget(path string, recursive bool) ([]string, error) {
	info, err := m.fs.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, m.watchNearestExistingAncestor(path)
		}
		return nil, err
	}
	if !info.IsDir() {
		paths := m.remember(path)
		return paths, m.watch(filepath.Dir(path))
	}
	if recursive {
		return m.addTree(path)
	}
	return m.addDirectory(path, false, map[string]bool{})
}
func (m *watchManager) watchNearestExistingAncestor(path string) error {
	for candidate := filepath.Dir(path); ; candidate = filepath.Dir(candidate) {
		info, err := m.fs.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return m.watch(candidate)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(candidate) == candidate {
			return fmt.Errorf("no existing directory contains watch target %s", path)
		}
	}
}
func (m *watchManager) link(path string) {
	parent := filepath.Dir(path)
	if parent == path {
		return
	}
	if m.children[parent] == nil {
		m.children[parent] = map[string]struct{}{}
	}
	m.children[parent][path] = struct{}{}
}
func (m *watchManager) watch(path string) error {
	path = filepath.Clean(path)
	if m.isWatched(path) {
		return nil
	}
	if err := m.backend.Add(path); err != nil {
		return err
	}
	m.mu.Lock()
	m.watched[path] = struct{}{}
	m.link(path)
	m.mu.Unlock()
	return nil
}
func (m *watchManager) addTree(root string) ([]string, error) {
	if m.ignoreDir != nil && m.ignoreDir(root, filepath.Base(root)) {
		return nil, nil
	}
	return m.addDirectory(root, true, map[string]bool{})
}
func (m *watchManager) addDirectory(directory string, recursive bool, visited map[string]bool) ([]string, error) {
	m.mu.RLock()
	complete, inventoried := m.complete[directory]
	m.mu.RUnlock()
	if inventoried && (!recursive || complete) {
		return nil, nil
	}
	if m.followSymlinks {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return nil, err
		}
		if visited[resolved] {
			return nil, nil
		}
		visited[resolved] = true
	}
	if err := m.watch(directory); err != nil {
		return nil, err
	}
	entries, err := m.fs.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if !m.followSymlinks {
				continue
			}
			info, err := m.fs.Stat(path)
			if err != nil {
				return paths, err
			}
			isDir = info.IsDir()
		}
		if !isDir {
			paths = append(paths, m.remember(path)...)
			continue
		}
		if !recursive || m.ignoreDir != nil && m.ignoreDir(path, entry.Name()) {
			continue
		}
		found, err := m.addDirectory(path, true, visited)
		paths = append(paths, found...)
		if err != nil {
			return paths, err
		}
	}
	m.mu.Lock()
	m.complete[directory] = recursive
	m.mu.Unlock()
	return paths, nil
}
func (m *watchManager) isWatched(path string) bool {
	m.mu.RLock()
	_, ok := m.watched[filepath.Clean(path)]
	m.mu.RUnlock()
	return ok
}
func (m *watchManager) remember(path string) []string {
	classification, ok := m.paths.classification(path, false)
	if !ok {
		return nil
	}
	m.mu.Lock()
	_, known := m.known[path]
	m.known[path] = classification
	m.link(path)
	m.mu.Unlock()
	if known {
		return nil
	}
	return []string{path}
}
func (m *watchManager) forget(path string) {
	m.mu.Lock()
	delete(m.known, path)
	delete(m.children[filepath.Dir(path)], path)
	m.mu.Unlock()
}
func (m *watchManager) removeTree(path string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var paths []string
	var failures error
	var remove func(string)
	remove = func(current string) {
		for child := range m.children[current] {
			remove(child)
		}
		if _, ok := m.known[current]; ok {
			paths = append(paths, current)
			delete(m.known, current)
		}
		if _, ok := m.watched[current]; ok {
			if err := m.backend.Remove(current); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) && !errors.Is(err, os.ErrNotExist) {
				failures = errors.Join(failures, err)
			}
			delete(m.watched, current)
		}
		delete(m.complete, current)
		delete(m.children, current)
	}
	remove(path)
	delete(m.children[filepath.Dir(path)], path)
	return paths, failures
}

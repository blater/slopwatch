package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// New creates a monitor. It does not touch the filesystem or start goroutines.
func New(cfg Config) (*Monitor, error) {
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	root = filepath.Clean(root)
	if cfg.Debounce <= 0 {
		cfg.Debounce = 80 * time.Millisecond
	}
	factory := cfg.BackendFactory
	if factory == nil {
		factory = func() (Backend, error) {
			w, err := fsnotify.NewWatcher()
			if err != nil {
				return nil, err
			}
			backend := &fsnotifyBackend{watcher: w, events: make(chan Event), errors: make(chan error), done: make(chan struct{})}
			go backend.forward()
			return backend, nil
		}
	}
	backend, err := factory()
	if err != nil {
		return nil, err
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []Scope{{Path: root, Recursive: true, Kind: KindSource}}
	}
	m := &Monitor{
		engine: monitorEngine{
			paths: pathPolicy{root: root, classifier: cfg.Classifier},
			watch: watchManager{
				backend: backend, ignoreDir: cfg.IgnoreDirectory,
				followSymlinks: cfg.FollowSymlinks, watched: make(map[string]struct{}),
			},
			reconcile: cfg.Reconcile,
		},
		started: make(chan struct{}),
	}
	m.engine.paths.scopes = normalizeScopes(&m.engine.paths, cfg.Scopes)
	m.engine.paths.inputs = normalizeInputs(&m.engine.paths, cfg)
	m.engine.events = eventManager{
		paths: &m.engine.paths, watch: &m.engine.watch,
		dirty: &m.engine.dirty, wake: make(chan struct{}, 1), done: make(chan struct{}),
		debounce: cfg.Debounce,
	}
	m.engine.dirty.entries = make(map[string]DirtyEntry)
	return m, nil
}

func normalizeScopes(m *pathPolicy, scopes []Scope) []Scope {
	result := make([]Scope, 0, len(scopes))
	for _, scope := range scopes {
		scope.Path = m.absolute(scope.Path)
		if !scope.Directory {
			if info, err := os.Stat(scope.Path); err == nil {
				scope.Directory = info.IsDir()
			}
		}
		result = append(result, scope)
	}
	return result
}

func normalizeInputs(m *pathPolicy, cfg Config) []Input {
	inputs := make([]Input, 0, len(cfg.Inputs)+len(cfg.DependencyPaths)+len(cfg.ConfigurationPaths))
	for _, input := range cfg.Inputs {
		input.Path = m.absolute(input.Path)
		if !input.Directory {
			if info, err := os.Stat(input.Path); err == nil {
				input.Directory = info.IsDir()
			}
		}
		inputs = append(inputs, input)
	}
	for _, path := range cfg.DependencyPaths {
		inputs = append(inputs, Input{Path: m.absolute(path), Kind: KindDependency})
	}
	for _, path := range cfg.ConfigurationPaths {
		inputs = append(inputs, Input{Path: m.absolute(path), Kind: KindConfiguration})
	}
	return inputs
}

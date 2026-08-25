package follow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	workspacefs "github.com/blater/slopwatch/internal/workspace"
)

var ignoredDirectories = map[string]bool{
	".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true,
	".pytest_cache": true, ".ruff_cache": true, "build": true,
	"coverage": true, "dist": true, "node_modules": true, "out": true,
	"target": true, "vendor": true,
}

var testDirectories = map[string]bool{
	"__tests__": true, "integration-test": true, "integration-tests": true,
	"integrationtest": true, "integrationtests": true, "spec": true,
	"specs": true, "test": true, "test-fixtures": true,
	"testfixtures": true, "tests": true,
}

type sourceChange struct {
	Paths []string
	Full  bool
	Err   error
}

type sourceWatcher struct {
	root           string
	includeTests   bool
	followSymlinks bool
	languages      map[string]bool
	scopes         []watchScope
	monitor        *workspacefs.Monitor
	startOnce      sync.Once
	startErr       error
	done           chan struct{}
}

type watchScope struct {
	path      string
	directory bool
}

func newSourceWatcher(root string, targets []string, includeTests, followSymlinks bool, languages []string) (*sourceWatcher, error) {
	selected := make(map[string]bool, len(languages))
	for _, language := range languages {
		selected[language] = true
	}
	result := &sourceWatcher{
		root: root, includeTests: includeTests, followSymlinks: followSymlinks,
		languages: selected, done: make(chan struct{}),
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	monitorScopes := make([]workspacefs.Scope, 0, len(targets))
	for _, target := range targets {
		absolute := target
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, target)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, err
		}
		absolute = filepath.Clean(absolute)
		result.scopes = append(result.scopes, watchScope{path: absolute, directory: info.IsDir()})
		monitorScopes = append(monitorScopes, workspacefs.Scope{
			Path: absolute, Recursive: info.IsDir(), Directory: info.IsDir(), Kind: workspacefs.KindSource,
		})
	}
	monitor, err := workspacefs.New(workspacefs.Config{
		Root: root, Scopes: monitorScopes, Inputs: configurationInputs(result),
		FollowSymlinks: followSymlinks,
		Classifier: workspacefs.ClassifierFunc(func(relative string, directory bool) (workspacefs.Classification, bool) {
			if directory {
				return workspacefs.Classification{}, false
			}
			if language, ok := configurationLanguage(relative); ok {
				if len(result.languages) == 0 || language == "" || result.languages[language] {
					return workspacefs.Classification{Kind: workspacefs.KindConfiguration, Language: language}, true
				}
			}
			if excluded(result, relative) {
				return workspacefs.Classification{}, false
			}
			language, ok := languageFor(result, relative)
			if !ok || len(result.languages) > 0 && !result.languages[language] {
				return workspacefs.Classification{}, false
			}
			return workspacefs.Classification{Kind: workspacefs.KindSource, Language: language}, true
		}),
		IgnoreDirectory: func(_ string, name string) bool {
			return ignoredDirectories[strings.ToLower(name)]
		},
	})
	if err != nil {
		return nil, err
	}
	result.monitor = monitor
	return result, nil
}

func (watcher *sourceWatcher) close() {
	select {
	case <-watcher.done:
	default:
		close(watcher.done)
	}
	_ = watcher.monitor.Close()
}

func (watcher *sourceWatcher) start() error {
	watcher.startOnce.Do(func() { watcher.startErr = watcher.monitor.Start(context.Background()) })
	if watcher.startErr != nil {
		return fmt.Errorf("source watcher: %w", watcher.startErr)
	}
	return nil
}

func (watcher *sourceWatcher) wait() sourceChange {
	if err := watcher.start(); err != nil {
		return sourceChange{Err: err}
	}
	batch, err := watcher.monitor.WaitAndDrain(context.Background())
	if err != nil {
		return sourceChange{Err: err}
	}
	paths := make([]string, 0, len(batch.Entries))
	full := batch.All
	for _, entry := range batch.Entries {
		if entry.Kind == workspacefs.KindSource {
			paths = append(paths, entry.Path)
		} else {
			// Configuration and dependency inputs can alter ownership and type
			// context for many units. A path-only refresh is not cache-safe.
			full = true
		}
	}
	return sourceChange{Paths: paths, Full: full}
}

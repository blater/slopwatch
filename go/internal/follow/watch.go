package follow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/sourceignore"
	"github.com/blater/slopwatch/internal/unitplan"
	workspacefs "github.com/blater/slopwatch/internal/workspace"
)

var testDirectories = map[string]bool{
	"__tests__": true, "integration-test": true, "integration-tests": true,
	"integrationtest": true, "integrationtests": true, "spec": true,
	"specs": true, "test": true, "test-fixtures": true,
	"testfixtures": true, "tests": true,
}

type sourceChange struct {
	watcher     *sourceWatcher
	IgnoreRules bool
	Paths       []string
	Closed      bool
	Err         error
}

type sourceWatcher struct {
	ancestorRules  map[string]string
	matcher        *sourceignore.Matcher
	root           string
	includeTests   bool
	followSymlinks bool
	languages      map[string]bool
	scopes         []watchScope
	outputScopes   []watchScope
	monitor        *workspacefs.Monitor
	startOnce      sync.Once
	closeOnce      sync.Once
	startErr       error
	done           chan struct{}
}

type watchScope struct {
	path      string
	directory bool
}

func newSourceWatcher(root string, targets []string, includeTests, followSymlinks bool, languages []string, disableGitignore ...bool) (*sourceWatcher, error) {
	disabled := len(disableGitignore) > 0 && disableGitignore[0]
	matcher, err := sourceignore.New(root, disabled)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(languages))
	for _, language := range languages {
		selected[language] = true
	}
	result := &sourceWatcher{
		matcher: matcher,
		root:    root, includeTests: includeTests, followSymlinks: followSymlinks,
		languages: selected, done: make(chan struct{}),
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	monitorScopes := []workspacefs.Scope{{Path: root, Recursive: true, Directory: true, Kind: workspacefs.KindSource}}
	result.scopes = []watchScope{{path: filepath.Clean(root), directory: true}}
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
		result.outputScopes = append(result.outputScopes, watchScope{path: absolute, directory: info.IsDir()})
		// Explicit entry points remain authorized if replaced by symlinks later.
		// Completed inventory makes these overlapping scopes registration-only.
		if absolute == filepath.Clean(root) {
			continue
		}
		result.scopes = append(result.scopes, watchScope{path: absolute, directory: info.IsDir()})
		monitorScopes = append(monitorScopes, workspacefs.Scope{
			Path: absolute, Recursive: info.IsDir(), Directory: info.IsDir(), Kind: workspacefs.KindSource,
		})
	}
	result.ancestorRules = map[string]string{}
	var monitorInputs []workspacefs.Input
	for _, input := range configurationInputs(result) {
		relative, err := filepath.Rel(root, input.Path)
		if filepath.Base(input.Path) == ".gitignore" && (err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			result.ancestorRules[input.Path] = sourceignore.InputFingerprint(input.Path)
		} else {
			monitorInputs = append(monitorInputs, input)
		}
	}
	monitor, err := workspacefs.New(workspacefs.Config{
		Root: root, Scopes: monitorScopes, Inputs: monitorInputs,
		FollowSymlinks: followSymlinks,
		Classifier: workspacefs.ClassifierFunc(func(relative string, directory bool) (workspacefs.Classification, bool) {
			if directory {
				return workspacefs.Classification{}, false
			}
			if filepath.Base(relative) == ".gitignore" {
				return workspacefs.Classification{Kind: workspacefs.KindConfiguration}, true
			}
			if _, configuration := configurationLanguage(relative); configuration {
				return workspacefs.Classification{}, false
			}
			if excluded(result, relative) {
				return workspacefs.Classification{}, false
			}
			language, ok := languageFor(result, relative)
			if !ok || !native.ContextSourceEligible(relative, language, includeTests) || len(result.languages) > 0 && !result.languages[language] {
				return workspacefs.Classification{}, false
			}
			return workspacefs.Classification{Kind: workspacefs.KindSource, Language: language}, true
		}),
		IgnoreDirectory: func(path string, name string) bool {
			return unitplan.IgnoredDirectory(name) || result.matcher.Ignored(path, true)
		},
	})
	if err != nil {
		return nil, err
	}
	result.monitor = monitor
	return result, nil
}

func (watcher *sourceWatcher) close() {
	watcher.closeOnce.Do(func() { close(watcher.done); _ = watcher.monitor.Close() })
}

func (watcher *sourceWatcher) start() error {
	watcher.startOnce.Do(func() { watcher.startErr = watcher.monitor.Start(context.Background()); watcher.matcher.Freeze() })
	if watcher.startErr != nil {
		return fmt.Errorf("source watcher: %w", watcher.startErr)
	}
	return nil
}

func (watcher *sourceWatcher) wait() sourceChange {
	if err := watcher.start(); err != nil {
		return sourceChange{Err: err, Closed: true, watcher: watcher}
	}
	batch, err, rulesChanged := watcher.waitBatch()
	paths := make([]string, 0, len(batch.Entries))
	for _, entry := range batch.Entries {
		if entry.Kind == workspacefs.KindSource {
			paths = append(paths, entry.Path)
		}
		if filepath.Base(entry.Path) == ".gitignore" {
			rulesChanged = true
		}
	}
	return sourceChange{Paths: paths, IgnoreRules: rulesChanged, Err: errors.Join(err, batch.Err), Closed: batch.Closed || errors.Is(err, io.EOF), watcher: watcher}
}

// External ancestor paths are exact configuration inputs, not source scopes.
// Polling a bounded list avoids kqueue opening arbitrary siblings in /tmp or a
// home directory merely to detect an ancestor .gitignore creation.
func (watcher *sourceWatcher) waitBatch() (workspacefs.DirtyBatch, error, bool) {
	return watcher.waitBatchFrom(watcher.monitor.WaitAndDrain, 250*time.Millisecond)
}

func (watcher *sourceWatcher) waitBatchFrom(wait func(context.Context) (workspacefs.DirtyBatch, error), interval time.Duration) (workspacefs.DirtyBatch, error, bool) {
	if len(watcher.ancestorRules) == 0 {
		batch, err := wait(context.Background())
		return batch, err, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		batch workspacefs.DirtyBatch
		err   error
	}
	ready := make(chan result, 1)
	go func() { batch, err := wait(ctx); ready <- result{batch, err} }()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case found := <-ready:
			// A steady stream of source batches can reset the timer indefinitely.
			// Recheck before every return while retaining the source batch.
			return found.batch, found.err, watcher.ancestorRulesChanged()
		case <-ticker.C:
			if watcher.ancestorRulesChanged() {
				cancel()
				found := <-ready
				// Cancellation does not consume dirty state; a batch already
				// drained by the waiter must be delivered with the notice.
				if errors.Is(found.err, context.Canceled) {
					found.err = nil
				}
				return found.batch, found.err, true
			}
		}
	}
}

func (watcher *sourceWatcher) ancestorRulesChanged() bool {
	changed := false
	for path, previous := range watcher.ancestorRules {
		next := sourceignore.InputFingerprint(path)
		if next != previous {
			watcher.ancestorRules[path] = next
			changed = true
		}
	}
	return changed
}

func (watcher *sourceWatcher) startupMatcher() *sourceignore.Matcher {
	if watcher == nil {
		return nil
	}
	return watcher.matcher
}

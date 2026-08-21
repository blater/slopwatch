package follow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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
	Err   error
}

type sourceWatcher struct {
	root         string
	includeTests bool
	languages    map[string]bool
	scopes       []watchScope
	watcher      *fsnotify.Watcher
	changes      chan sourceChange
	done         chan struct{}
}

type watchScope struct {
	path      string
	directory bool
}

func newSourceWatcher(root string, targets []string, includeTests bool, languages []string) (*sourceWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(languages))
	for _, language := range languages {
		selected[language] = true
	}
	result := &sourceWatcher{
		root: root, includeTests: includeTests, languages: selected,
		watcher: watcher, changes: make(chan sourceChange, 8), done: make(chan struct{}),
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	for _, target := range targets {
		absolute := target
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, target)
		}
		info, statErr := os.Stat(absolute)
		if statErr != nil {
			watcher.Close()
			return nil, statErr
		}
		if !info.IsDir() {
			result.scopes = append(result.scopes, watchScope{path: filepath.Clean(absolute)})
			absolute = filepath.Dir(absolute)
		} else {
			result.scopes = append(result.scopes, watchScope{path: filepath.Clean(absolute), directory: true})
		}
		if err := result.addTree(absolute); err != nil {
			watcher.Close()
			return nil, err
		}
	}
	go result.run()
	return result, nil
}

func (watcher *sourceWatcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && ignoredDirectories[strings.ToLower(entry.Name())] {
			return filepath.SkipDir
		}
		return watcher.watcher.Add(path)
	})
}

func (watcher *sourceWatcher) close() {
	close(watcher.done)
	_ = watcher.watcher.Close()
}

func (watcher *sourceWatcher) eligible(path string) (string, string, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	relative, err := filepath.Rel(watcher.root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	inScope := false
	for _, scope := range watcher.scopes {
		if !scope.directory && filepath.Clean(absolute) == scope.path {
			inScope = true
			break
		}
		if scope.directory {
			scoped, scopeErr := filepath.Rel(scope.path, absolute)
			if scopeErr == nil && scoped != ".." && !strings.HasPrefix(scoped, ".."+string(filepath.Separator)) {
				inScope = true
				break
			}
		}
	}
	if !inScope {
		return "", "", false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		lower := strings.ToLower(part)
		if ignoredDirectories[lower] {
			return "", "", false
		}
		if !watcher.includeTests && testDirectories[lower] {
			return "", "", false
		}
	}
	name := strings.ToLower(filepath.Base(relative))
	language := ""
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		language = "go"
		if !watcher.includeTests && strings.HasSuffix(name, "_test.go") {
			return "", "", false
		}
	case ".java":
		language = "java"
	case ".rs":
		language = "rust"
	case ".ts", ".tsx", ".mts", ".cts":
		language = "typescript"
		if !watcher.includeTests && isTypeScriptTest(name) {
			return "", "", false
		}
	default:
		return "", "", false
	}
	if len(watcher.languages) > 0 && !watcher.languages[language] {
		return "", "", false
	}
	return filepath.ToSlash(relative), language, true
}

func isTypeScriptTest(name string) bool {
	for _, suffix := range []string{
		".spec.cts", ".spec.mts", ".spec.ts", ".spec.tsx",
		".test.cts", ".test.mts", ".test.ts", ".test.tsx",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func (watcher *sourceWatcher) run() {
	pending := map[string]string{}
	var timer *time.Timer
	var timerChannel <-chan time.Time
	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for path := range pending {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		pending = map[string]string{}
		select {
		case watcher.changes <- sourceChange{Paths: paths}:
		default:
		}
	}
	for {
		select {
		case <-watcher.done:
			if timer != nil {
				timer.Stop()
			}
			return
		case err, ok := <-watcher.watcher.Errors:
			if ok {
				select {
				case watcher.changes <- sourceChange{Err: fmt.Errorf("source watcher: %w", err)}:
				default:
				}
			}
		case event, ok := <-watcher.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = watcher.addTree(event.Name)
					continue
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			path, language, ok := watcher.eligible(event.Name)
			if !ok {
				continue
			}
			pending[path] = language
			if timer == nil {
				timer = time.NewTimer(80 * time.Millisecond)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(80 * time.Millisecond)
			}
			timerChannel = timer.C
		case <-timerChannel:
			flush()
			timerChannel = nil
		}
	}
}

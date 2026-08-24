package follow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		Root: root, Scopes: monitorScopes, Inputs: result.configurationInputs(),
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
			if result.excluded(relative) {
				return workspacefs.Classification{}, false
			}
			language, ok := result.languageFor(relative)
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

func (watcher *sourceWatcher) configurationInputs() []workspacefs.Input {
	seen := map[string]bool{}
	var inputs []workspacefs.Input
	add := func(path string) {
		path = filepath.Clean(path)
		if seen[path] || watcher.inScope(path) {
			return
		}
		seen[path] = true
		inputs = append(inputs, workspacefs.Input{Path: path, Kind: workspacefs.KindConfiguration})
	}
	for _, scope := range watcher.scopes {
		directory := scope.path
		if !scope.directory {
			directory = filepath.Dir(directory)
		}
		for {
			// Include known names even when absent so creation is observed.
			for _, relative := range []string{
				"go.mod", "go.sum", "go.work", "go.work.sum", "Cargo.toml", "Cargo.lock", "build.rs",
				"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts",
				"gradle.properties", "gradle.lockfile", "package.json", "package-lock.json",
				"pnpm-lock.yaml", "yarn.lock", "tsconfig.json", ".cargo/config", ".cargo/config.toml",
				".mvn/maven.config", ".mvn/jvm.config", "gradle/wrapper/gradle-wrapper.properties",
			} {
				add(filepath.Join(directory, filepath.FromSlash(relative)))
			}
			// Existing named tsconfig/build inputs can have project-specific names.
			if entries, err := os.ReadDir(directory); err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						continue
					}
					relative, err := filepath.Rel(watcher.root, filepath.Join(directory, entry.Name()))
					if err == nil {
						if _, ok := configurationLanguage(relative); ok {
							add(filepath.Join(directory, entry.Name()))
						}
					}
				}
			}
			if directory == watcher.root {
				break
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			relative, err := filepath.Rel(watcher.root, parent)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				break
			}
			directory = parent
		}
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	return inputs
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

func (watcher *sourceWatcher) inScope(absolute string) bool {
	for _, scope := range watcher.scopes {
		if !scope.directory && filepath.Clean(absolute) == scope.path {
			return true
		}
		if scope.directory {
			relative, err := filepath.Rel(scope.path, absolute)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

func (watcher *sourceWatcher) excluded(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		lower := strings.ToLower(part)
		if ignoredDirectories[lower] || !watcher.includeTests && testDirectories[lower] {
			return true
		}
	}
	return false
}

func (watcher *sourceWatcher) languageFor(relative string) (string, bool) {
	name := strings.ToLower(filepath.Base(relative))
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".go":
		if !watcher.includeTests && strings.HasSuffix(name, "_test.go") {
			return "", false
		}
		return "go", true
	case ".java":
		return "java", true
	case ".rs":
		return "rust", true
	case ".ts", ".tsx", ".mts", ".cts":
		if !watcher.includeTests && isTypeScriptTest(name) {
			return "", false
		}
		return "typescript", true
	default:
		return "", false
	}
}

// configurationLanguage recognizes the analysis-affecting inputs owned by
// the language planners. An empty language means the input can affect more
// than one selected language.
func configurationLanguage(relative string) (string, bool) {
	name := strings.ToLower(filepath.Base(relative))
	switch name {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return "go", true
	case "cargo.toml", "cargo.lock", "build.rs":
		return "rust", true
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts",
		"gradle.properties", "gradle.lockfile", "maven.config", "jvm.config":
		return "java", true
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return "typescript", true
	}
	if strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json") {
		return "typescript", true
	}
	path := strings.ToLower(filepath.ToSlash(relative))
	if strings.HasSuffix(path, "/.cargo/config") || strings.HasSuffix(path, "/.cargo/config.toml") ||
		strings.HasSuffix(path, "/.mvn/maven.config") || strings.HasSuffix(path, "/.mvn/jvm.config") ||
		strings.Contains("/"+path, "/gradle/wrapper/") {
		return "", true
	}
	return "", false
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
	if !watcher.inScope(absolute) || watcher.excluded(relative) {
		return "", "", false
	}
	if !watcher.followSymlinks && watcher.isNestedSymlink(absolute) {
		return "", "", false
	}
	language, ok := watcher.languageFor(relative)
	if !ok || len(watcher.languages) > 0 && !watcher.languages[language] {
		return "", "", false
	}
	return filepath.ToSlash(relative), language, true
}

func (watcher *sourceWatcher) isNestedSymlink(path string) bool {
	for _, scope := range watcher.scopes {
		if !scope.directory {
			// An explicitly selected symlinked file is a target, not a nested
			// link discovered while traversing a directory.
			if filepath.Clean(path) == scope.path {
				return false
			}
			continue
		}
		relative, err := filepath.Rel(scope.path, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		current := scope.path
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			metadata, statErr := os.Lstat(current)
			if statErr != nil {
				break
			}
			if metadata.Mode()&os.ModeSymlink != 0 {
				return true
			}
		}
	}
	return false
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

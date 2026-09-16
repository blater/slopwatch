package follow

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	workspacefs "github.com/blater/slopwatch/internal/workspace"
)

var knownConfigurationFiles = []string{
	"go.mod", "go.sum", "go.work", "go.work.sum", "Cargo.toml", "Cargo.lock", "build.rs",
	"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts",
	"gradle.properties", "gradle.lockfile", "package.json", "package-lock.json",
	"pnpm-lock.yaml", "yarn.lock", "tsconfig.json", ".cargo/config", ".cargo/config.toml",
	".mvn/maven.config", ".mvn/jvm.config", "gradle/wrapper/gradle-wrapper.properties",
}

func configurationInputs(watcher *sourceWatcher) []workspacefs.Input {
	seen := map[string]bool{}
	var inputs []workspacefs.Input
	add := func(path string) {
		path = filepath.Clean(path)
		if seen[path] || inScope(watcher, path) {
			return
		}
		seen[path] = true
		inputs = append(inputs, workspacefs.Input{Path: path, Kind: workspacefs.KindConfiguration})
	}
	for _, scope := range watcher.scopes {
		for directory := scopeDirectory(scope); directory != ""; directory = parentConfigurationDirectory(watcher.root, directory) {
			addKnownConfigurationInputs(directory, add)
			addDiscoveredConfigurationInputs(watcher.root, directory, add)
		}
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	return inputs
}

func scopeDirectory(scope watchScope) string {
	if scope.directory {
		return scope.path
	}
	return filepath.Dir(scope.path)
}

func parentConfigurationDirectory(root, directory string) string {
	if directory == root {
		return ""
	}
	parent := filepath.Dir(directory)
	if parent == directory {
		return ""
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return parent
}

func addKnownConfigurationInputs(directory string, add func(string)) {
	for _, relative := range knownConfigurationFiles {
		add(filepath.Join(directory, filepath.FromSlash(relative)))
	}
}

func addDiscoveredConfigurationInputs(root, directory string, add func(string)) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		relative, err := filepath.Rel(root, filepath.Join(directory, entry.Name()))
		if err == nil {
			if _, ok := configurationLanguage(relative); ok {
				add(filepath.Join(directory, entry.Name()))
			}
		}
	}
}

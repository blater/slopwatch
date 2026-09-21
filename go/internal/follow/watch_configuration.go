package follow

import (
	"path/filepath"
	"sort"
	"strings"

	workspacefs "github.com/blater/slopwatch/internal/workspace"
)

var knownConfigurationFiles = []string{".gitignore"}

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
		}
	}
	for _, path := range watcher.matcher.AncestorInputs() {
		add(path)
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

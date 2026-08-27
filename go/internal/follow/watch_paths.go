package follow

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/blater/slopmochi/internal/sourcepath"
)

func (watcher *sourceWatcher) eligible(path string) (string, string, bool) {
	return eligible(watcher, path)
}

func inScope(watcher *sourceWatcher, absolute string) bool {
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

func excluded(watcher *sourceWatcher, relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		lower := strings.ToLower(part)
		if sourcepath.IsIgnoredDirectory(lower) || !watcher.includeTests && testDirectories[lower] {
			return true
		}
	}
	return false
}

func languageFor(watcher *sourceWatcher, relative string) (string, bool) {
	name := strings.ToLower(filepath.Base(relative))
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".go":
		if !watcher.includeTests && strings.HasSuffix(name, "_test.go") {
			return "", false
		}
		return "go", true
	case ".java":
		if sourcepath.IsJavaResource(relative) {
			return "", false
		}
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

func eligible(watcher *sourceWatcher, path string) (string, string, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	relative, err := filepath.Rel(watcher.root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	if !inScope(watcher, absolute) || excluded(watcher, relative) {
		return "", "", false
	}
	if !watcher.followSymlinks && isNestedSymlink(watcher, absolute) {
		return "", "", false
	}
	language, ok := languageFor(watcher, relative)
	if !ok || len(watcher.languages) > 0 && !watcher.languages[language] {
		return "", "", false
	}
	return filepath.ToSlash(relative), language, true
}

func isNestedSymlink(watcher *sourceWatcher, path string) bool {
	for _, scope := range watcher.scopes {
		if !scope.directory {
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

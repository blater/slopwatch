package native

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/unitplan"
)

func containsTestPath(paths []string, language string) bool {
	for _, path := range paths {
		if isTestPath(path, language) {
			return true
		}
	}
	return false
}

func withoutTestPaths(paths []string, language string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isTestPath(path, language) {
			result = append(result, path)
		}
	}
	return result
}

func isTestPath(path, language string) bool {
	lower := strings.ToLower(path)
	switch language {
	case "go":
		return strings.HasSuffix(lower, "_test.go")
	case "rust":
		return strings.Contains(lower, "/tests/") || strings.Contains(lower, "/benches/") || strings.HasPrefix(lower, "tests/") || strings.HasPrefix(lower, "benches/")
	case "java":
		return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/")
	case "typescript":
		return typescriptTest(filepath.Base(lower))
	default:
		return false
	}
}

func discoveredPathSet(discovered map[string][]string, selected []string) map[string]bool {
	result := make(map[string]bool)
	for _, language := range selected {
		for _, path := range discovered[language] {
			result[path] = true
		}
	}
	return result
}

func intersectPaths(paths []string, wanted map[string]bool) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if wanted[path] {
			result = append(result, path)
		}
	}
	return result
}

func unitInputPaths(unit unitplan.Unit) []string {
	seen := make(map[string]bool)
	for _, paths := range [][]string{unit.Sources, unit.ContextSources, unit.ConfigInputs} {
		for _, path := range paths {
			seen[path] = true
		}
	}
	return mapKeys(seen)
}

func mapKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[path] = true
	}
	return result
}

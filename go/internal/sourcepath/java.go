// Package sourcepath classifies source-looking paths whose repository role is
// more specific than their filename extension.
package sourcepath

import (
	"path"
	"strings"
)

var ignoredSourceDirectories = map[string]bool{
	".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true,
	".pytest_cache": true, ".ruff_cache": true, "build": true,
	"coverage": true, "dist": true, "node_modules": true, "out": true,
	"target": true, "vendor": true,
}

// IsIgnoredDirectory reports whether name is a conventional generated-output,
// dependency, cache, or tool-state directory excluded from source discovery.
func IsIgnoredDirectory(name string) bool {
	return ignoredSourceDirectories[strings.ToLower(name)]
}

// IsSourceFile reports whether path is a Slopmark-supported source file and
// is not beneath a conventional generated-output or dependency directory.
// Test source files remain source files; generated test reports do not.
func IsSourceFile(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
	parts := strings.Split(normalized, "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		if IsIgnoredDirectory(part) {
			return false
		}
	}
	switch path.Ext(normalized) {
	case ".go", ".rs", ".ts", ".tsx", ".mts", ".cts":
		return true
	case ".java":
		return !IsJavaResource(normalized)
	default:
		return false
	}
}

// IsJavaResource reports whether path is beneath a conventional Java resource
// source root such as src/main/resources or src/test/resources. Java-looking
// files there are resources or templates, not compiler inputs.
func IsJavaResource(path string) bool {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(path, "\\", "/")), "/")
	for index := 0; index+2 < len(parts)-1; index++ {
		if parts[index] == "src" && parts[index+1] != "" && parts[index+2] == "resources" {
			return true
		}
	}
	return false
}

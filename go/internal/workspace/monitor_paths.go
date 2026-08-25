package workspace

import (
	"path/filepath"
	"strings"
)

func (m *pathPolicy) absolute(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(m.root, path))
}

func (m *pathPolicy) relative(path string) (string, bool) {
	rel, err := filepath.Rel(m.root, m.absolute(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	return filepath.ToSlash(rel), true
}

func contains(scope Scope, path string) bool {
	if filepath.Clean(scope.Path) == filepath.Clean(path) {
		return true
	}
	if !scope.Recursive {
		return scope.Directory && filepath.Dir(filepath.Clean(path)) == filepath.Clean(scope.Path)
	}
	rel, err := filepath.Rel(scope.Path, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *pathPolicy) inScope(path string) bool {
	for _, scope := range m.scopes {
		if contains(scope, path) {
			return true
		}
	}
	for _, input := range m.inputs {
		if contains(Scope{Path: input.Path, Recursive: input.Recursive, Directory: input.Directory}, path) {
			return true
		}
	}
	return false
}

func (m *pathPolicy) classification(path string, isDir bool) (Classification, bool) {
	if classification, ok := m.explicitClassification(path); ok {
		return classification, true
	}
	rel, ok := m.relative(path)
	if !ok || !m.inScope(path) || m.classifier == nil {
		return Classification{}, false
	}
	classification, ok := m.classifier.Classify(rel, isDir)
	if !ok {
		return Classification{}, false
	}
	return m.scopeClassification(path, classification), true
}

func (m *pathPolicy) explicitClassification(path string) (Classification, bool) {
	for _, input := range m.inputs {
		if contains(Scope{Path: input.Path, Recursive: input.Recursive, Directory: input.Directory}, path) {
			return Classification{Kind: input.Kind}, true
		}
	}
	return Classification{}, false
}

func (m *pathPolicy) scopeClassification(path string, classification Classification) Classification {
	for _, scope := range m.scopes {
		if contains(scope, path) && classification.Kind == KindSource {
			// A non-source classifier result is more specific than the scope's
			// default. This lets manifests inside a source tree trigger a full
			// dependency/configuration revalidation.
			classification.Kind = scope.Kind
			break
		}
	}
	return classification
}

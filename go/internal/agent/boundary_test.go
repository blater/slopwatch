package agent_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This dependency rule protects the deep adapter boundary: provider
// strategies receive all task/configuration evidence through agent.Request
// and may not reach sideways into caches, preferences, analysis, Git workflow,
// publication, or UI packages.
func TestAgentPackagesKeepDeepBoundary(t *testing.T) {
	disallowed := []string{
		"/internal/analysiscache", "/internal/preferences", "/internal/appconfig",
		"/internal/native", "/internal/fixanalysis", "/internal/candidate",
		"/internal/delivery", "/internal/publisher", "/internal/follow",
	}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range file.Imports {
			value, _ := strconv.Unquote(imported.Path.Value)
			for _, forbidden := range disallowed {
				if strings.Contains(value, forbidden) {
					t.Errorf("%s imports forbidden adapter dependency %s", path, value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

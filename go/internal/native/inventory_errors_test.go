package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireDiscoveredSourcesOnlyClassifiesEmptyInventories(t *testing.T) {
	cases := map[string]map[string][]string{
		"nil inventory":   nil,
		"empty inventory": map[string][]string{},
		"empty language":  map[string][]string{"go": nil, "rust": []string{}},
	}
	for name, discovered := range cases {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(requireDiscoveredSources(discovered), ErrNoSources) {
				t.Fatalf("requireDiscoveredSources(%#v) did not return ErrNoSources", discovered)
			}
		})
	}
	if err := requireDiscoveredSources(map[string][]string{"go": {"main.go"}, "rust": nil}); err != nil {
		t.Fatalf("mixed inventory rejected: %v", err)
	}
}

func TestAnalyzeReturnsTypedNoSourcesForExplicitLanguageOnEmptyWorkspace(t *testing.T) {
	analyzer := &Analyzer{workspace: t.TempDir(), options: Options{Targets: []string{"."}, Languages: []string{"go"}}}
	_, err := analyzer.Analyze(context.Background(), nil, nil)
	if !errors.Is(err, ErrNoSources) {
		t.Fatalf("empty analysis error = %v, want ErrNoSources", err)
	}
	if err.Error() != ErrNoSources.Error() {
		t.Fatalf("empty analysis text = %q, want %q", err.Error(), ErrNoSources.Error())
	}
}

func TestAnalyzeKeepsMissingLanguageAndDiscoveryErrorsDistinct(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	analyzer := &Analyzer{workspace: workspace, options: Options{Targets: []string{"."}, Languages: []string{"rust"}}}
	_, err := analyzer.Analyze(context.Background(), nil, nil)
	if err == nil || errors.Is(err, ErrNoSources) || !strings.Contains(err.Error(), "no source files discovered for: rust") {
		t.Fatalf("missing language error = %v", err)
	}
	missing := &Analyzer{workspace: workspace, options: Options{Targets: []string{"missing"}}}
	_, err = missing.Analyze(context.Background(), nil, nil)
	if err == nil || errors.Is(err, ErrNoSources) {
		t.Fatalf("discovery error = %v, want hard error", err)
	}
}

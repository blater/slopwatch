package analysiscache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRootResolution(t *testing.T) {
	t.Parallel()
	home := filepath.Join(string(filepath.Separator), "users", "ada")
	absXDG := filepath.Join(string(filepath.Separator), "var", "cache", "ada")
	tests := []struct {
		name string
		xdg  string
		home string
		want string
	}{
		{"unset", "", home, filepath.Join(home, ".cache", "slopwatch")},
		{"absolute xdg", absXDG, home, filepath.Join(absXDG, "slopwatch")},
		{"absolute xdg needs no home", absXDG, "", filepath.Join(absXDG, "slopwatch")},
		{"relative xdg falls back", "relative/cache", home, filepath.Join(home, ".cache", "slopwatch")},
		{"nul xdg falls back", string([]byte{'/', 'x', 0, 'y'}), home, filepath.Join(home, ".cache", "slopwatch")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := cacheRoot(test.xdg, test.home)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("cacheRoot() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultRootUsesXDGOrDotCacheFallback(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CACHE_HOME", xdg)
	got, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "slopwatch"); got != want {
		t.Fatalf("DefaultRoot with XDG = %q, want %q", got, want)
	}
	t.Setenv("XDG_CACHE_HOME", "relative/cache")
	got, err = DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".cache", "slopwatch"); got != want {
		t.Fatalf("DefaultRoot fallback = %q, want %q", got, want)
	}
}

func TestCacheRootRejectsInvalidFallbackHome(t *testing.T) {
	t.Parallel()
	for _, home := range []string{"", "relative", string([]byte{'/', 'x', 0, 'y'})} {
		if _, err := cacheRoot("relative-xdg", home); err == nil {
			t.Fatalf("cacheRoot accepted invalid home %q", home)
		}
	}
}

func TestUnitKeyIsCanonicalAndCorrectnessSensitive(t *testing.T) {
	t.Parallel()
	a := UnitKeyInput{
		UnitID: "go:example/pkg", Language: "go",
		Sources: []InputFingerprint{
			{Path: "pkg" + string(filepath.Separator) + "b.go", ContentHash: DigestBytes([]byte("b"))},
			{Path: "pkg/a.go", ContentHash: DigestBytes([]byte("a"))},
		},
		Configuration:   []InputFingerprint{{Path: "./go.mod", ContentHash: DigestBytes([]byte("module example"))}},
		Dependencies:    []DependencyFingerprint{{UnitID: "z", Fingerprint: keyFor([]byte("z"))}, {UnitID: "a", Fingerprint: keyFor([]byte("a"))}},
		AnalyzerDigest:  DigestBytes([]byte("analyzer")),
		FactVersion:     "facts-1",
		ProtocolVersion: "1",
		CatalogVersion:  "catalog-1",
		Components:      []ComponentDefinition{{ID: "z", Version: "1"}, {ID: "a", Version: "2"}},
		ParserMode:      "syntax",
		IncludeTests:    true,
		Targets:         []string{"pkg/b.go", "./pkg/a.go"},
		Languages:       []string{"rust", "go"},
		Toolchain:       map[string]string{"go": "1.25"},
	}
	b := a
	b.Sources = []InputFingerprint{a.Sources[1], a.Sources[0]}
	b.Dependencies = []DependencyFingerprint{a.Dependencies[1], a.Dependencies[0]}
	b.Components = []ComponentDefinition{a.Components[1], a.Components[0]}
	b.Targets = []string{"pkg/a.go", "pkg/b.go"}
	b.Languages = []string{"go", "rust"}
	first, err := UnitKey(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := UnitKey(b)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical equivalents differ: %s != %s", first, second)
	}

	changed := a
	changed.Sources = append([]InputFingerprint(nil), a.Sources...)
	changed.Sources[0].ContentHash = DigestBytes([]byte("changed"))
	different, err := UnitKey(changed)
	if err != nil {
		t.Fatal(err)
	}
	if different == first {
		t.Fatal("source content change did not invalidate unit key")
	}

	mutations := []struct {
		name   string
		mutate func(*UnitKeyInput)
	}{
		{"configuration", func(value *UnitKeyInput) { value.Configuration[0].ContentHash = DigestBytes([]byte("config-2")) }},
		{"dependency", func(value *UnitKeyInput) { value.Dependencies[0].Fingerprint = keyFor([]byte("dependency-2")) }},
		{"analyzer", func(value *UnitKeyInput) { value.AnalyzerDigest = DigestBytes([]byte("analyzer-2")) }},
		{"fact schema", func(value *UnitKeyInput) { value.FactVersion = "facts-2" }},
		{"protocol", func(value *UnitKeyInput) { value.ProtocolVersion = "2" }},
		{"catalog", func(value *UnitKeyInput) { value.CatalogVersion = "catalog-2" }},
		{"component definition", func(value *UnitKeyInput) { value.Components[0].Version = "2" }},
		{"parser mode", func(value *UnitKeyInput) { value.ParserMode = "typed" }},
		{"type analysis", func(value *UnitKeyInput) { value.TypeAnalysisMode = "on" }},
		{"include tests", func(value *UnitKeyInput) { value.IncludeTests = false }},
		{"target", func(value *UnitKeyInput) { value.Targets = []string{"pkg/a.go"} }},
		{"language", func(value *UnitKeyInput) { value.Languages = []string{"go"} }},
		{"toolchain", func(value *UnitKeyInput) { value.Toolchain["go"] = "1.26" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			copy := cloneKeyInput(t, a)
			mutation.mutate(&copy)
			got, keyErr := UnitKey(copy)
			if keyErr != nil {
				t.Fatal(keyErr)
			}
			if got == first {
				t.Fatalf("%s change did not invalidate unit key", mutation.name)
			}
		})
	}
}

func TestWorkspaceViewKeyIsCanonicalAndScopeSensitive(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	first, err := WorkspaceViewKey(workspace, ViewOptions{
		Targets: []string{"pkg/b", "./pkg/a"}, Languages: []string{"rust", "go"}, IncludeTests: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := WorkspaceViewKey(workspace, ViewOptions{
		Targets: []string{"pkg/a", "pkg/b"}, Languages: []string{"go", "rust"}, IncludeTests: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != equivalent {
		t.Fatal("equivalent workspace scopes produced different keys")
	}
	changes := []ViewOptions{
		{Targets: []string{"pkg/a"}, Languages: []string{"go", "rust"}, IncludeTests: true},
		{Targets: []string{"pkg/a", "pkg/b"}, Languages: []string{"go"}, IncludeTests: true},
		{Targets: []string{"pkg/a", "pkg/b"}, Languages: []string{"go", "rust"}},
		{Targets: []string{"pkg/a", "pkg/b"}, Languages: []string{"go", "rust"}, IncludeTests: true, TypeScriptTypes: true},
	}
	for _, options := range changes {
		got, keyErr := WorkspaceViewKey(workspace, options)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		if got == first {
			t.Fatalf("scope change did not change workspace view key: %#v", options)
		}
	}
}

func cloneKeyInput(t *testing.T, input UnitKeyInput) UnitKeyInput {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var result UnitKeyInput
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

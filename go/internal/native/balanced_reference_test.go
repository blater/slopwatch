package native

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestNativeStructuralAnalysisMatchesBalancedReference(t *testing.T) {
	assertBalancedReference(t, "reference.go")
}

func TestNativeJavaAndRustScoresMatchBalancedReference(t *testing.T) {
	assertBalancedReference(t, "Reference.java")
	assertBalancedReference(t, "reference.rs")
}

func assertBalancedReference(t *testing.T, name string) {
	t.Helper()
	root := testInstallationRoot(t)
	requireBalancedAnalyzer(t, root, name)
	workspace := copyBalancedFixture(t, name)
	analyzer, err := New(workspace, root, Options{Targets: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBalancedDocument(t, name, document)
}

func requireBalancedAnalyzer(t *testing.T, root, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "analyzers", "structural", "slopslap-structural")); err != nil {
		t.Skip("structural analyzer is not built")
	}
	if filepath.Ext(name) == ".go" {
		return
	}
	for _, required := range []string{"slopslap-structural-java.jar", "slopslap-structural-rust", filepath.Join("java-runtime", "bin", "java")} {
		if _, err := os.Stat(filepath.Join(root, "analyzers", "structural", required)); err != nil {
			t.Skip("structural analyzer helpers are not built")
		}
	}
}

func copyBalancedFixture(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", "balanced", name))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, name), source, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func assertBalancedDocument(t *testing.T, name string, document report.Document) {
	t.Helper()
	if len(document.Files) != 1 {
		t.Fatalf("got %d files", len(document.Files))
	}
	file := document.Files[0]
	if difference := math.Abs(file.Score - 20.854268271702); difference > 1e-9 {
		t.Fatalf("%s score = %.12f, reference = 20.854268271702", name, file.Score)
	}
	if file.Path != name || !file.Complete {
		t.Fatalf("%s result path=%q complete=%t", name, file.Path, file.Complete)
	}
	component := file.Components["cognitive_complexity"]
	if len(component.Subjects) != 1 || component.Subjects[0].Value != 21 {
		t.Fatalf("%s cognitive subjects = %#v", name, component.Subjects)
	}
}

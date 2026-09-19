package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func TestAnalyzeChangesRefreshesAndRemovesAffectedRows(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	writeTestFile(t, workspace, "other/b.go", "package other\nvar B = 2\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 22\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"pkg/a.go"})
	assertRequestPaths(t, requests, []string{"pkg/a.go"}, []string{"other/b.go"})
	requests = nil
	if err := removeChangeFile(workspace, "pkg/a.go"); err != nil {
		t.Fatal(err)
	}
	document, replacements, err = analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertDeletedChange(t, document, replacements, requests, "pkg/a.go")
}

func TestAnalyzeChangesExpandsTransitiveReverseDependencies(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 1\n")
	writeTestFile(t, workspace, "app/app.go", "package app\nimport \"example/lib\"\nvar Value = lib.Value\n")
	writeTestFile(t, workspace, "unrelated/other.go", "package unrelated\nconst Value = 3\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 2\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || !requestContainsPath(requests[0], "lib/lib.go") || !requestContainsPath(requests[0], "app/app.go") || requestContainsPath(requests[0], "unrelated/other.go") {
		t.Fatalf("reverse dependency request = %#v", requests)
	}
	assertChangePaths(t, document, replacements, []string{"app/app.go", "lib/lib.go"})
}

func TestAnalyzeChangesLimitsOutsideTargetDependencyToConsumer(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 1\n")
	writeTestFile(t, workspace, "app/app.go", "package app\nimport \"example/lib\"\nvar Value = lib.Value\n")
	writeTestFile(t, workspace, "unrelated/other.go", "package unrelated\nconst Value = 3\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	analyzer := newCacheTestAnalyzer(t, workspace,
		Options{Targets: []string{"app"}, Languages: []string{"go"}, ReadCache: true},
		store, goTestCatalog())
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 2\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"app/app.go"})
	assertRequestPaths(t, requests, []string{"app/app.go", "lib/lib.go"}, []string{"unrelated/other.go"})
}

func TestAnalyzeChangesUncachedDependencyKeepsConsumerScope(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 1\n")
	writeTestFile(t, workspace, "app/app.go", "package app\nimport \"example/lib\"\nvar Value = lib.Value\n")
	writeTestFile(t, workspace, "unrelated/other.go", "package unrelated\nconst Value = 3\n")
	analyzer := newChangeTestAnalyzerWithOptions(t, workspace,
		Options{Targets: []string{"app"}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	analyzer.SetCacheStore(nil)
	requests = nil
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 2\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"app/app.go"})
	assertRequestPaths(t, requests, []string{"app/app.go", "lib/lib.go"}, []string{"unrelated/other.go"})
}

func TestAnalyzeChangesRefreshesPeerFilesAndLeavesUnrelatedUnits(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	writeTestFile(t, workspace, "pkg/b.go", "package pkg\nvar B = 2\n")
	writeTestFile(t, workspace, "other/c.go", "package other\nvar C = 3\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 22\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"pkg/a.go", "pkg/b.go"})
	assertRequestPaths(t, requests, []string{"pkg/a.go", "pkg/b.go"}, []string{"other/c.go"})
}

func TestAnalyzeChangesDeletionRetainsReverseDependencyContext(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "lib/lib.go", "package lib\nconst Value = 1\n")
	writeTestFile(t, workspace, "app/app.go", "package app\nimport \"example/lib\"\nvar Value = lib.Value\n")
	writeTestFile(t, workspace, "unrelated/other.go", "package unrelated\nconst Value = 3\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	if err := os.Remove(filepath.Join(workspace, "lib/lib.go")); err != nil {
		t.Fatal(err)
	}
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"app/app.go", "lib/lib.go"})
	assertRequestPaths(t, requests, []string{"app/app.go"}, []string{"lib/lib.go", "unrelated/other.go"})
}

func TestAnalyzeChangesReportsAndRetriesTransientFailure(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("temporary analyzer failure")
	analyzer.runUnits = func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error) {
		return nil, failure
	}
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 2\n")
	document, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != 1 || document.Files[0].Complete || document.Files[0].ValidZero {
		t.Fatalf("transient failure should remain incomplete: %#v", document.Files)
	}
	requireRecoveryDiagnostic(t, document.Diagnostics, "native.analyzer_failed")
	requests = nil
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || !requestContainsPath(requests[0], "pkg/a.go") {
		t.Fatalf("retry did not analyze unchanged failed source: %#v", requests)
	}
}

func TestAnalyzeChangesDoesNotRerunWarmUnchangedUnits(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	analyzer := newChangeTestAnalyzer(t, workspace)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 2\n")
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("changed incremental analysis requests = %#v, want one miss", requests)
	}
	requests = nil
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatalf("warm incremental analysis reran backend: %#v", requests)
	}
}

func TestAnalyzeChangesRefreshesNewLanguagePeerFile(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "web/a.ts", "export const A = 1\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	analyzer := newCacheTestAnalyzer(t, workspace,
		Options{Targets: []string{"."}, Languages: []string{"typescript"}, ReadCache: true},
		store, typescriptTestCatalog())
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, workspace, "web/b.ts", "export const B = 2\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"web/b.ts"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"web/a.ts", "web/b.ts"})
	assertRequestPaths(t, requests, []string{"web/a.ts", "web/b.ts"}, nil)
}

func assertRequestPaths(t *testing.T, requests []analyzerRequest, required, forbidden []string) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("analysis request count = %d, want 1", len(requests))
	}
	for _, path := range required {
		if !requestContainsPath(requests[0], path) {
			t.Fatalf("request omitted %q: %#v", path, requests[0])
		}
	}
	for _, path := range forbidden {
		if requestContainsPath(requests[0], path) {
			t.Fatalf("request included unrelated %q: %#v", path, requests[0])
		}
	}
}

func assertDeletedChange(t *testing.T, document report.Document, replacements []string, requests []analyzerRequest, path string) {
	t.Helper()
	if len(document.Files) != 0 {
		t.Fatalf("deleted refresh document = %#v", document.Files)
	}
	if len(requests) != 0 {
		t.Fatalf("deleted refresh requests = %#v", requests)
	}
	if len(replacements) != 1 || replacements[0] != path {
		t.Fatalf("deleted refresh replacements = %v, want [%s]", replacements, path)
	}
}

func newChangeTestAnalyzer(t *testing.T, workspace string) *Analyzer {
	t.Helper()
	return newChangeTestAnalyzerWithOptions(t, workspace,
		Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
}

func newChangeTestAnalyzerWithOptions(t *testing.T, workspace string, options Options, catalog catalogDocument) *Analyzer {
	t.Helper()
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	return newCacheTestAnalyzer(t, workspace, options, store, catalog)
}

func recordChangeRequests(t *testing.T, requests *[]analyzerRequest) analyzerUnitsRunner {
	t.Helper()
	return func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		*requests = append(*requests, request)
		return fakeBatchInputs(t, request), nil
	}
}

func requestContainsPath(request analyzerRequest, want string) bool {
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			if path == want {
				return true
			}
		}
	}
	return false
}

func assertChangePaths(t *testing.T, document report.Document, replacements, want []string) {
	t.Helper()
	sort.Strings(want)
	sort.Strings(replacements)
	if len(replacements) != len(want) {
		t.Fatalf("replacement paths = %v, want %v", replacements, want)
	}
	for index := range want {
		if replacements[index] != want[index] {
			t.Fatalf("replacement paths = %v, want %v", replacements, want)
		}
	}
	for _, file := range document.Files {
		if !changePathIncluded(want, file.Path) {
			t.Fatalf("incremental document contains unrelated path %q", file.Path)
		}
	}
}

func changePathIncluded(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func removeChangeFile(workspace, path string) error {
	return os.Remove(filepath.Join(workspace, filepath.FromSlash(path)))
}

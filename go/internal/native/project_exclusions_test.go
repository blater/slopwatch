package native

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/unitplan"
)

func TestProjectExclusionsInventoryAndContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".slopwatch.toml", "[files]\nexclude = '''\nignored.go\nblocked/\n'''\n")
	for _, path := range []string{"keep.go", "ignored.go", "blocked/other.go"} {
		writeTestFile(t, root, path, "package p\n")
	}
	analyzer := &Analyzer{workspace: root}
	analyzer.SetDisableGitignore(true)
	for _, target := range []string{".", "ignored.go", "blocked"} {
		got, err := analyzer.discover([]string{target}, true, false)
		want := 0
		if target == "." {
			want = 1
		}
		if err != nil || len(got["go"]) != want {
			t.Fatalf("%s=%v %v", target, got, err)
		}
		if target == "." && filepath.Base(got["go"][0]) != "keep.go" {
			t.Fatalf("retained source missing: %v", got)
		}
	}
	plan, err := unitplan.PlanWorkspace(root, unitplan.Options{DisableGitignore: true, Targets: []string{"ignored.go"}})
	if err != nil {
		t.Fatal(err)
	}
	retained := false
	for _, unit := range plan.Units {
		for _, path := range append(unit.Sources, unit.ContextSources...) {
			if path != "keep.go" {
				t.Fatalf("excluded path in context: %s", path)
			}
			retained = true
		}
	}
	if !retained {
		t.Fatal("planning dropped retained keep.go")
	}
	writeTestFile(t, root, ".slopwatch.toml", "[files]\nexclude = false\n")
	if _, err := analyzer.discover(nil, true, false); err == nil {
		t.Fatal("discovery swallowed malformed TOML")
	}
	if _, err := unitplan.PlanWorkspace(root, unitplan.Options{}); err == nil {
		t.Fatal("planning swallowed malformed TOML")
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err == nil {
		t.Fatal("analysis swallowed malformed TOML")
	}
}

func TestMalformedProjectPreservesIncrementalPlan(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "pkg/a.go", "package pkg\nvar A = 1\n")
	analyzer := newChangeTestAnalyzer(t, root)
	var requests []analyzerRequest
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, root, "pkg/a.go", "package pkg\nvar A = 2\n")
	writeTestFile(t, root, ".slopwatch.toml", "[files]\nexclude = false\n")
	document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replacements, []string{"pkg/a.go"})
	if len(requests) == 0 {
		t.Fatal("startup policy did not refresh changed source")
	}
}

func TestProjectExclusionsReconcileCachedRows(t *testing.T) {
	analyzer := startupProjectionFixture(t)
	writeTestFile(t, analyzer.workspace, ".slopwatch.toml", "[files]\nexclude = 'kept.java'\n")
	document, ok := analyzer.CachedProjection()
	if !ok || len(document.Files) != 1 || document.Files[0].Path != "new.java" {
		t.Fatalf("excluded cached row leaked: %+v %v", document.Files, ok)
	}
	writeTestFile(t, analyzer.workspace, ".slopwatch.toml", "[files]\nexclude = ''\n")
	document, ok = analyzer.CachedProjection()
	if !ok || len(document.Files) != 2 {
		t.Fatalf("removed exclusion did not restore inventory: %+v %v", document.Files, ok)
	}
}

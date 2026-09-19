package native

import (
	"context"
	"errors"
	"github.com/blater/slopwatch/internal/unitplan"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryGitignoreExplicitTargetsAndContext(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{".gitignore": "ignored.go\nblocked/\n", "keep.go": "package p", "ignored.go": "package p", "blocked/other.go": "package p"} {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	analyzer := &Analyzer{workspace: root}
	for _, target := range []string{".", "ignored.go", "blocked"} {
		got, err := analyzer.discover([]string{target}, true, false)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if target == "." {
			want = 1
		}
		if len(got["go"]) != want {
			t.Fatalf("%s=%v", target, got)
		}
	}
	plan, err := workspacePlan(analyzer.engine(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range plan.Units {
		for _, path := range append(u.Sources, u.ContextSources...) {
			if path != "keep.go" {
				t.Fatalf("ignored context: %s", path)
			}
		}
	}
	analyzer.SetDisableGitignore(true)
	got, err := analyzer.discover([]string{"."}, true, false)
	if err != nil || len(got["go"]) != 3 {
		t.Fatalf("disabled=%v %v", got, err)
	}
	plan, err = unitplan.PlanWorkspace(root, unitplan.Options{DisableGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, u := range plan.Units {
		count += len(u.Sources)
	}
	if count != 3 {
		t.Fatalf("disabled context count=%d", count)
	}
}

func TestGitignoreViewIdentitySeparatesPreferenceAndReusesRuleEdits(t *testing.T) {
	root := t.TempDir()
	analyzer := &Analyzer{workspace: root}
	first, err := analyzer.viewKey(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.go\n"), 0644); err != nil {
		t.Fatal(err)
	}
	next, _ := analyzer.viewKey(Options{})
	disabled, _ := analyzer.viewKey(Options{DisableGitignore: true})
	if first != next || next == disabled || first == disabled {
		t.Fatal("view policy identities collide")
	}
}

func TestIgnoredExplicitSymlinksUseLogicalRules(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "real.go"), []byte("package p"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "logical")); err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("logical/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	analyzer := &Analyzer{workspace: root}
	for _, follow := range []bool{false, true} {
		got, err := analyzer.discover([]string{"logical"}, true, follow)
		if err != nil || len(got) != 0 {
			t.Fatalf("ignored symlink=%v %v", got, err)
		}
		analyzer.SetDisableGitignore(true)
		got, err = analyzer.discover([]string{"logical"}, true, follow)
		if err != nil || len(got["go"]) != 1 {
			t.Fatalf("explicit symlink=%v %v", got, err)
		}
		analyzer.SetDisableGitignore(false)
	}
}

func TestIgnoreMutationDuringAnalysisCannotPublishOrCacheOldPolicy(t *testing.T) {
	analyzer, _ := indexedCacheFixture(t)
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		writeTestFile(t, analyzer.workspace, "pkg/.gitignore", "a.go\n")
		return fakeBatchInputs(t, request), nil
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if !errors.Is(err, ErrWorkspaceChanged) || len(document.Files) != 0 {
		t.Fatalf("changed rules published: %+v %v", document.Files, err)
	}
	if _, ok := analyzer.CachedProjection(); ok {
		t.Fatal("mutated policy populated cache projection")
	}
}

func TestRuleEditCannotRestoreIgnoredCachedProjectionRow(t *testing.T) {
	analyzer := startupProjectionFixture(t)
	writeTestFile(t, analyzer.workspace, ".gitignore", "kept.java\n")
	document, ok := analyzer.CachedProjection()
	if !ok {
		t.Fatal("stable view should reconcile its current inventory")
	}
	if len(document.Files) != 1 || document.Files[0].Path != "new.java" {
		t.Fatalf("ignored cached row leaked: %+v", document.Files)
	}
}

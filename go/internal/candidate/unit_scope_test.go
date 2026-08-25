package candidate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestUnitScopePlannerFreezesTestsInTargetAnalysisUnitBelowRepoRoot(t *testing.T) {
	repository := t.TempDir()
	analysis := filepath.Join(repository, "go")
	for path, body := range map[string]string{"go.mod": "module example.com/test\n", "pkg/a.go": "package pkg\n", "pkg/a_test.go": "package pkg\n", "other/b_test.go": "package other\n"} {
		full := filepath.Join(analysis, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target, _ := fix.ParseRepoPath("go/pkg/a.go")
	paths, err := (UnitScopePlanner{}).Plan(context.Background(), fix.WorkspaceIdentity{RepositoryRoot: repository, AnalysisRoot: analysis}, []fix.RepoPath{target}, "targets-and-tests")
	if err != nil {
		t.Fatal(err)
	}
	want := map[fix.RepoPath]bool{"go/pkg/a.go": true, "go/pkg/a_test.go": true}
	if len(paths) != len(want) {
		t.Fatalf("allowed=%v", paths)
	}
	for _, path := range paths {
		if !want[path] {
			t.Fatalf("unexpected allowed path %s", path)
		}
	}
}

func TestUnitScopePlannerSelectsTypeScriptTestsWithoutWideningProject(t *testing.T) {
	repository := t.TempDir()
	for path, body := range map[string]string{
		"package.json":        `{}`,
		"src/a.ts":            `export const a = 1`,
		"src/a.spec.ts":       `import {a} from "./a"`,
		"src/unrelated.ts":    `export const other = 2`,
		"src/unrelated.md":    `not source`,
		"src/helper.test.tsx": `export const helper = 3`,
	} {
		full := filepath.Join(repository, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target, _ := fix.ParseRepoPath("src/a.ts")
	paths, err := (UnitScopePlanner{}).Plan(context.Background(), fix.WorkspaceIdentity{RepositoryRoot: repository, AnalysisRoot: repository}, []fix.RepoPath{target}, "targets-and-tests")
	if err != nil {
		t.Fatal(err)
	}
	want := map[fix.RepoPath]bool{"src/a.ts": true, "src/a.spec.ts": true, "src/helper.test.tsx": true}
	if len(paths) != len(want) {
		t.Fatalf("allowed=%v", paths)
	}
	for _, path := range paths {
		if !want[path] {
			t.Fatalf("unexpected allowed path %s", path)
		}
	}
}

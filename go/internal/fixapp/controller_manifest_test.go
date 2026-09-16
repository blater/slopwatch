package fixapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func TestLargeTargetManifestContainsEverySelectedFileInCandidateStaging(t *testing.T) {
	staging, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	targets := manifestTargets(200)
	manifest, err := prepareTargetManifest(fix.CandidateIdentity{StagingRoot: staging}, targets)
	if err != nil {
		t.Fatal(err)
	}
	assertManifestMetadata(t, manifest, staging, len(targets))
	assertManifestContents(t, manifest.Path, targets)
}

func manifestTargets(count int) []fix.RepoPath {
	targets := make([]fix.RepoPath, count)
	for index := range targets {
		targets[index] = fix.RepoPath(fmt.Sprintf("src/feature-%03d/a-deliberately-long-selected-filename-for-manifest.go", index))
	}
	return targets
}

func assertManifestMetadata(t *testing.T, manifest *agent.TargetManifest, staging string, count int) {
	t.Helper()
	if manifest == nil || manifest.Count != count || filepath.Dir(filepath.Dir(manifest.Path)) != staging {
		t.Fatalf("manifest = %#v, staging = %q", manifest, staging)
	}
}

func assertManifestContents(t *testing.T, path string, targets []fix.RepoPath) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, target := range targets {
		if strings.Count(text, target.String()+"\n") != 1 {
			t.Fatalf("manifest omitted or duplicated %s", target)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest permissions = %v", info.Mode().Perm())
	}
}

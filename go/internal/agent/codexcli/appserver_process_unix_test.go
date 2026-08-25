//go:build unix

package codexcli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

func TestCloseTerminatesOwnedSameGroupDescendants(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "descendant", capture)), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	data, err := os.ReadFile(capture + ".terminated")
	if err != nil || string(data) != "terminated" {
		t.Fatalf("owned descendant was not terminated before return: data=%q err=%v", data, err)
	}
}

func TestCloseWaitsForTermIgnoringDescendantToBeKilled(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "stubborn", capture)), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	before, err := os.Stat(capture + ".writes")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	after, err := os.Stat(capture + ".writes")
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("descendant continued writing after Execute returned: %d -> %d", before.Size(), after.Size())
	}
}

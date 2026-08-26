//go:build unix

package codexcli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	data, err := os.ReadFile(capture + ".pid")
	if err != nil {
		t.Fatalf("owned descendant did not start: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("invalid owned descendant pid %q: %v", data, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned descendant %d survived Execute return: %v", pid, err)
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

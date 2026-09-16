//go:build unix || windows

package analysiscache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	lockHelperEnvironment = "SLOPWATCH_FILELOCK_HELPER"
	lockRootEnvironment   = "SLOPWATCH_FILELOCK_ROOT"
	lockGateEnvironment   = "SLOPWATCH_FILELOCK_GATE"
	lockIDEnvironment     = "SLOPWATCH_FILELOCK_ID"
)

func TestGenerationCommitUsesCrossProcessLock(t *testing.T) {
	if os.Getenv(lockHelperEnvironment) == "1" {
		runGenerationCommitHelper(t)
		return
	}

	root := filepath.Join(t.TempDir(), "cache")
	gate := filepath.Join(t.TempDir(), "start")
	const writers = 8
	type child struct {
		command *exec.Cmd
		output  *childOutput
	}
	children := make([]child, 0, writers)
	for index := 0; index < writers; index++ {
		output := &childOutput{}
		command := exec.Command(os.Args[0], "-test.run=^TestGenerationCommitUsesCrossProcessLock$", "-test.count=1")
		command.Env = append(os.Environ(),
			lockHelperEnvironment+"=1",
			lockRootEnvironment+"="+root,
			lockGateEnvironment+"="+gate,
			fmt.Sprintf("%s=%d", lockIDEnvironment, index),
		)
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, child{command: command, output: output})
	}
	if err := os.WriteFile(gate, []byte("start"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, item := range children {
		if err := item.command.Wait(); err != nil {
			t.Fatalf("generation writer failed: %v\n%s", err, item.output.data)
		}
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	view := keyFor([]byte("cross-process-view"))
	generation, ok := store.LoadGeneration(view)
	if !ok || generation.Number != writers {
		t.Fatalf("final generation = %#v, %v; want number %d", generation, ok, writers)
	}
	entries, err := os.ReadDir(filepath.Join(store.workspaceDir(view), "generations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != writers {
		t.Fatalf("generation files = %d, want %d", len(entries), writers)
	}
}

type childOutput struct{ data []byte }

func (output *childOutput) Write(data []byte) (int, error) {
	output.data = append(output.data, data...)
	return len(data), nil
}

func runGenerationCommitHelper(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv(lockGateEnvironment)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for generation commit gate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	store, err := NewStore(os.Getenv(lockRootEnvironment))
	if err != nil {
		t.Fatal(err)
	}
	view := keyFor([]byte("cross-process-view"))
	projection := ArtifactRef{Digest: DigestBytes([]byte("projection-" + os.Getenv(lockIDEnvironment)))}
	if _, err := store.CommitGeneration(view, Generation{Projection: projection}); err != nil {
		t.Fatal(err)
	}
}

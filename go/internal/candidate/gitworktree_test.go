package candidate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/blater/slopwatch/internal/isolation"
)

const testCandidateCommandOutputBytes = int64(4 << 20)

func testGitWorktreeConfig() GitWorktreeConfig {
	return GitWorktreeConfig{DiscoveryCommandOutputBytes: testCandidateCommandOutputBytes}
}

type directExecutor struct{}

func (directExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = request.Environment
	command.Stdin = nil
	command.Stdin = bytes.NewReader(request.Stdin)
	stdout, err := command.Output()
	result := isolation.Result{Stdout: stdout}
	if err == nil {
		return result, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
		result.Stderr = exit.Stderr
		return result, nil
	}
	return result, err
}

type cancelAfterWorktreeAddExecutor struct {
	once   sync.Once
	cancel context.CancelFunc
}

func (executor *cancelAfterWorktreeAddExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	result, err := (directExecutor{}).Run(ctx, request)
	for index := 0; err == nil && result.Successful() && index+1 < len(request.Arguments); index++ {
		if request.Arguments[index] == "worktree" && request.Arguments[index+1] == "add" {
			executor.once.Do(executor.cancel)
			break
		}
	}
	return result, err
}

func initializeRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init", "-q")
	gitRun(t, repository, "config", "user.name", "Test")
	gitRun(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "main.go")
	gitRun(t, repository, "commit", "-q", "-m", "base")
	return repository
}

func gitRun(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func gitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, _ := command.Output()
	return string(output)
}

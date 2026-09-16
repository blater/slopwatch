package delivery

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/isolation"
)

type testExecutor struct{}

func (testExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = request.Environment
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

type executorFunc func(context.Context, isolation.Request) (isolation.Result, error)

func (function executorFunc) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	return function(ctx, request)
}

func deliveryRepository(t *testing.T) (string, string) {
	t.Helper()
	repository := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitCommand(t, repository, "init", "-q")
	gitCommand(t, repository, "config", "user.name", "Test")
	gitCommand(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "add", "main.go")
	gitCommand(t, repository, "commit", "-q", "-m", "base")
	gitCommand(t, repository, "init", "-q", "--bare", remote)
	gitCommand(t, repository, "remote", "add", "origin", remote)
	return repository, remote
}

func gitCommand(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func gitResult(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return string(output)
}

package validation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestRunnerUsesArgvAndRequiresStableCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{result: isolation.Result{ExitCode: 0, Stdout: []byte("ok")}}
	runner, _ := NewRunner(executor)
	result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{{ID: "test", Executable: "/usr/bin/true", Arguments: []string{"literal;not-shell"}, Required: true}}})
	if err != nil || !result.Passed || !result.Stable() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := executor.requests[0].Arguments; len(got) != 1 || got[0] != "literal;not-shell" {
		t.Fatalf("arguments = %q", got)
	}
}

func TestRunnerFailsWhenCheckMutatesCandidate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{result: isolation.Result{ExitCode: 0}, run: func(isolation.Request) {
		_ = os.WriteFile(path, []byte("after"), 0o600)
	}}
	runner, _ := NewRunner(executor)
	result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{{ID: "test", Executable: "/usr/bin/true", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.Stable() {
		t.Fatalf("mutating validation passed: %+v", result)
	}
}

func TestRunnerFailsClosedWithoutConfiningExecutor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, _ := NewRunner(DenyAllExecutor{})
	_, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{{ID: "test", Executable: "/usr/bin/true", Required: true}}})
	if err == nil {
		t.Fatal("validation ran without proven confinement")
	}
}

func TestRunnerDetectsModeAndSymlinkTargetMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		setup  func(string) string
		mutate func(string)
	}{
		{name: "mode", setup: func(root string) string {
			path := filepath.Join(root, "tool")
			_ = os.WriteFile(path, []byte("x"), 0o644)
			return path
		}, mutate: func(path string) { _ = os.Chmod(path, 0o755) }},
		{name: "symlink", setup: func(root string) string {
			path := filepath.Join(root, "link")
			_ = os.Symlink("one", path)
			return path
		}, mutate: func(path string) { _ = os.Remove(path); _ = os.Symlink("two", path) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := test.setup(root)
			executor := &recordingExecutor{result: isolation.Result{ExitCode: 0}, run: func(isolation.Request) { test.mutate(path) }}
			runner, _ := NewRunner(executor)
			result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{{ID: "test", Executable: "/usr/bin/true", Required: true}}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Passed || result.Stable() {
				t.Fatalf("mutation passed: %+v", result)
			}
		})
	}
}

type recordingExecutor struct {
	requests []isolation.Request
	result   isolation.Result
	run      func(isolation.Request)
}

func (executor *recordingExecutor) RunValidation(_ context.Context, _ fix.CandidateIdentity, request isolation.Request) (isolation.Result, isolation.Conformance, error) {
	executor.requests = append(executor.requests, request)
	if executor.run != nil {
		executor.run(request)
	}
	return executor.result, isolation.Conformance{CandidateWrite: true, OutsideWriteDenied: true, GitMetadataDenied: true, SensitiveReadsDenied: true, ToolNetworkPolicy: true, CrashContainment: true}, nil
}

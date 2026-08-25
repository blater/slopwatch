package validation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestRunnerUsesArgvAndRequiresStableCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{result: isolation.Result{ExitCode: 0, Stdout: []byte("ok")}}
	workspaceLimits := testWorkspaceLimits()
	runner := newTestRunner(t, executor, workspaceLimits)
	result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{validCheck("test", []string{"literal;not-shell"})}})
	if err != nil || !result.Passed || !result.Stable() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := executor.requests[0].Arguments; len(got) != 1 || got[0] != "literal;not-shell" {
		t.Fatalf("arguments = %q", got)
	}
	if request := executor.requests[0]; request.WorkspaceLimits != workspaceLimits || request.Limits.WallTime != time.Second || request.Limits.MaxStdoutBytes != 1<<10 || request.Limits.MaxStderrBytes != 1<<10 {
		t.Fatalf("explicit validation limits were not propagated unchanged: %#v", request)
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
	runner := newTestRunner(t, executor, testWorkspaceLimits())
	result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{validCheck("test", nil)}})
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
	runner := newTestRunner(t, DenyAllExecutor{}, testWorkspaceLimits())
	readiness := runner.Preflight(context.Background(), fix.WorkspaceIdentity{}, Plan{ID: "unit", Checks: []Check{validCheck("test", nil)}})
	if !readiness.Required || readiness.Ready || readiness.Diagnostic == "" {
		t.Fatalf("Preflight() = %#v", readiness)
	}
	_, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{validCheck("test", nil)}})
	if err == nil {
		t.Fatal("validation ran without proven confinement")
	}
}

func TestConfiningExecutorRoutesExactCandidatePolicy(t *testing.T) {
	backend := &recordingConfinement{capability: isolation.ConfinementCapability{Available: true, CrashContainment: true, Backend: "test"}}
	executor := ConfiningExecutor{Confinement: backend, SensitiveRoots: []string{"/secret"}}
	readiness := executor.Readiness(context.Background())
	if !readiness.Ready {
		t.Fatalf("Readiness() = %#v", readiness)
	}
	candidate := fix.CandidateIdentity{RepositoryRoot: "/candidate", GitCommonDir: "/git/common"}
	request := isolation.Request{Executable: "/usr/bin/true", Directory: "/candidate", WorkspaceLimits: testWorkspaceLimits()}
	if _, _, err := executor.RunValidation(context.Background(), candidate, request); err != nil {
		t.Fatal(err)
	}
	if backend.policy.CandidateRoot != "/candidate" || backend.policy.GitCommonDir != "/git/common" || len(backend.policy.SensitiveRoots) != 1 || backend.policy.SensitiveRoots[0] != "/secret" {
		t.Fatalf("candidate policy = %#v", backend.policy)
	}
}

type recordingConfinement struct {
	capability isolation.ConfinementCapability
	policy     isolation.CandidatePolicy
}

func (backend *recordingConfinement) Capability(context.Context) isolation.ConfinementCapability {
	return backend.capability
}
func (backend *recordingConfinement) RunCandidate(_ context.Context, policy isolation.CandidatePolicy, _ isolation.Request) (isolation.Result, isolation.Conformance, error) {
	backend.policy = policy
	return isolation.Result{ExitCode: 0}, isolation.Conformance{CandidateWrite: true, OutsideWriteDenied: true, GitMetadataDenied: true, SensitiveReadsDenied: true, ToolNetworkPolicy: true, CrashContainment: true}, nil
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
			runner := newTestRunner(t, executor, testWorkspaceLimits())
			result, err := runner.Validate(context.Background(), fix.CandidateIdentity{Job: "job", RepositoryRoot: root}, Plan{ID: "unit", Checks: []Check{validCheck("test", nil)}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Passed || result.Stable() {
				t.Fatalf("mutation passed: %+v", result)
			}
		})
	}
}

func TestTreeFingerprintRejectsOversizeSparseFilesAndNestedGitMetadata(t *testing.T) {
	limits := testWorkspaceLimits()
	limits.MaxFileBytes = 8
	t.Run("oversize sparse file", func(t *testing.T) {
		root := t.TempDir()
		file, err := os.Create(filepath.Join(root, "large.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(limits.MaxFileBytes + 1); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		if _, err := treeFingerprint(t.Context(), root, limits); err == nil {
			t.Fatal("oversize sparse file was accepted")
		}
	})
	t.Run("nested git", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "vendor", ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := treeFingerprint(t.Context(), root, limits); err == nil {
			t.Fatal("nested .git was invisibly excluded")
		}
	})
}

func TestTreeFingerprintHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := treeFingerprint(ctx, root, testWorkspaceLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("treeFingerprint error=%v", err)
	}
}

func TestRunnerRequiresExplicitWorkspaceAndCheckLimits(t *testing.T) {
	if _, err := NewRunner(&recordingExecutor{}); err == nil || !strings.Contains(err.Error(), "explicit typed configuration") {
		t.Fatalf("missing runner configuration error=%v", err)
	}
	if _, err := NewRunner(&recordingExecutor{}, RunnerConfig{Environment: []string{"PATH=/bin"}}); err == nil || !strings.Contains(err.Error(), "workspace limits") {
		t.Fatalf("missing workspace limits error=%v", err)
	}
	runner := newTestRunner(t, &recordingExecutor{}, testWorkspaceLimits())
	for _, check := range []Check{
		{ID: "timeout", Executable: "/usr/bin/true", MaxOutputBytes: 1},
		{ID: "output", Executable: "/usr/bin/true", Timeout: time.Second},
	} {
		readiness := runner.Preflight(t.Context(), fix.WorkspaceIdentity{}, Plan{ID: "unit", Checks: []Check{check}})
		if readiness.Ready || readiness.Diagnostic == "" {
			t.Fatalf("invalid check was ready: %#v", readiness)
		}
	}
}

func TestTreeFingerprintEnforcesEveryExplicitWorkspaceLimit(t *testing.T) {
	tests := []struct {
		name   string
		limits isolation.WorkspaceLimits
		setup  func(string) error
	}{
		{name: "files", limits: isolation.WorkspaceLimits{MaxFiles: 1, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			if err := os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, "b"), []byte("b"), 0o600)
		}},
		{name: "directories", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 1, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.MkdirAll(filepath.Join(root, "a", "b"), 0o700)
		}},
		{name: "paths", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 2, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "long"), []byte("a"), 0o600)
		}},
		{name: "file bytes", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 1, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "a"), []byte("ab"), 0o600)
		}},
		{name: "total bytes", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 1}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "a"), []byte("ab"), 0o600)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := test.setup(root); err != nil {
				t.Fatal(err)
			}
			if _, err := treeFingerprint(t.Context(), root, test.limits); err == nil {
				t.Fatal("workspace exceeding explicit limit was fingerprinted")
			}
		})
	}
}

func testWorkspaceLimits() isolation.WorkspaceLimits {
	return isolation.WorkspaceLimits{MaxFiles: 100, MaxDirectories: 100, MaxPathBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20}
}

func newTestRunner(t *testing.T, executor Executor, limits isolation.WorkspaceLimits) *Runner {
	t.Helper()
	runner, err := NewRunner(executor, RunnerConfig{Environment: []string{"PATH=/usr/bin:/bin"}, WorkspaceLimits: limits})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func validCheck(id CheckID, arguments []string) Check {
	return Check{ID: id, Executable: "/usr/bin/true", Arguments: arguments, Required: true, Timeout: time.Second, MaxOutputBytes: 1 << 10}
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

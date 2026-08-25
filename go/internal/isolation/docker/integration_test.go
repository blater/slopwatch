package docker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

// This opt-in contract test runs the actual image, PID 1, sentinel, tmpfs copy
// and cleanup path. CI/release builders set the three variables after building
// containers/fix-validation/Dockerfile.
func TestRealValidationImageContract(t *testing.T) {
	image := os.Getenv("SLOPWATCH_TEST_FIX_IMAGE")
	host := os.Getenv("SLOPWATCH_TEST_DOCKER_HOST")
	if image == "" || host == "" {
		t.Skip("real Docker validation contract not configured")
	}
	dockerCLI, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewCommandClient(dockerCLI, host)
	if err != nil {
		t.Fatal(err)
	}
	state, candidate := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(candidate, ".git"), []byte("gitdir: hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := []byte("package sample\n")
	if err := os.WriteFile(filepath.Join(candidate, "sample.go"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := New(Config{Client: client, Image: image, InstallationID: "integration-contract", StateRoot: state,
		SupervisorPath: "/usr/local/bin/slopmark", ProbeExecutable: "/usr/local/bin/slopmark", ExecutableMap: map[string]string{"/host/slopmark": "/usr/local/bin/slopmark"},
		PIDsLimit: 256, MemoryBytes: 4 << 30, CPUMillis: 2000, TemporaryBytes: 64 << 20, WorkspaceBytes: 8 << 20,
		NofileLimit: 1024, GeneratedFileBytes: 1 << 20, StopTimeout: 5 * time.Second, ControlTimeout: 15 * time.Second,
		SentinelWallTime: 10 * time.Second, CrashProbeWallTime: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if err := backend.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	request := isolation.Request{Executable: "/host/slopmark", Arguments: []string{"--help"}, Directory: candidate,
		Environment: []string{"PATH=/usr/bin:/bin"}, Limits: isolation.Limits{WallTime: 10 * time.Second, TerminateGrace: time.Second, MaxStdoutBytes: 1 << 20, MaxStderrBytes: 1 << 20},
		WorkspaceLimits: isolation.WorkspaceLimits{MaxFiles: 100, MaxDirectories: 100, MaxPathBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20}}
	run, conformance, err := backend.RunCandidate(ctx, isolation.CandidatePolicy{CandidateRoot: candidate, GitCommonDir: filepath.Join(state, "denied-git")}, request)
	if err != nil || !run.Successful() || !validationConformant(conformance) {
		t.Fatalf("run=%+v conformance=%+v err=%v", run, conformance, err)
	}
	contents, err := os.ReadFile(filepath.Join(candidate, "sample.go"))
	if err != nil || string(contents) != string(original) {
		t.Fatalf("host candidate changed: %q %v", contents, err)
	}
}

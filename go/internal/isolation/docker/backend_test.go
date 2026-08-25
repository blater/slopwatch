package docker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

const (
	testImage          = "example.invalid/slopwatch@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testContainerOne   = "1111111111111111111111111111111111111111111111111111111111111111"
	testContainerTwo   = "2222222222222222222222222222222222222222222222222222222222222222"
	testContainerThree = "3333333333333333333333333333333333333333333333333333333333333333"
	testContainerFour  = "4444444444444444444444444444444444444444444444444444444444444444"
)

type scriptedStep struct {
	command string
	result  isolation.Result
	err     error
}
type scriptedClient struct {
	mu       sync.Mutex
	steps    []scriptedStep
	commands []Command
}

func (client *scriptedClient) Run(_ context.Context, command Command, observe func([]byte)) (isolation.Result, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.commands = append(client.commands, command)
	if len(client.steps) == 0 {
		return isolation.Result{}, errors.New("unexpected docker command")
	}
	step := client.steps[0]
	client.steps = client.steps[1:]
	if len(command.Arguments) == 0 || command.Arguments[0] != step.command {
		return isolation.Result{}, errors.New("unexpected docker command state")
	}
	if observe != nil && len(step.result.Stdout) > 0 {
		observe(append([]byte(nil), step.result.Stdout...))
	}
	return step.result, step.err
}

func TestBackendRejectsMutableImageReference(t *testing.T) {
	if _, err := New(Config{Client: &scriptedClient{}, Image: "example/slopwatch:latest", InstallationID: "test", StateRoot: t.TempDir(), SupervisorPath: "/slopwatch", ProbeExecutable: "/slopwatch"}); err == nil {
		t.Fatal("mutable image tag was accepted")
	}
}

func TestBackendRejectsMissingOperationalPolicy(t *testing.T) {
	base := testBackendConfig(&scriptedClient{}, t.TempDir())
	tests := []struct {
		name string
		omit func(*Config)
	}{
		{name: "pids", omit: func(config *Config) { config.PIDsLimit = 0 }},
		{name: "memory", omit: func(config *Config) { config.MemoryBytes = 0 }},
		{name: "cpu", omit: func(config *Config) { config.CPUMillis = 0 }},
		{name: "temporary tmpfs", omit: func(config *Config) { config.TemporaryBytes = 0 }},
		{name: "workspace tmpfs", omit: func(config *Config) { config.WorkspaceBytes = 0 }},
		{name: "nofile", omit: func(config *Config) { config.NofileLimit = 0 }},
		{name: "generated file", omit: func(config *Config) { config.GeneratedFileBytes = 0 }},
		{name: "stop timeout", omit: func(config *Config) { config.StopTimeout = 0 }},
		{name: "control timeout", omit: func(config *Config) { config.ControlTimeout = 0 }},
		{name: "sentinel wall time", omit: func(config *Config) { config.SentinelWallTime = 0 }},
		{name: "crash probe wall time", omit: func(config *Config) { config.CrashProbeWallTime = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.omit(&config)
			if _, err := New(config); err == nil {
				t.Fatal("missing operational policy was accepted")
			}
		})
	}
}

func TestBackendRejectsControlTimeoutThatCannotCoverStop(t *testing.T) {
	config := testBackendConfig(&scriptedClient{}, t.TempDir())
	config.ControlTimeout = config.StopTimeout
	if _, err := New(config); err == nil || !strings.Contains(err.Error(), "ControlTimeout must be greater than StopTimeout") {
		t.Fatalf("New() error = %v", err)
	}
}

func TestFormatCPUMillis(t *testing.T) {
	for value, want := range map[int64]string{1: "0.001", 250: "0.25", 1500: "1.5", 2000: "2"} {
		if got := formatCPUMillis(value); got != want {
			t.Errorf("formatCPUMillis(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestNormalizeRequiresDockerStoragePolicyToCoverAdmittedWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: denied"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := isolation.Request{Executable: "/host/go", Directory: root,
		WorkspaceLimits: isolation.WorkspaceLimits{MaxFiles: 10, MaxDirectories: 10, MaxPathBytes: 4096, MaxFileBytes: 1024, MaxTotalBytes: 4096}}
	base := Config{ExecutableMap: map[string]string{"/host/go": "/usr/bin/go"}, WorkspaceBytes: 4097, GeneratedFileBytes: 1024}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "workspace tmpfs must have headroom", mutate: func(config *Config) { config.WorkspaceBytes = 4096 }, want: "tmpfs must exceed"},
		{name: "generated file must cover file admission", mutate: func(config *Config) { config.GeneratedFileBytes = 1023 }, want: "generated-file limit must cover"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.mutate(&config)
			backend := &Backend{config: config}
			if _, err := backend.normalize(isolation.CandidatePolicy{CandidateRoot: root}, request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalize() error = %v", err)
			}
		})
	}
}

func TestBackendReconcilesThenRunsExactMeasuredCandidate(t *testing.T) {
	probe, _ := json.Marshal(isolation.ProbeResult{CandidateWrite: true})
	client := &scriptedClient{steps: []scriptedStep{
		okStep("image", []byte(`["`+testImage+`"]`+"\nsha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\nv1\n")),
		okStep("ps", []byte(testContainerOne+"\n")), okStep("stop", nil), okStep("wait", []byte("0\n")), okStep("rm", nil),
		okStep("create", []byte(testContainerOne+"\n")), okStep("start", nil), okStep("rm", nil),
		okStep("create", []byte(testContainerTwo+"\n")), okStep("start", nil), okStep("rm", nil),
		okStep("create", []byte(testContainerThree+"\n")), okStep("start", probe), okStep("rm", nil),
		okStep("create", []byte(testContainerFour+"\n")), okStep("start", []byte("validated\n")), okStep("rm", nil),
	}}
	backend, root := newReadyBackend(t, client)
	var streamed string
	run, conformance, err := backend.RunCandidateStreaming(context.Background(), candidatePolicy(root), validationRequest(root), func(data []byte) { streamed += string(data) })
	if err != nil || !run.Successful() || string(run.Stdout) != "validated\n" || streamed != "validated\n" || !validationConformant(conformance) {
		t.Fatalf("run=%+v conformance=%+v stream=%q err=%v", run, conformance, streamed, err)
	}
	client.mu.Lock()
	commands := append([]Command(nil), client.commands...)
	remaining := len(client.steps)
	client.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("%d scripted docker commands were unused", remaining)
	}
	var actualCreate []string
	for _, command := range commands {
		if len(command.Arguments) > 0 && command.Arguments[0] == "create" {
			actualCreate = command.Arguments
		}
	}
	joined := strings.Join(actualCreate, "\x00")
	for _, required := range []string{"--read-only", "--network\x00none", "--cap-drop\x00ALL", "--security-opt\x00no-new-privileges", "--pids-limit", "--memory", "--cpus\x002", "--pull\x00never", "--ulimit\x00nofile=1024:1024", "--ulimit\x00fsize=1048576:1048576", "--entrypoint\x00/usr/local/bin/slopwatch", protocolLabel + "=" + protocolV1, "dst=/candidate,readonly", "dst=/candidate/.git,readonly", "/workspace:rw,nosuid,nodev,size=", "dst=/run/slopwatch/request.json,readonly", testImage, ContainerSupervisorArgument} {
		if !strings.Contains(joined, required) {
			t.Errorf("docker create argv lacks %q: %v", required, actualCreate)
		}
	}
	if strings.Contains(joined, "dst=/workspace,rw") {
		t.Fatalf("host candidate was mounted writable as workspace: %v", actualCreate)
	}
	if len(commands) < 2 || !strings.Contains(strings.Join(commands[1].Arguments, "\x00"), "label="+protocolLabel+"="+protocolV1) {
		t.Fatalf("orphan reconciliation was not scoped to the confinement protocol: %v", commands)
	}
	if !strings.Contains(strings.Join(commands[1].Arguments, "\x00"), "--no-trunc") {
		t.Fatalf("orphan reconciliation did not request full container IDs: %v", commands[1].Arguments)
	}
	if strings.Contains(joined, "docker.sock") || strings.Contains(joined, "/bin/sh") {
		t.Fatalf("unsafe socket or shell in docker argv: %v", actualCreate)
	}
	for _, command := range commands {
		if command.Limits.WallTime <= 0 || command.Limits.TerminateGrace <= 0 || command.Limits.MaxStdoutBytes <= 0 || command.Limits.MaxStderrBytes <= 0 {
			t.Fatalf("docker command %v lacked explicit limits: %+v", command.Arguments, command.Limits)
		}
	}
}

func TestReconcileRejectsImageMissingMappedExecutable(t *testing.T) {
	client := &scriptedClient{steps: []scriptedStep{
		okStep("image", []byte(`["`+testImage+`"]`+"\nsha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\nv1\n")),
		okStep("ps", nil),
		okStep("create", []byte(testContainerOne+"\n")),
		{command: "start", result: isolation.Result{ExitCode: 1, Stderr: []byte("missing executable\n")}},
		okStep("rm", nil),
	}}
	state := t.TempDir()
	config := testBackendConfig(client, state)
	config.ExecutableMap = map[string]string{"/host/go": "/usr/bin/go"}
	backend, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "validation executable probe") {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if backend.Capability(t.Context()).Available {
		t.Fatal("backend became available despite missing mapped executable")
	}
	if err := backend.ExecutableReady(t.Context(), "/host/go"); err == nil {
		t.Fatal("missing mapped executable was reported ready")
	}
	client.mu.Lock()
	commands := append([]Command(nil), client.commands...)
	client.mu.Unlock()
	if len(commands) < 4 || !strings.Contains(strings.Join(commands[2].Arguments, "\x00"), ContainerExecutableProbeArgument+"\x00/usr/bin/go") {
		t.Fatalf("mapped executable was not probed inside immutable image: %+v", commands)
	}
}

func TestAmbiguousCreateCleanupRequiresFullContainerIDAndNoTrunc(t *testing.T) {
	shortClient := &scriptedClient{steps: []scriptedStep{okStep("ps", []byte("111111111111\n"))}}
	shortBackend := &Backend{config: Config{Client: shortClient, InstallationID: "test", StopTimeout: time.Second, ControlTimeout: 10 * time.Second}}
	if err := shortBackend.cleanupRunLabel(t.Context(), "run-safe"); err == nil || !strings.Contains(err.Error(), "invalid container ID") {
		t.Fatalf("truncated container ID error=%v", err)
	}
	shortClient.mu.Lock()
	shortCommand := append([]string(nil), shortClient.commands[0].Arguments...)
	shortClient.mu.Unlock()
	if !strings.Contains(strings.Join(shortCommand, "\x00"), "--no-trunc") {
		t.Fatalf("ambiguous cleanup argv lacks --no-trunc: %v", shortCommand)
	}

	fullClient := &scriptedClient{steps: []scriptedStep{okStep("ps", []byte(testContainerOne+"\n")), okStep("stop", nil), okStep("wait", []byte("0\n")), okStep("rm", nil)}}
	fullBackend := &Backend{config: Config{Client: fullClient, InstallationID: "test", StopTimeout: time.Second, ControlTimeout: 10 * time.Second}}
	if err := fullBackend.cleanupRunLabel(t.Context(), "run-safe"); err != nil {
		t.Fatal(err)
	}
}

func TestCrashProbeAmbiguousCreateReconcilesByRunLabel(t *testing.T) {
	for _, test := range []struct {
		name   string
		create scriptedStep
	}{
		{name: "daemon created then returned failure", create: scriptedStep{command: "create", result: isolation.Result{ExitCode: 1}, err: errors.New("connection lost")}},
		{name: "invalid returned id", create: okStep("create", []byte("111111111111\n"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &scriptedClient{steps: []scriptedStep{
				test.create, okStep("ps", []byte(testContainerOne+"\n")), okStep("stop", nil), okStep("wait", []byte("0\n")), okStep("rm", nil),
			}}
			backend := &Backend{config: Config{Client: client, InstallationID: "test-installation", StateRoot: t.TempDir(),
				Image: testImage, SupervisorPath: "/usr/local/bin/slopwatch", StopTimeout: time.Second, ControlTimeout: 10 * time.Second}}
			request := isolation.Request{Limits: isolation.Limits{WallTime: time.Second, TerminateGrace: time.Millisecond}}
			if _, err := backend.runOneCrashProbe(t.Context(), isolation.CandidatePolicy{}, request, t.TempDir()); err == nil {
				t.Fatal("ambiguous crash-probe create succeeded")
			}
			client.mu.Lock()
			commands := append([]Command(nil), client.commands...)
			remaining := len(client.steps)
			client.mu.Unlock()
			if remaining != 0 || len(commands) != 5 || commands[1].Arguments[0] != "ps" ||
				!strings.Contains(strings.Join(commands[1].Arguments, "\x00"), "--no-trunc") ||
				!strings.Contains(strings.Join(commands[1].Arguments, "\x00"), "label=com.slopwatch.run=") {
				t.Fatalf("ambiguous crash-probe cleanup commands=%+v remaining=%d", commands, remaining)
			}
		})
	}
}

func TestCanceledCandidateStopsKillsWaitsAndRemovesExactContainer(t *testing.T) {
	probe, _ := json.Marshal(isolation.ProbeResult{CandidateWrite: true})
	client := &scriptedClient{steps: []scriptedStep{
		okStep("image", []byte(`["`+testImage+`"]`+"\nsha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\nv1\n")), okStep("ps", nil),
		okStep("create", []byte(testContainerOne+"\n")), okStep("start", nil), okStep("rm", nil),
		okStep("create", []byte(testContainerTwo+"\n")), okStep("start", nil), okStep("rm", nil),
		okStep("create", []byte(testContainerThree+"\n")), okStep("start", probe), okStep("rm", nil),
		okStep("create", []byte(testContainerFour+"\n")), {command: "start", result: isolation.Result{ExitCode: 130, Canceled: true}},
		{command: "stop", result: isolation.Result{ExitCode: 1}}, okStep("kill", nil), okStep("wait", []byte("137\n")), okStep("rm", nil),
	}}
	backend, root := newReadyBackend(t, client)
	run, _, err := backend.RunCandidate(context.Background(), candidatePolicy(root), validationRequest(root))
	if err != nil || !run.Canceled {
		t.Fatalf("canceled run=%+v err=%v", run, err)
	}
	client.mu.Lock()
	commands := append([]Command(nil), client.commands...)
	client.mu.Unlock()
	want := []string{"stop", "kill", "wait", "rm"}
	got := []string{}
	for _, command := range commands {
		if len(command.Arguments) > 0 {
			name := command.Arguments[0]
			if name == "stop" || name == "kill" || name == "wait" || name == "rm" {
				if command.Arguments[len(command.Arguments)-1] == testContainerFour {
					got = append(got, name)
				}
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cancellation cleanup order=%v want=%v", got, want)
	}
}

func newReadyBackend(t *testing.T, client *scriptedClient) (*Backend, string) {
	t.Helper()
	state := t.TempDir()
	root := filepath.Join(t.TempDir(), "candidate")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: denied"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := New(testBackendConfig(client, state))
	if err != nil {
		t.Fatal(err)
	}
	if backend.Capability(context.Background()).Available {
		t.Fatal("backend was ready before orphan/crash reconciliation")
	}
	if err := backend.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !backend.Capability(context.Background()).Available {
		t.Fatal("backend unavailable after reconciliation")
	}
	return backend, root
}

func testBackendConfig(client Client, state string) Config {
	return Config{
		Client: client, Image: testImage, InstallationID: "test-installation", StateRoot: state,
		SupervisorPath: "/usr/local/bin/slopwatch", ProbeExecutable: "/usr/local/bin/slopwatch",
		ExecutableMap: map[string]string{"/usr/bin/go": "/usr/bin/go"}, User: "1000:1000",
		PIDsLimit: 256, MemoryBytes: 4 << 30, CPUMillis: 2000,
		TemporaryBytes: 64 << 20, WorkspaceBytes: 8 << 20,
		NofileLimit: 1024, GeneratedFileBytes: 1 << 20,
		StopTimeout: time.Second, ControlTimeout: 10 * time.Second,
		SentinelWallTime: 10 * time.Second, CrashProbeWallTime: 10 * time.Second,
	}
}
func candidatePolicy(root string) isolation.CandidatePolicy {
	return isolation.CandidatePolicy{CandidateRoot: root, GitCommonDir: filepath.Join(filepath.Dir(root), "sensitive-git"), SensitiveRoots: []string{filepath.Join(filepath.Dir(root), "sensitive")}}
}
func validationRequest(root string) isolation.Request {
	return isolation.Request{Executable: "/usr/bin/go", Arguments: []string{"test", "./..."}, Directory: root, Environment: []string{"PATH=/usr/bin:/bin"},
		Limits:          isolation.Limits{WallTime: time.Minute, TerminateGrace: time.Second, MaxStdoutBytes: 1 << 20, MaxStderrBytes: 1 << 20},
		WorkspaceLimits: isolation.WorkspaceLimits{MaxFiles: 100, MaxDirectories: 100, MaxPathBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20}}
}
func okStep(command string, stdout []byte) scriptedStep {
	return scriptedStep{command: command, result: isolation.Result{ExitCode: 0, Stdout: stdout}}
}

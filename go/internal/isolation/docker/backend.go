// Package docker implements candidate confinement with an installation-owned
// Linux image and the Docker CLI. It never invokes a shell.
package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

const (
	protocolLabel = "com.slopwatch.confinement.protocol"
	protocolV1    = "v1"
	workspacePath = "/workspace"
	requestPath   = "/run/slopwatch/request.json"
)

var (
	digestPattern       = regexp.MustCompile(`^(?:[a-zA-Z0-9][a-zA-Z0-9._/-]*@)?sha256:[0-9a-f]{64}$`)
	installationPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	containerIDPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Command struct {
	Arguments []string
	Stdin     []byte
	Limits    isolation.Limits
	Attach    bool
}

// Client is deliberately argv-oriented. CommandClient is the production
// implementation; tests use a deterministic fake state machine.
type Client interface {
	Run(context.Context, Command, func([]byte)) (isolation.Result, error)
}

type Config struct {
	Client          Client
	Image           string
	InstallationID  string
	StateRoot       string
	SupervisorPath  string
	ProbeExecutable string
	ExecutableMap   map[string]string
	PIDsLimit       int
	MemoryBytes     int64
	// CPUMillis is a fixed-point CPU quota: 1000 means one logical CPU.
	CPUMillis      int64
	User           string
	TemporaryBytes int64
	WorkspaceBytes int64
	NofileLimit    int64
	// GeneratedFileBytes becomes the container RLIMIT_FSIZE hard and soft cap.
	GeneratedFileBytes int64
	StopTimeout        time.Duration
	// ControlTimeout bounds each Docker lifecycle command and must exceed StopTimeout.
	ControlTimeout time.Duration
	// SentinelWallTime bounds the candidate-specific filesystem/network probe.
	SentinelWallTime   time.Duration
	CrashProbeWallTime time.Duration
}

type Backend struct {
	config              Config
	mu                  sync.RWMutex
	reconciled          bool
	crashMeasured       bool
	executablesVerified bool
	diagnostic          string
}

func New(config Config) (*Backend, error) {
	if config.Client == nil || !digestPattern.MatchString(config.Image) || !installationPattern.MatchString(config.InstallationID) ||
		config.StateRoot == "" || !filepath.IsAbs(config.StateRoot) || !strings.HasPrefix(config.SupervisorPath, "/") || !strings.HasPrefix(config.ProbeExecutable, "/") {
		return nil, errors.New("docker confinement configuration is incomplete or not immutable")
	}
	canonical, err := filepath.EvalSymlinks(config.StateRoot)
	if err != nil {
		return nil, fmt.Errorf("canonicalize docker confinement state: %w", err)
	}
	config.StateRoot = canonical
	for _, required := range []struct {
		name    string
		missing bool
	}{
		{name: "PIDsLimit", missing: config.PIDsLimit <= 0},
		{name: "MemoryBytes", missing: config.MemoryBytes <= 0},
		{name: "CPUMillis", missing: config.CPUMillis <= 0},
		{name: "TemporaryBytes", missing: config.TemporaryBytes <= 0},
		{name: "WorkspaceBytes", missing: config.WorkspaceBytes <= 0},
		{name: "NofileLimit", missing: config.NofileLimit <= 0},
		{name: "GeneratedFileBytes", missing: config.GeneratedFileBytes <= 0},
		{name: "StopTimeout", missing: config.StopTimeout <= 0},
		{name: "ControlTimeout", missing: config.ControlTimeout <= 0},
		{name: "SentinelWallTime", missing: config.SentinelWallTime <= 0},
		{name: "CrashProbeWallTime", missing: config.CrashProbeWallTime <= 0},
	} {
		if required.missing {
			return nil, fmt.Errorf("docker confinement %s must be explicitly positive", required.name)
		}
	}
	if config.User == "" {
		config.User = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	if !regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`).MatchString(config.User) {
		return nil, errors.New("docker confinement requires a numeric non-root uid:gid")
	}
	if config.ControlTimeout <= config.StopTimeout {
		return nil, errors.New("docker confinement ControlTimeout must be greater than StopTimeout")
	}
	config.ExecutableMap = cloneMap(config.ExecutableMap)
	if !safeMountPath(config.StateRoot) {
		return nil, errors.New("docker confinement state root contains unsupported mount characters")
	}
	return &Backend{config: config, diagnostic: "docker confinement requires exclusive-lease orphan reconciliation and a crash probe"}, nil
}

func (backend *Backend) Capability(context.Context) isolation.ConfinementCapability {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	available := backend.reconciled && backend.crashMeasured && backend.executablesVerified
	return isolation.ConfinementCapability{Available: available, CrashContainment: available, Backend: "docker-linux-v1", Diagnostic: backend.diagnostic}
}

// Reconcile must be called while the caller holds its installation-wide
// exclusive lease. Until it succeeds, Capability remains unavailable.
func (backend *Backend) Reconcile(ctx context.Context) error {
	if err := backend.verifyImage(ctx); err != nil {
		backend.setUnavailable(err)
		return err
	}
	list, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"ps", "-aq", "--no-trunc", "--filter", "label=com.slopwatch.installation=" + backend.config.InstallationID,
		"--filter", "label=" + protocolLabel + "=" + protocolV1}, Limits: backend.controlLimits()}, nil)
	if err != nil || !list.Successful() || list.StdoutTruncated {
		err = errors.Join(err, errors.New("list owned docker confinement containers failed"))
		backend.setUnavailable(err)
		return err
	}
	for _, id := range strings.Fields(string(list.Stdout)) {
		if !containerIDPattern.MatchString(id) {
			err := errors.New("docker returned an invalid owned container ID")
			backend.setUnavailable(err)
			return err
		}
		if err := backend.cleanup(context.Background(), id, true); err != nil {
			backend.setUnavailable(err)
			return fmt.Errorf("reconcile owned docker container %s: %w", id[:12], err)
		}
	}
	if err := backend.verifyMappedExecutables(ctx); err != nil {
		backend.setUnavailable(err)
		return err
	}
	if err := backend.measureCrashContainment(ctx); err != nil {
		backend.setUnavailable(err)
		return err
	}
	backend.mu.Lock()
	backend.reconciled, backend.crashMeasured, backend.executablesVerified = true, true, true
	backend.diagnostic = "docker image, mapped executables, orphan reconciliation, and PID-namespace escape probe passed"
	backend.mu.Unlock()
	return nil
}

// ExecutableReady proves that an installation-owned host executable maps to a
// path that was observed as an executable regular file in the immutable image.
func (backend *Backend) ExecutableReady(_ context.Context, executable string) error {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if !backend.executablesVerified {
		return errors.New("validation image executables have not been verified")
	}
	if _, ok := backend.config.ExecutableMap[executable]; !ok {
		return fmt.Errorf("validation executable %q is not mapped into the immutable image", executable)
	}
	return nil
}

func (backend *Backend) RunCandidate(ctx context.Context, policy isolation.CandidatePolicy, request isolation.Request) (isolation.Result, isolation.Conformance, error) {
	return backend.RunCandidateStreaming(ctx, policy, request, nil)
}

// RunCandidateStreaming is the optional streaming confinement seam consumed by
// structured provider adapters; validation uses the buffered method above.
func (backend *Backend) RunCandidateStreaming(ctx context.Context, policy isolation.CandidatePolicy, request isolation.Request, observe func([]byte)) (isolation.Result, isolation.Conformance, error) {
	capability := backend.Capability(ctx)
	if !capability.Available || !capability.CrashContainment {
		return isolation.Result{}, isolation.Conformance{Diagnostic: capability.Diagnostic}, errors.New(capability.Diagnostic)
	}
	normalized, err := backend.normalize(policy, request)
	if err != nil {
		return isolation.Result{}, isolation.Conformance{Diagnostic: err.Error()}, err
	}
	conformance, err := backend.measureCandidate(ctx, normalized.policy, normalized.hostRoot, normalized.request.WorkspaceLimits)
	if err != nil || !validationConformant(conformance) {
		if err == nil {
			err = fmt.Errorf("docker candidate confinement gates failed: %v", conformance.FailedGates())
		}
		return isolation.Result{}, conformance, err
	}
	run, err := backend.runOne(ctx, normalized.policy, normalized.request, normalized.hostRoot, observe)
	return run, conformance, err
}

type normalizedRun struct {
	policy   isolation.CandidatePolicy
	request  isolation.Request
	hostRoot string
}

func (backend *Backend) normalize(policy isolation.CandidatePolicy, request isolation.Request) (normalizedRun, error) {
	if err := request.WorkspaceLimits.Validate(); err != nil {
		return normalizedRun{}, fmt.Errorf("docker candidate workspace limits: %w", err)
	}
	if backend.config.WorkspaceBytes <= request.WorkspaceLimits.MaxTotalBytes {
		return normalizedRun{}, errors.New("docker workspace tmpfs must exceed the admitted candidate total-byte limit")
	}
	if backend.config.GeneratedFileBytes < request.WorkspaceLimits.MaxFileBytes {
		return normalizedRun{}, errors.New("docker generated-file limit must cover the admitted candidate per-file limit")
	}
	root, err := filepath.EvalSymlinks(policy.CandidateRoot)
	if err != nil {
		return normalizedRun{}, errors.New("docker candidate root must be an existing path")
	}
	if !safeMountPath(root) {
		return normalizedRun{}, errors.New("docker candidate root contains unsupported mount characters")
	}
	gitInfo, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !gitInfo.Mode().IsRegular() {
		return normalizedRun{}, errors.New("docker candidate requires a maskable worktree .git file")
	}
	working, err := filepath.EvalSymlinks(request.Directory)
	if err != nil || !within(root, working) {
		return normalizedRun{}, errors.New("docker candidate working directory escapes candidate")
	}
	relative, _ := filepath.Rel(root, working)
	inImageExecutable, ok := backend.config.ExecutableMap[request.Executable]
	if !ok || !strings.HasPrefix(inImageExecutable, "/") {
		return normalizedRun{}, fmt.Errorf("docker image has no trusted executable mapping for %q", request.Executable)
	}
	request.Executable = inImageExecutable
	request.Directory = workspacePath
	if relative != "." {
		request.Directory = filepath.ToSlash(filepath.Join(workspacePath, relative))
	}
	policy.CandidateRoot = root
	return normalizedRun{policy: policy, request: request, hostRoot: root}, nil
}

func (backend *Backend) measureCandidate(ctx context.Context, policy isolation.CandidatePolicy, hostRoot string, workspaceLimits isolation.WorkspaceLimits) (isolation.Conformance, error) {
	nonce, err := isolation.NewProbeNonce()
	if err != nil {
		return isolation.Conformance{}, err
	}
	payload, err := json.Marshal(isolation.ProbeRequest{CandidateRoot: workspacePath, GitCommonDir: policy.GitCommonDir,
		OutsideRoot: "/slopwatch-denied/outside", SensitiveRoots: append([]string(nil), policy.SensitiveRoots...), Network: "tcp", NetworkAddress: "127.0.0.1:9", Nonce: nonce})
	if err != nil {
		return isolation.Conformance{}, err
	}
	request := isolation.Request{Executable: backend.config.ProbeExecutable, Arguments: []string{isolation.ProbeArgument}, Directory: workspacePath,
		Environment: []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin"}, Stdin: payload,
		Limits: backend.probeLimits(backend.config.SentinelWallTime), WorkspaceLimits: workspaceLimits}
	run, err := backend.runOne(ctx, policy, request, hostRoot, nil)
	if err != nil || !run.Successful() || run.StdoutTruncated {
		return isolation.Conformance{Diagnostic: "docker filesystem/network sentinel failed"}, errors.Join(err, errors.New("docker filesystem/network sentinel failed"))
	}
	var observed isolation.ProbeResult
	if err := json.Unmarshal(run.Stdout, &observed); err != nil {
		return isolation.Conformance{Diagnostic: "docker sentinel returned invalid JSON"}, err
	}
	result := isolation.Conformance{CandidateWrite: observed.CandidateWrite, OutsideWriteDenied: !observed.OutsideWrite,
		GitMetadataDenied: !observed.GitMetadataRead && !observed.GitMetadataWrite, SensitiveReadsDenied: !observed.SensitiveRead,
		ToolNetworkPolicy: !observed.NetworkConnected, CrashContainment: true}
	if !validationConformant(result) {
		result.Diagnostic = fmt.Sprintf("docker candidate confinement gates failed: %v", result.FailedGates())
	}
	return result, nil
}

func validationConformant(value isolation.Conformance) bool {
	return value.CandidateWrite && value.OutsideWriteDenied && value.GitMetadataDenied && value.SensitiveReadsDenied && value.ToolNetworkPolicy && value.CrashContainment
}

func (backend *Backend) setUnavailable(err error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.reconciled, backend.crashMeasured = false, false
	backend.diagnostic = err.Error()
}

func cloneMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func within(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func safeMountPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, ",\x00\r\n")
}

func secondsCeiling(value time.Duration) string {
	seconds := int64((value + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

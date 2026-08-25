package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

const (
	// Fixed-shape Docker control/probe output is a non-disableable protocol
	// invariant. It is intentionally separate from user validation output.
	dockerControlOutputBytes  = int64(1 << 20)
	dockerProbeOutputBytes    = int64(64 << 10)
	controlTerminateGrace     = 2 * time.Second
	probeTerminateGrace       = time.Second
	crashProbeDelay           = 750 * time.Millisecond
	crashProbeObservationWait = 250 * time.Millisecond
)

func (backend *Backend) verifyImage(ctx context.Context) error {
	format := `{{json .RepoDigests}}` + "\n" + `{{.Id}}` + "\n" + `{{index .Config.Labels "` + protocolLabel + `"}}`
	result, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"image", "inspect", "--format", format, backend.config.Image}, Limits: backend.controlLimits()}, nil)
	if err != nil || !result.Successful() || result.StdoutTruncated {
		return errors.Join(err, errors.New("inspect immutable docker confinement image failed"))
	}
	lines := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
	if len(lines) != 3 || lines[2] != protocolV1 {
		return errors.New("docker confinement image lacks the required protocol label")
	}
	if strings.Contains(backend.config.Image, "@") {
		var digests []string
		if json.Unmarshal([]byte(lines[0]), &digests) != nil || !contains(digests, backend.config.Image) {
			return errors.New("docker confinement image digest was not verified")
		}
	} else if lines[1] != backend.config.Image {
		return errors.New("docker confinement image ID was not verified")
	}
	return nil
}

func (backend *Backend) verifyMappedExecutables(ctx context.Context) error {
	paths := make([]string, 0, len(backend.config.ExecutableMap))
	seen := make(map[string]struct{}, len(backend.config.ExecutableMap))
	for _, path := range backend.config.ExecutableMap {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
			return errors.New("validation executable map contains an unsafe in-image path")
		}
		if _, exists := seen[path]; !exists {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	runID, err := randomID()
	if err != nil {
		return err
	}
	name := "slopwatch-" + backend.config.InstallationID + "-" + runID
	arguments := []string{"create", "--name", name,
		"--label", "com.slopwatch.installation=" + backend.config.InstallationID,
		"--label", protocolLabel + "=" + protocolV1,
		"--label", "com.slopwatch.run=" + runID,
		"--pull", "never", "--read-only", "--network", "none", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--pids-limit", fmt.Sprint(backend.config.PIDsLimit),
		"--memory", fmt.Sprint(backend.config.MemoryBytes), "--cpus", formatCPUMillis(backend.config.CPUMillis),
		"--user", backend.config.User,
		"--ulimit", "nofile=" + strconv.FormatInt(backend.config.NofileLimit, 10) + ":" + strconv.FormatInt(backend.config.NofileLimit, 10),
		"--ulimit", "fsize=" + strconv.FormatInt(backend.config.GeneratedFileBytes, 10) + ":" + strconv.FormatInt(backend.config.GeneratedFileBytes, 10),
		"--entrypoint", backend.config.SupervisorPath,
		backend.config.Image, ContainerExecutableProbeArgument}
	arguments = append(arguments, paths...)
	created, err := backend.config.Client.Run(ctx, Command{Arguments: arguments, Limits: backend.controlLimits()}, nil)
	if err != nil || !created.Successful() || created.StdoutTruncated {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		return errors.Join(err, cleanupErr, errors.New("create validation executable probe failed"))
	}
	containerID := strings.TrimSpace(string(created.Stdout))
	if !containerIDPattern.MatchString(containerID) {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		return errors.Join(cleanupErr, errors.New("validation executable probe returned an invalid container ID"))
	}
	run, runErr := backend.config.Client.Run(ctx, Command{Arguments: []string{"start", "-a", containerID}, Limits: backend.controlLimits(), Attach: true}, nil)
	cleanupErr := backend.cleanup(context.Background(), containerID, ctx.Err() != nil || run.Canceled || run.TimedOut)
	if runErr != nil || !run.Successful() || run.StdoutTruncated || run.StderrTruncated {
		return errors.Join(runErr, cleanupErr, errors.New("immutable image validation executable probe failed"))
	}
	return cleanupErr
}

func (backend *Backend) runOne(ctx context.Context, policy isolation.CandidatePolicy, request isolation.Request, hostRoot string, observe func([]byte)) (isolation.Result, error) {
	runID, err := randomID()
	if err != nil {
		return isolation.Result{}, err
	}
	runRoot, err := os.MkdirTemp(backend.config.StateRoot, ".container-run-")
	if err != nil {
		return isolation.Result{}, fmt.Errorf("create docker run state: %w", err)
	}
	defer os.RemoveAll(runRoot)
	if err := os.Chmod(runRoot, 0o700); err != nil {
		return isolation.Result{}, err
	}
	mask := filepath.Join(runRoot, "git-mask")
	if err := os.WriteFile(mask, nil, 0); err != nil {
		return isolation.Result{}, err
	}
	requestFile := filepath.Join(runRoot, "request.json")
	encoded, err := json.Marshal(containerRequest{Version: 1, Request: request})
	if err != nil {
		return isolation.Result{}, err
	}
	if err := os.WriteFile(requestFile, encoded, 0o600); err != nil {
		return isolation.Result{}, err
	}

	name := "slopwatch-" + backend.config.InstallationID + "-" + runID
	arguments := backend.createArguments(name, runID, hostRoot, mask, requestFile, request.Limits, true)
	created, err := backend.config.Client.Run(ctx, Command{Arguments: arguments, Limits: backend.controlLimits()}, nil)
	if err != nil || !created.Successful() || created.StdoutTruncated {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		if cleanupErr != nil {
			backend.setUnavailable(cleanupErr)
		}
		return isolation.Result{}, errors.Join(err, cleanupErr, errors.New("create confined docker container failed"))
	}
	containerID := strings.TrimSpace(string(created.Stdout))
	if !containerIDPattern.MatchString(containerID) {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		if cleanupErr != nil {
			backend.setUnavailable(cleanupErr)
		}
		return isolation.Result{}, errors.Join(cleanupErr, errors.New("docker create returned an invalid container ID"))
	}

	run, runErr := backend.config.Client.Run(ctx, Command{Arguments: []string{"start", "-a", "-i", containerID}, Limits: request.Limits, Attach: true}, observe)
	canceled := ctx.Err() != nil || run.Canceled || run.TimedOut
	cleanupErr := backend.cleanup(context.Background(), containerID, canceled)
	if cleanupErr != nil {
		backend.setUnavailable(cleanupErr)
		return run, errors.Join(runErr, cleanupErr)
	}
	return run, runErr
}

func (backend *Backend) cleanupRunLabel(ctx context.Context, runID string) error {
	list, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"ps", "-aq", "--no-trunc", "--filter", "label=com.slopwatch.installation=" + backend.config.InstallationID,
		"--filter", "label=com.slopwatch.run=" + runID}, Limits: backend.controlLimits()}, nil)
	if err != nil || !list.Successful() || list.StdoutTruncated {
		return errors.Join(err, errors.New("reconcile ambiguous docker create failed"))
	}
	for _, id := range strings.Fields(string(list.Stdout)) {
		if !containerIDPattern.MatchString(id) {
			return errors.New("ambiguous docker create returned an invalid container ID")
		}
		if err := backend.cleanup(ctx, id, true); err != nil {
			return err
		}
	}
	return nil
}

func (backend *Backend) createArguments(name, runID, hostRoot, mask, requestFile string, limits isolation.Limits, sourceReadonly bool) []string {
	sourceMode := "readonly"
	if !sourceReadonly {
		sourceMode = "rw"
	}
	arguments := []string{"create", "--name", name,
		"--label", "com.slopwatch.installation=" + backend.config.InstallationID,
		"--label", protocolLabel + "=" + protocolV1,
		"--label", "com.slopwatch.run=" + runID,
		"--pull", "never", "--read-only", "--network", "none", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--pids-limit", fmt.Sprint(backend.config.PIDsLimit),
		"--memory", fmt.Sprint(backend.config.MemoryBytes), "--cpus", formatCPUMillis(backend.config.CPUMillis),
		"--user", backend.config.User,
		"--ulimit", "nofile=" + strconv.FormatInt(backend.config.NofileLimit, 10) + ":" + strconv.FormatInt(backend.config.NofileLimit, 10),
		"--ulimit", "fsize=" + strconv.FormatInt(backend.config.GeneratedFileBytes, 10) + ":" + strconv.FormatInt(backend.config.GeneratedFileBytes, 10),
		"--stop-timeout", secondsCeiling(backend.config.StopTimeout), "-i",
		"--mount", "type=bind,src=" + hostRoot + ",dst=" + candidateSourcePath + "," + sourceMode,
		"--mount", "type=bind,src=" + mask + ",dst=" + candidateSourcePath + "/.git,readonly",
		"--mount", "type=bind,src=" + requestFile + ",dst=" + requestPath + ",readonly",
		"--tmpfs", workspacePath + ":rw,nosuid,nodev,size=" + fmt.Sprint(backend.config.WorkspaceBytes),
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=" + fmt.Sprint(backend.config.TemporaryBytes),
		"--entrypoint", backend.config.SupervisorPath,
		backend.config.Image, ContainerSupervisorArgument, requestPath}
	_ = limits // wall/output limits are enforced by PID 1 and the attach client.
	return arguments
}

func (backend *Backend) cleanup(ctx context.Context, containerID string, interrupt bool) error {
	if !containerIDPattern.MatchString(containerID) {
		return errors.New("refuse cleanup of invalid docker container ID")
	}
	var result error
	if interrupt {
		stop, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"stop", "--time", secondsCeiling(backend.config.StopTimeout), containerID}, Limits: backend.controlLimits()}, nil)
		if err != nil || !stop.Successful() {
			kill, killErr := backend.config.Client.Run(ctx, Command{Arguments: []string{"kill", containerID}, Limits: backend.controlLimits()}, nil)
			if killErr != nil || !kill.Successful() {
				result = errors.Join(result, err, killErr, errors.New("docker stop/kill failed"))
			}
		}
		wait, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"wait", containerID}, Limits: backend.controlLimits()}, nil)
		if err != nil || wait.StdoutTruncated {
			result = errors.Join(result, err, errors.New("docker wait failed"))
		}
	}
	removed, err := backend.config.Client.Run(ctx, Command{Arguments: []string{"rm", "-f", containerID}, Limits: backend.controlLimits()}, nil)
	if err != nil || !removed.Successful() {
		result = errors.Join(result, err, errors.New("docker exact removal failed"))
	}
	return result
}

func (backend *Backend) measureCrashContainment(ctx context.Context) error {
	root, err := os.MkdirTemp(backend.config.StateRoot, ".crash-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("masked"), 0o600); err != nil {
		return err
	}
	marker := filepath.Join(root, ".escaped-descendant")
	request := isolation.Request{Executable: backend.config.ProbeExecutable,
		Arguments: []string{ContainerEscapeProbeArgument, candidateSourcePath + "/.escaped-descendant", crashProbeDelay.String()}, Directory: workspacePath,
		Environment:     []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin"},
		Limits:          backend.probeLimits(backend.config.CrashProbeWallTime),
		WorkspaceLimits: isolation.WorkspaceLimits{MaxFiles: 16, MaxDirectories: 16, MaxPathBytes: 4 << 10, MaxFileBytes: 1 << 20, MaxTotalBytes: 1 << 20}}
	policy := isolation.CandidatePolicy{CandidateRoot: root, GitCommonDir: filepath.Join(root, "denied-git")}
	// This dedicated empty probe root is the only host path ever mounted RW;
	// production candidates always use runOne's read-only source mount.
	run, err := backend.runOneCrashProbe(ctx, policy, request, root)
	if err != nil || !run.Successful() {
		return errors.Join(err, errors.New("docker setsid escape probe failed to execute"))
	}
	timer := time.NewTimer(crashProbeDelay + crashProbeObservationWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if _, err := os.Stat(marker); err == nil {
		return errors.New("setsid descendant survived docker PID 1 exit")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (backend *Backend) runOneCrashProbe(ctx context.Context, policy isolation.CandidatePolicy, request isolation.Request, hostRoot string) (isolation.Result, error) {
	runID, err := randomID()
	if err != nil {
		return isolation.Result{}, err
	}
	runRoot, err := os.MkdirTemp(backend.config.StateRoot, ".crash-container-run-")
	if err != nil {
		return isolation.Result{}, err
	}
	defer os.RemoveAll(runRoot)
	mask := filepath.Join(runRoot, "git-mask")
	requestFile := filepath.Join(runRoot, "request.json")
	if err := os.WriteFile(mask, nil, 0o600); err != nil {
		return isolation.Result{}, err
	}
	encoded, err := json.Marshal(containerRequest{Version: 1, Request: request})
	if err != nil {
		return isolation.Result{}, err
	}
	if err := os.WriteFile(requestFile, encoded, 0o600); err != nil {
		return isolation.Result{}, err
	}
	name := "slopwatch-" + backend.config.InstallationID + "-" + runID
	created, err := backend.config.Client.Run(ctx, Command{Arguments: backend.createArguments(name, runID, hostRoot, mask, requestFile, request.Limits, false), Limits: backend.controlLimits()}, nil)
	if err != nil || !created.Successful() || created.StdoutTruncated {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		if cleanupErr != nil {
			backend.setUnavailable(cleanupErr)
		}
		return isolation.Result{}, errors.Join(err, cleanupErr, errors.New("create crash probe container failed"))
	}
	id := strings.TrimSpace(string(created.Stdout))
	if !containerIDPattern.MatchString(id) {
		cleanupErr := backend.cleanupRunLabel(context.Background(), runID)
		if cleanupErr != nil {
			backend.setUnavailable(cleanupErr)
		}
		return isolation.Result{}, errors.Join(cleanupErr, errors.New("crash probe returned invalid container ID"))
	}
	run, runErr := backend.config.Client.Run(ctx, Command{Arguments: []string{"start", "-a", "-i", id}, Limits: request.Limits, Attach: true}, nil)
	cleanupErr := backend.cleanup(context.Background(), id, ctx.Err() != nil || run.Canceled || run.TimedOut)
	return run, errors.Join(runErr, cleanupErr)
}

func (backend *Backend) controlLimits() isolation.Limits {
	return isolation.Limits{WallTime: backend.config.ControlTimeout, TerminateGrace: controlTerminateGrace, MaxStdoutBytes: dockerControlOutputBytes, MaxStderrBytes: dockerControlOutputBytes}
}

func (backend *Backend) probeLimits(wallTime time.Duration) isolation.Limits {
	return isolation.Limits{WallTime: wallTime, TerminateGrace: probeTerminateGrace, MaxStdoutBytes: dockerProbeOutputBytes, MaxStderrBytes: dockerProbeOutputBytes}
}

func formatCPUMillis(value int64) string {
	whole, fraction := value/1000, value%1000
	if fraction == 0 {
		return strconv.FormatInt(whole, 10)
	}
	return strconv.FormatInt(whole, 10) + "." + strings.TrimRight(fmt.Sprintf("%03d", fraction), "0")
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

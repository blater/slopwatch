package docker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

const (
	ContainerSupervisorArgument      = "__slopwatch_container_supervisor_v1"
	ContainerEscapeProbeArgument     = "__slopwatch_container_escape_probe_v1"
	ContainerExecutableProbeArgument = "__slopwatch_container_executable_probe_v1"
	containerEscapeChildArgument     = "__slopwatch_container_escape_child_v1"
	// maximumContainerRequestBytes is a fixed protocol-envelope security
	// invariant, not a workspace/candidate volume policy or user preference.
	maximumContainerRequestBytes = 16 << 20
	candidateSourcePath          = "/candidate"
)

type containerRequest struct {
	Version int               `json:"version"`
	Request isolation.Request `json:"request"`
}

// ContainerSupervisorMain is the image's trusted PID 1. Docker attach keeps
// stdin open as its parent-death control channel; the untrusted target receives
// only the bounded stdin bytes from the structured request.
func ContainerSupervisorMain(arguments []string) (bool, int) {
	if len(arguments) >= 2 && arguments[0] == ContainerExecutableProbeArgument {
		if os.Getpid() != 1 {
			return true, 125
		}
		for _, path := range arguments[1:] {
			if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
				return true, 125
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				fmt.Fprintln(os.Stderr, "validation executable is unavailable:", path)
				return true, 126
			}
		}
		return true, 0
	}
	if len(arguments) != 2 || arguments[0] != ContainerSupervisorArgument {
		return false, 0
	}
	if os.Getpid() != 1 {
		fmt.Fprintln(os.Stderr, "container supervisor must run as PID 1")
		return true, 125
	}
	return true, serveContainer(arguments[1])
}

func serveContainer(path string) int {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open container request:", err)
		return 125
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximumContainerRequestBytes+1))
	if err != nil || len(payload) > maximumContainerRequestBytes {
		return 125
	}
	var envelope containerRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || envelope.Version != 1 {
		fmt.Fprintln(os.Stderr, "invalid container request")
		return 125
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return 125
	}
	request := envelope.Request
	if request.Executable == "" || request.Directory == "" || request.Limits.WallTime <= 0 || request.Limits.TerminateGrace <= 0 {
		return 125
	}
	if err := request.WorkspaceLimits.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "invalid validation workspace limits:", err)
		return 125
	}
	if err := copyCandidateWorkspace(candidateSourcePath, workspacePath, request.WorkspaceLimits); err != nil {
		fmt.Fprintln(os.Stderr, "prepare bounded validation workspace:", err)
		return 125
	}
	command := exec.Command(request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin = bytes.NewReader(request.Stdin)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := configureTarget(command); err != nil {
		return 125
	}
	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	controlGone := make(chan struct{}, 1)
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); controlGone <- struct{}{} }()
	timer := time.NewTimer(request.Limits.WallTime)
	defer timer.Stop()
	select {
	case err := <-waited:
		cleanupTargetGroup(command)
		return commandExitCode(err)
	case <-controlGone:
		terminateTarget(command, waited, request.Limits.TerminateGrace)
		return 130
	case <-timer.C:
		terminateTarget(command, waited, request.Limits.TerminateGrace)
		return 124
	}
}

func copyCandidateWorkspace(source, destination string, limits isolation.WorkspaceLimits) error {
	if err := limits.Validate(); err != nil {
		return fmt.Errorf("candidate copy workspace limits: %w", err)
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return fmt.Errorf("open candidate copy source: %w", err)
	}
	defer sourceRoot.Close()
	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open candidate copy destination: %w", err)
	}
	defer destinationRoot.Close()
	var files, directories, pathBytes int64
	var totalBytes int64
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("candidate copy escaped source")
		}
		if relative == "." {
			return nil
		}
		addedPathBytes := int64(len(filepath.ToSlash(relative)))
		if addedPathBytes > limits.MaxPathBytes-pathBytes {
			return fmt.Errorf("candidate paths exceed %d path bytes", limits.MaxPathBytes)
		}
		pathBytes += addedPathBytes
		if entry.Name() == ".git" {
			if filepath.Clean(relative) != ".git" {
				return fmt.Errorf("nested git metadata %q is forbidden", relative)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if directories >= limits.MaxDirectories {
				return fmt.Errorf("candidate directories exceed %d", limits.MaxDirectories)
			}
			directories++
			return destinationRoot.Mkdir(relative, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("candidate copy rejects non-regular file %q", relative)
		}
		if files >= limits.MaxFiles {
			return fmt.Errorf("candidate files exceed %d", limits.MaxFiles)
		}
		files++
		if info.Size() < 0 || info.Size() > limits.MaxFileBytes || info.Size() > limits.MaxTotalBytes-totalBytes {
			return fmt.Errorf("candidate content exceeds copy limit at %q", relative)
		}
		totalBytes += info.Size()
		input, err := sourceRoot.Open(relative)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() & 0o777
		output, err := destinationRoot.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			_ = input.Close()
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, limits.MaxFileBytes))
		var extra [1]byte
		extraBytes, extraErr := input.Read(extra[:])
		closeErr := errors.Join(output.Close(), input.Close())
		if copyErr != nil || extraBytes != 0 || extraErr != nil && !errors.Is(extraErr, io.EOF) || written != info.Size() {
			return errors.Join(copyErr, extraErr, closeErr, errors.New("candidate file changed during bounded copy or exceeded its per-file limit"))
		}
		return closeErr
	})
}

func terminateTarget(command *exec.Cmd, waited <-chan error, grace time.Duration) {
	_ = terminateTargetGroup(command)
	select {
	case <-waited:
		return
	case <-time.After(grace):
	}
	_ = killTargetGroup(command)
	select {
	case <-waited:
	case <-time.After(grace):
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
}

func cleanupTargetGroup(command *exec.Cmd) { _ = killTargetGroup(command) }

// ContainerEscapeProbeMain creates a setsid descendant which writes only if it
// survives PID 1 exit. It is an empirical release gate, not production work.
func ContainerEscapeProbeMain(arguments []string) (bool, int) {
	if len(arguments) == 3 && arguments[0] == ContainerEscapeProbeArgument {
		delay, err := time.ParseDuration(arguments[2])
		if err != nil || delay <= 0 {
			return true, 125
		}
		child := exec.Command(os.Args[0], containerEscapeChildArgument, arguments[1], strconv.FormatInt(int64(delay), 10))
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := configureEscapeChild(child); err != nil {
			return true, 125
		}
		if err := child.Start(); err != nil {
			return true, 125
		}
		fmt.Fprintln(os.Stdout, child.Process.Pid)
		return true, 0
	}
	if len(arguments) == 3 && arguments[0] == containerEscapeChildArgument {
		nanos, err := strconv.ParseInt(arguments[2], 10, 64)
		if err != nil || nanos <= 0 {
			return true, 125
		}
		time.Sleep(time.Duration(nanos))
		if err := os.WriteFile(arguments[1], []byte("escaped"), 0o600); err != nil {
			return true, 2
		}
		return true, 0
	}
	return false, 0
}

package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// parentCancelWait is a fixed supervisor-cleanup allowance after the caller's
// configured termination grace has elapsed. It never limits a live process;
// it only bounds how long the already-cancelled parent waits before killing a
// wedged supervisor.
const parentCancelWait = 2 * time.Second

type supervisorRequest struct {
	Executable          string   `json:"executable"`
	Arguments           []string `json:"arguments"`
	Directory           string   `json:"directory"`
	Environment         []string `json:"environment"`
	Stdin               []byte   `json:"stdin"`
	WallTimeNanos       int64    `json:"wall_time_nanos"`
	TerminateGraceNanos int64    `json:"terminate_grace_nanos"`
}

func (runner Runner) Run(ctx context.Context, request Request) (Result, error) {
	return runner.run(ctx, request, nil)
}

func (runner Runner) RunStreaming(ctx context.Context, request Request, observeStdout func([]byte)) (Result, error) {
	return runner.run(ctx, request, observeStdout)
}

func (runner Runner) run(ctx context.Context, request Request, observeStdout func([]byte)) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	supervisor, err := runner.supervisorPath()
	if err != nil {
		return Result{}, err
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("create supervisor control pipe: %w", err)
	}
	defer controlRead.Close()
	defer controlWrite.Close()
	requestRead, requestWrite, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("create supervisor request pipe: %w", err)
	}
	defer requestRead.Close()
	defer requestWrite.Close()

	arguments := append(append([]string(nil), runner.PrefixArguments...), SupervisorArgument)
	command := exec.Command(supervisor, arguments...)
	command.ExtraFiles = []*os.File{controlRead, requestRead}
	command.Env = minimalSupervisorEnvironment()
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open supervisor stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open supervisor stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("start supervisor: %w", err)
	}
	_ = controlRead.Close()
	_ = requestRead.Close()

	stdoutBuffer := newBoundedBuffer(request.Limits.MaxStdoutBytes)
	stderrBuffer := newBoundedBuffer(request.Limits.MaxStderrBytes)
	var drains sync.WaitGroup
	drains.Add(2)
	go func() {
		defer drains.Done()
		if observeStdout == nil {
			_, _ = io.Copy(stdoutBuffer, stdout)
			return
		}
		_, _ = io.Copy(&observedWriter{destination: stdoutBuffer, remaining: request.Limits.MaxStdoutBytes, observe: observeStdout}, stdout)
	}()
	go func() { defer drains.Done(); _, _ = io.Copy(stderrBuffer, stderr) }()

	encoded := supervisorRequest{
		Executable: request.Executable, Arguments: request.Arguments,
		Directory: request.Directory, Environment: request.Environment,
		Stdin: request.Stdin, WallTimeNanos: int64(request.Limits.WallTime),
		TerminateGraceNanos: int64(request.Limits.TerminateGrace),
	}
	encodeErr := json.NewEncoder(requestWrite).Encode(encoded)
	_ = requestWrite.Close()
	if encodeErr != nil {
		_ = controlWrite.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		drains.Wait()
		return Result{}, fmt.Errorf("send supervisor request: %w", encodeErr)
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var waitErr error
	canceled := false
	select {
	case waitErr = <-waited:
	case <-ctx.Done():
		canceled = true
		_ = controlWrite.Close()
		select {
		case waitErr = <-waited:
		case <-time.After(request.Limits.TerminateGrace + parentCancelWait):
			_ = command.Process.Kill()
			waitErr = <-waited
		}
	}
	_ = controlWrite.Close()
	drains.Wait()
	stdoutBytes, stdoutTruncated := stdoutBuffer.snapshot()
	stderrBytes, stderrTruncated := stderrBuffer.snapshot()
	result := Result{
		ExitCode: exitCode(waitErr), Stdout: stdoutBytes, Stderr: stderrBytes,
		StdoutTruncated: stdoutTruncated, StderrTruncated: stderrTruncated,
		Canceled: canceled || errors.Is(ctx.Err(), context.Canceled),
	}
	if result.ExitCode == supervisorTimeoutExit {
		result.TimedOut = true
	}
	return result, nil
}

type observedWriter struct {
	destination io.Writer
	remaining   int64
	observe     func([]byte)
}

func (writer *observedWriter) Write(data []byte) (int, error) {
	count, err := writer.destination.Write(data)
	if writer.remaining > 0 {
		length := int64(len(data))
		if length > writer.remaining {
			length = writer.remaining
		}
		chunk := append([]byte(nil), data[:int(length)]...)
		writer.remaining -= length
		writer.observe(chunk)
	}
	return count, err
}

func validateRequest(request Request) error {
	if request.Executable == "" || !filepath.IsAbs(request.Executable) || request.Directory == "" || !filepath.IsAbs(request.Directory) {
		return ErrInvalidRequest
	}
	if request.Limits.WallTime < 0 || request.Limits.TerminateGrace <= 0 || request.Limits.MaxStdoutBytes <= 0 || request.Limits.MaxStderrBytes <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

func (runner Runner) supervisorPath() (string, error) {
	path := runner.SupervisorExecutable
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve supervisor executable: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve supervisor executable: %w", err)
	}
	return absolute, nil
}

func minimalSupervisorEnvironment() []string {
	return []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

type CommandClient struct {
	executable string
	dockerHost string
}

func NewCommandClient(executable string, configuredHost ...string) (*CommandClient, error) {
	if executable == "" {
		return nil, errors.New("docker executable is required")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return nil, errors.New("docker executable is not an executable regular file")
	}
	host := os.Getenv("DOCKER_HOST")
	if len(configuredHost) > 0 {
		host = configuredHost[0]
	}
	parsedHost, parseErr := url.Parse(host)
	if parseErr != nil || parsedHost.Scheme != "unix" || parsedHost.User != nil || parsedHost.Host != "" ||
		parsedHost.RawQuery != "" || parsedHost.Fragment != "" || !filepath.IsAbs(parsedHost.Path) ||
		filepath.Clean(parsedHost.Path) != parsedHost.Path || strings.ContainsAny(host, "\x00\r\n") {
		return nil, errors.New("docker confinement requires an explicit local Unix daemon socket")
	}
	return &CommandClient{executable: resolved, dockerHost: host}, nil
}

func (client *CommandClient) Run(ctx context.Context, request Command, observe func([]byte)) (isolation.Result, error) {
	if client == nil || client.executable == "" || len(request.Arguments) == 0 {
		return isolation.Result{}, errors.New("invalid docker command")
	}
	limits := request.Limits
	if limits.WallTime <= 0 || limits.TerminateGrace <= 0 || limits.MaxStdoutBytes <= 0 || limits.MaxStderrBytes <= 0 {
		return isolation.Result{}, errors.New("docker command requires explicit positive wall-time, termination-grace, stdout, and stderr limits")
	}
	command := exec.Command(client.executable, request.Arguments...)
	command.Env = []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "DOCKER_HOST=" + client.dockerHost}
	if err := configureCLIProcess(command); err != nil {
		return isolation.Result{}, err
	}
	var stdin io.WriteCloser
	if request.Attach {
		var err error
		stdin, err = command.StdinPipe()
		if err != nil {
			return isolation.Result{}, err
		}
	} else {
		command.Stdin = bytes.NewReader(request.Stdin)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return isolation.Result{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return isolation.Result{}, err
	}
	if err := command.Start(); err != nil {
		return isolation.Result{}, err
	}
	stdoutBuffer, stderrBuffer := &limitedBuffer{limit: limits.MaxStdoutBytes}, &limitedBuffer{limit: limits.MaxStderrBytes}
	var drains sync.WaitGroup
	drains.Add(2)
	go func() {
		defer drains.Done()
		_, _ = io.Copy(&observingWriter{buffer: stdoutBuffer, observe: observe}, stdout)
	}()
	go func() { defer drains.Done(); _, _ = io.Copy(stderrBuffer, stderr) }()
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	timer := time.NewTimer(limits.WallTime)
	defer timer.Stop()
	var waitErr error
	canceled, timedOut := false, false
	select {
	case waitErr = <-waited:
	case <-ctx.Done():
		canceled = true
		if stdin != nil {
			_ = stdin.Close()
		}
		waitErr = waitOrKill(command, waited, limits.TerminateGrace)
	case <-timer.C:
		timedOut = true
		if stdin != nil {
			_ = stdin.Close()
		}
		waitErr = waitOrKill(command, waited, limits.TerminateGrace)
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	drains.Wait()
	stdoutBytes, stdoutTruncated := stdoutBuffer.snapshot()
	stderrBytes, stderrTruncated := stderrBuffer.snapshot()
	return isolation.Result{ExitCode: commandExitCode(waitErr), Stdout: stdoutBytes, Stderr: stderrBytes,
		StdoutTruncated: stdoutTruncated, StderrTruncated: stderrTruncated, Canceled: canceled, TimedOut: timedOut}, nil
}

func waitOrKill(command *exec.Cmd, waited <-chan error, grace time.Duration) error {
	select {
	case err := <-waited:
		return err
	case <-time.After(grace):
	}
	_ = terminateCLIProcess(command)
	select {
	case err := <-waited:
		return err
	case <-time.After(grace):
	}
	_ = killCLIProcess(command)
	return <-waited
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int64
	truncated bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	original := len(data)
	remaining := buffer.limit - int64(len(buffer.data))
	if remaining <= 0 {
		buffer.truncated = buffer.truncated || original > 0
		return original, nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		buffer.truncated = true
	}
	buffer.data = append(buffer.data, data...)
	return original, nil
}
func (buffer *limitedBuffer) snapshot() ([]byte, bool) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte(nil), buffer.data...), buffer.truncated
}

type observingWriter struct {
	buffer  *limitedBuffer
	observe func([]byte)
}

func (writer *observingWriter) Write(data []byte) (int, error) {
	before, _ := writer.buffer.snapshot()
	count, err := writer.buffer.Write(data)
	after, _ := writer.buffer.snapshot()
	if writer.observe != nil && len(after) > len(before) {
		writer.observe(append([]byte(nil), after[len(before):]...))
	}
	return count, err
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

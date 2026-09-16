package candidate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

type gitExecutor struct {
	git    string
	runner isolation.Executor
}

func (executor gitExecutor) text(ctx context.Context, directory string, arguments ...string) (string, error) {
	data, err := executor.bytes(ctx, directory, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (executor gitExecutor) bytes(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	maximum := commandOutputBytes(ctx)
	if maximum <= 0 {
		return nil, errors.New("candidate Git command output budget is not configured")
	}
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	result, err := executor.runner.Run(ctx, isolation.Request{Executable: executor.git, Arguments: trusted, Directory: directory,
		Environment: []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"},
		Stdin:       nil, Limits: isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
	if err != nil {
		return nil, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("Git output exceeded safety limit")
	}
	if !result.Successful() {
		return nil, fmt.Errorf("git %s failed with exit %d: %s", arguments[0], result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}

type commandOutputContextKey struct{}

func withCommandOutputBytes(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

func commandOutputBytes(ctx context.Context) int64 {
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	return maximum
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

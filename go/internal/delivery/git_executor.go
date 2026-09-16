package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

type gitExecutor struct {
	git    string
	runner isolation.Executor
}

type commandOutputContextKey struct{}

func withCommandOutput(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

var errGitExitOne = errors.New("git exited with status one")

func (executor gitExecutor) text(ctx context.Context, root string, arguments ...string) (string, error) {
	data, err := executor.bytes(ctx, root, false, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (executor gitExecutor) textEnv(ctx context.Context, root string, environment []string, arguments ...string) (string, error) {
	data, err := executor.bytesEnv(ctx, root, environment, false, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (executor gitExecutor) bytes(ctx context.Context, root string, exitOne bool, arguments ...string) ([]byte, error) {
	return executor.bytesEnv(ctx, root, nil, exitOne, arguments...)
}

func (executor gitExecutor) bytesEnv(ctx context.Context, root string, extraEnvironment []string, exitOne bool, arguments ...string) ([]byte, error) {
	return executor.bytesInputEnv(ctx, root, extraEnvironment, nil, exitOne, arguments...)
}

func (executor gitExecutor) bytesInputEnv(ctx context.Context, root string, extraEnvironment []string, input []byte, exitOne bool, arguments ...string) ([]byte, error) {
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	if maximum <= 0 {
		return nil, errors.New("Git command output budget is not configured")
	}
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true",
		// An admitted repository may name a remote, but it may not add a
		// process-launch path to publication through local credential config.
		// An empty helper value resets all helpers read earlier by Git.
		"-c", "credential.helper=", "-c", "credential.interactive=never", "-c", "core.askPass=",
		"-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always", "-c", "protocol.ext.allow=never", "-c", "core.sshCommand=ssh", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	environment := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Slopwatch", "GIT_AUTHOR_EMAIL=slopwatch@localhost", "GIT_COMMITTER_NAME=Slopwatch", "GIT_COMMITTER_EMAIL=slopwatch@localhost"}
	environment = append(environment, extraEnvironment...)
	result, err := executor.runner.Run(ctx, isolation.Request{Executable: executor.git, Arguments: trusted, Directory: root,
		Environment: environment,
		Stdin:       input, Limits: isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
	if err != nil {
		return nil, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("Git output exceeded safety limit")
	}
	if result.ExitCode == 1 && exitOne {
		return result.Stdout, errGitExitOne
	}
	if !result.Successful() {
		return nil, fmt.Errorf("git %s exited with status %d: %s", arguments[0], result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}

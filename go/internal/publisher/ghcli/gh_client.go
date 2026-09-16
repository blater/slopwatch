package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

type ghClient struct {
	executable string
	directory  string
	runner     isolation.Executor
	getenv     func(string) string
}

func newGHClient(config Config, runner isolation.Executor) (ghClient, error) {
	if runner == nil {
		return ghClient{}, errors.New("GitHub publisher requires a process runner")
	}
	gh := filepath.Clean(config.Executable)
	if !filepath.IsAbs(gh) || gh != config.Executable {
		return ghClient{}, errors.New("GitHub publisher executable must be an absolute canonical path")
	}
	resolved, err := filepath.EvalSymlinks(gh)
	if err != nil || resolved != gh {
		return ghClient{}, errors.New("GitHub publisher executable must resolve to its canonical path")
	}
	info, err := os.Stat(gh)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return ghClient{}, errors.New("GitHub publisher executable must be a non-writable regular file")
	}
	directory := filepath.Clean(config.WorkingDirectory)
	if !filepath.IsAbs(directory) || directory != config.WorkingDirectory {
		return ghClient{}, errors.New("GitHub publisher working directory must be an absolute canonical path")
	}
	directoryResolved, err := filepath.EvalSymlinks(directory)
	if err != nil || directoryResolved != directory {
		return ghClient{}, errors.New("GitHub publisher working directory must resolve to its canonical path")
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm()&0o077 != 0 {
		return ghClient{}, errors.New("GitHub publisher working directory must be a private directory")
	}
	return ghClient{executable: gh, directory: directory, runner: runner, getenv: os.Getenv}, nil
}

func (client *ghClient) run(ctx context.Context, arguments ...string) (isolation.Result, error) {
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	if maximum <= 0 {
		return isolation.Result{}, errors.New("GitHub publisher command output budget is not configured")
	}
	result, err := client.runner.Run(ctx, isolation.Request{Executable: client.executable, Arguments: arguments, Directory: client.directory,
		Environment: client.environment(), Limits: isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
	if err != nil {
		return result, errors.New(client.redact(err.Error()))
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("GitHub CLI output exceeded safety limit")
	}
	if client.containsKnownSecret(result.Stdout) || client.containsKnownSecret(result.Stderr) {
		return isolation.Result{}, errors.New("GitHub CLI returned unsafe authentication material [REDACTED]")
	}
	if !result.Successful() {
		return result, fmt.Errorf("GitHub CLI exited with status %d: %s", result.ExitCode, client.redact(strings.TrimSpace(string(result.Stderr))))
	}
	return result, nil
}

func (client *ghClient) containsKnownSecret(payload []byte) bool {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		secret := client.getenv(key)
		if secret == "" {
			continue
		}
		if strings.Contains(string(payload), secret) {
			return true
		}
		var decoded any
		if json.Unmarshal(payload, &decoded) == nil && jsonContains(decoded, secret) {
			return true
		}
	}
	return false
}

func jsonContains(value any, secret string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, secret)
	case []any:
		for _, item := range typed {
			if jsonContains(item, secret) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if strings.Contains(key, secret) || jsonContains(item, secret) {
				return true
			}
		}
	}
	return false
}

func (client *ghClient) redact(value string) string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if secret := client.getenv(key); secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func (client *ghClient) environment() []string {
	result := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0"}
	for _, key := range []string{"HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := client.getenv(key); value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

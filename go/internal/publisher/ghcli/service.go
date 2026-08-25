// Package ghcli adapts GitHub CLI draft-PR creation behind the publisher port.
package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
	"github.com/blater/slopwatch/internal/publisher"
)

type Service struct {
	gh     string
	runner isolation.Executor
	getenv func(string) string
}

func New(runner isolation.Executor) (*Service, error) {
	if runner == nil {
		return nil, errors.New("GitHub publisher requires a process runner")
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil, err
	}
	gh, err = filepath.Abs(gh)
	if err != nil {
		return nil, err
	}
	return &Service{gh: gh, runner: runner, getenv: os.Getenv}, nil
}

func (service *Service) Create(ctx context.Context, request publisher.Request) (publisher.Result, error) {
	if request.Candidate.RepositoryRoot == "" || request.HostRepository == "" || request.BaseBranch == "" || request.HeadBranch == "" || request.Commit == "" {
		return publisher.Result{}, errors.New("GitHub pull request request is incomplete")
	}
	if existing, found, err := service.lookup(ctx, request); err != nil {
		return publisher.Result{}, err
	} else if found {
		return existing, nil
	}
	arguments := []string{"pr", "create", "--repo", request.HostRepository, "--base", request.BaseBranch, "--head", request.HeadBranch,
		"--title", request.Title, "--body", request.Body}
	if request.Draft {
		arguments = append(arguments, "--draft")
	}
	run, err := service.run(ctx, request.Candidate.RepositoryRoot, arguments...)
	if err != nil {
		return publisher.Result{}, err
	}
	url := lastNonemptyLine(string(run.Stdout))
	if url == "" {
		return publisher.Result{Ambiguous: true, Diagnostic: "GitHub CLI returned no pull request URL"}, errors.New("GitHub pull request creation response was ambiguous")
	}
	result := publisher.Result{ProviderID: url, URL: url, Draft: request.Draft}
	verified, found, reconcileErr := service.lookup(ctx, request)
	if reconcileErr != nil || !found {
		result.Ambiguous = true
		result.Diagnostic = "created pull request could not be reconciled"
		return result, errors.Join(errors.New(result.Diagnostic), reconcileErr)
	}
	return verified, nil
}

func (service *Service) Reconcile(ctx context.Context, request publisher.Request, previous publisher.Result) (publisher.Result, error) {
	result, found, err := service.lookup(ctx, request)
	if err != nil {
		previous.Ambiguous = true
		previous.Diagnostic = err.Error()
		return previous, err
	}
	if !found {
		previous.Ambiguous = false
		previous.Diagnostic = "pull request is absent"
		return previous, nil
	}
	return result, nil
}

type prRecord struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	Draft      bool   `json:"isDraft"`
	HeadRefOID string `json:"headRefOid"`
}

func (service *Service) lookup(ctx context.Context, request publisher.Request) (publisher.Result, bool, error) {
	run, err := service.run(ctx, request.Candidate.RepositoryRoot, "pr", "list", "--repo", request.HostRepository, "--state", "all", "--head", request.HeadBranch,
		"--json", "number,url,isDraft,headRefOid", "--limit", "20")
	if err != nil {
		return publisher.Result{}, false, err
	}
	var records []prRecord
	if err := json.Unmarshal(run.Stdout, &records); err != nil {
		return publisher.Result{}, false, fmt.Errorf("decode GitHub pull request lookup: %w", err)
	}
	for _, record := range records {
		if record.HeadRefOID == string(request.Commit) {
			return publisher.Result{ProviderID: strconv.Itoa(record.Number), URL: record.URL, Draft: record.Draft}, true, nil
		}
	}
	if len(records) > 0 {
		return publisher.Result{Ambiguous: true, Diagnostic: "branch already has a pull request for another commit"}, false,
			errors.New("GitHub branch pull request does not match the delivered commit")
	}
	return publisher.Result{}, false, nil
}

func (service *Service) run(ctx context.Context, directory string, arguments ...string) (isolation.Result, error) {
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.gh, Arguments: arguments, Directory: directory,
		Environment: service.environment(), Limits: isolation.Limits{WallTime: 2 * time.Minute, TerminateGrace: 2 * time.Second, MaxStdoutBytes: 2 << 20, MaxStderrBytes: 2 << 20}})
	if err != nil {
		return result, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("GitHub CLI output exceeded safety limit")
	}
	if !result.Successful() {
		return result, fmt.Errorf("GitHub CLI exited with status %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result, nil
}

func (service *Service) environment() []string {
	path := service.getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	result := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=" + path, "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0"}
	for _, key := range []string{"HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST"} {
		if value := service.getenv(key); value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func lastNonemptyLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if value := strings.TrimSpace(lines[index]); value != "" {
			return value
		}
	}
	return ""
}

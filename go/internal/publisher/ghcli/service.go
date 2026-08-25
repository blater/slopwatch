// Package ghcli adapts GitHub CLI pull-request creation behind the publisher port.
package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
	"github.com/blater/slopwatch/internal/publisher"
)

type Service struct {
	gh               string
	workingDirectory string
	runner           isolation.Executor
	getenv           func(string) string
}

type commandOutputContextKey struct{}

func withCommandOutput(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

const ProviderID = "github-cli"

type Config struct {
	Executable       string
	WorkingDirectory string
}

func New(config Config, runner isolation.Executor) (*Service, error) {
	if runner == nil {
		return nil, errors.New("GitHub publisher requires a process runner")
	}
	gh := filepath.Clean(config.Executable)
	if !filepath.IsAbs(gh) || gh != config.Executable {
		return nil, errors.New("GitHub publisher executable must be an absolute canonical path")
	}
	resolved, err := filepath.EvalSymlinks(gh)
	if err != nil || resolved != gh {
		return nil, errors.New("GitHub publisher executable must resolve to its canonical path")
	}
	info, err := os.Stat(gh)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("GitHub publisher executable must be a non-writable regular file")
	}
	directory := filepath.Clean(config.WorkingDirectory)
	if !filepath.IsAbs(directory) || directory != config.WorkingDirectory {
		return nil, errors.New("GitHub publisher working directory must be an absolute canonical path")
	}
	directoryResolved, err := filepath.EvalSymlinks(directory)
	if err != nil || directoryResolved != directory {
		return nil, errors.New("GitHub publisher working directory must resolve to its canonical path")
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("GitHub publisher working directory must be a private directory")
	}
	return &Service{gh: gh, workingDirectory: directory, runner: runner, getenv: os.Getenv}, nil
}

func (service *Service) Preflight(ctx context.Context, request publisher.PreflightRequest) (publisher.Readiness, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if request.Provider != ProviderID {
		return publisher.Readiness{}, fmt.Errorf("GitHub publisher does not support provider %q", request.Provider)
	}
	if !strings.EqualFold(request.RemoteHost, "github.com") || request.HostRepository == "" {
		return publisher.Readiness{}, errors.New("GitHub publisher requires a verified github.com owner/repository remote")
	}
	if service.containsKnownSecret([]byte(request.HostRepository)) {
		return publisher.Readiness{}, errors.New("GitHub publisher request contains unsafe authentication material [REDACTED]")
	}
	if _, err := service.run(ctx, "auth", "status", "--hostname", "github.com"); err != nil {
		return publisher.Readiness{}, fmt.Errorf("GitHub publisher authentication is not ready: %w", err)
	}
	result, err := service.run(ctx, "repo", "view", request.HostRepository, "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return publisher.Readiness{}, fmt.Errorf("GitHub repository is not ready for publication: %w", err)
	}
	if strings.TrimSpace(string(result.Stdout)) != request.HostRepository {
		return publisher.Readiness{}, errors.New("GitHub publisher resolved a different repository")
	}
	return publisher.Readiness{Provider: ProviderID, HostRepository: request.HostRepository}, nil
}

func (service *Service) Create(ctx context.Context, request publisher.Request) (publisher.Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if request.HostRepository == "" || request.BaseBranch == "" || request.HeadBranch == "" || request.Commit == "" {
		return publisher.Result{}, errors.New("GitHub pull request request is incomplete")
	}
	for _, value := range []string{request.HostRepository, request.BaseBranch, request.HeadBranch, request.Title, request.Body} {
		if service.containsKnownSecret([]byte(value)) {
			return publisher.Result{}, errors.New("GitHub pull request request contains unsafe authentication material [REDACTED]")
		}
	}
	if existing, found, err := service.lookup(ctx, request, 0); err != nil {
		return publisher.Result{}, err
	} else if found {
		return existing, nil
	}
	arguments := []string{"pr", "create", "--repo", request.HostRepository, "--base", request.BaseBranch, "--head", request.HeadBranch,
		"--title", request.Title, "--body", request.Body}
	if request.Draft {
		arguments = append(arguments, "--draft")
	}
	run, err := service.run(ctx, arguments...)
	if err != nil {
		result := publisher.Result{Ambiguous: true, Diagnostic: "GitHub pull request creation may have taken effect"}
		if value := lastNonemptyLine(string(run.Stdout)); value != "" {
			if number, parseErr := pullRequestNumber(value, request.HostRepository); parseErr == nil {
				result.ProviderID, result.URL = strconv.Itoa(number), value
			}
		}
		return result, err
	}
	url := lastNonemptyLine(string(run.Stdout))
	if url == "" {
		return publisher.Result{Ambiguous: true, Diagnostic: "GitHub CLI returned no pull request URL"}, errors.New("GitHub pull request creation response was ambiguous")
	}
	number, parseErr := pullRequestNumber(url, request.HostRepository)
	if parseErr != nil {
		return publisher.Result{Ambiguous: true, Diagnostic: "GitHub CLI returned an invalid pull request URL"}, parseErr
	}
	result := publisher.Result{ProviderID: strconv.Itoa(number), URL: url, Draft: request.Draft}
	verified, found, reconcileErr := service.lookup(ctx, request, number)
	if reconcileErr != nil || !found {
		result.Ambiguous = true
		result.Diagnostic = "created pull request could not be reconciled"
		return result, errors.Join(errors.New(result.Diagnostic), reconcileErr)
	}
	return verified, nil
}

func (service *Service) Reconcile(ctx context.Context, request publisher.Request, previous publisher.Result) (publisher.Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	expectedNumber, identityErr := previousPullRequestNumber(previous, request.HostRepository)
	if identityErr != nil {
		previous.Ambiguous = true
		previous.Diagnostic = "journaled pull request identity is invalid"
		return previous, identityErr
	}
	result, found, err := service.lookup(ctx, request, expectedNumber)
	if err != nil {
		previous.Ambiguous = true
		previous.Diagnostic = err.Error()
		return previous, err
	}
	if !found {
		if expectedNumber > 0 {
			previous.Ambiguous = true
			previous.Diagnostic = "journaled pull request is not visible for exact reconciliation"
			return previous, errors.New(previous.Diagnostic)
		}
		return publisher.Result{Diagnostic: "pull request is absent"}, nil
	}
	return result, nil
}

type prRecord struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	Draft             bool   `json:"isDraft"`
	BaseRefName       string `json:"baseRefName"`
	HeadRefName       string `json:"headRefName"`
	HeadRefOID        string `json:"headRefOid"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	State             string `json:"state"`
}

func (service *Service) lookup(ctx context.Context, request publisher.Request, expectedNumber int) (publisher.Result, bool, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	records, err := service.listPullRequests(ctx, request)
	if err != nil {
		return publisher.Result{}, false, err
	}
	var matches []publisher.Result
	for _, record := range records {
		if expectedNumber > 0 && record.Number == expectedNumber &&
			!recordMatchesRequest(record, request) {
			return publisher.Result{Ambiguous: true, Diagnostic: "journaled pull request no longer matches the delivered state"}, false,
				errors.New("journaled GitHub pull request identity does not match the delivered commit and review state")
		}
		if recordMatchesRequest(record, request) &&
			(expectedNumber == 0 || record.Number == expectedNumber) {
			if record.Number <= 0 {
				return publisher.Result{}, false, errors.New("GitHub pull request lookup returned an invalid number")
			}
			if err := validatePullRequestURL(record.URL, request.HostRepository, record.Number); err != nil {
				return publisher.Result{}, false, err
			}
			matches = append(matches, publisher.Result{ProviderID: strconv.Itoa(record.Number), URL: record.URL, Draft: record.Draft})
		}
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(matches) > 1 {
		return publisher.Result{Ambiguous: true, Diagnostic: "multiple pull requests match the delivered state"}, false,
			errors.New("GitHub pull request identity is not unique")
	}
	if len(records) > 0 {
		return publisher.Result{Ambiguous: true, Diagnostic: "branch already has a pull request for another commit"}, false,
			errors.New("GitHub branch pull request does not match the delivered commit")
	}
	return publisher.Result{}, false, nil
}

func recordMatchesRequest(record prRecord, request publisher.Request) bool {
	return record.HeadRefOID == string(request.Commit) && record.Draft == request.Draft &&
		record.BaseRefName == request.BaseBranch && record.HeadRefName == request.HeadBranch &&
		!record.IsCrossRepository && strings.EqualFold(record.State, "OPEN")
}

// listPullRequests expands gh's result window until the response proves it is
// complete. There is deliberately no compiled PR-count ceiling: the visible
// command-output byte budget is the only operational bound, and an exhausted
// budget produces an actionable error rather than a false "absent" result.
func (service *Service) listPullRequests(ctx context.Context, request publisher.Request) ([]prRecord, error) {
	limit := 100
	for {
		run, err := service.run(ctx, "pr", "list", "--repo", request.HostRepository, "--state", "all", "--head", request.HeadBranch,
			"--json", "number,url,isDraft,baseRefName,headRefName,headRefOid,isCrossRepository,state", "--limit", strconv.Itoa(limit))
		if err != nil {
			return nil, err
		}
		var records []prRecord
		if err := json.Unmarshal(run.Stdout, &records); err != nil {
			return nil, fmt.Errorf("decode GitHub pull request lookup: %w", err)
		}
		if len(records) < limit {
			return records, nil
		}
		if limit > int(^uint(0)>>1)/2 {
			return nil, errors.New("GitHub pull request result window exceeds platform addressability")
		}
		limit *= 2
	}
}

func previousPullRequestNumber(previous publisher.Result, hostRepository string) (int, error) {
	var expected int
	if previous.ProviderID != "" {
		number, err := strconv.Atoi(previous.ProviderID)
		if err != nil || number <= 0 {
			return 0, errors.New("journaled GitHub pull request number is invalid")
		}
		expected = number
	}
	if previous.URL != "" {
		number, err := pullRequestNumber(previous.URL, hostRepository)
		if err != nil || expected > 0 && number != expected {
			return 0, errors.New("journaled GitHub pull request URL and number disagree")
		}
		expected = number
	}
	return expected, nil
}

func (service *Service) run(ctx context.Context, arguments ...string) (isolation.Result, error) {
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	if maximum <= 0 {
		return isolation.Result{}, errors.New("GitHub publisher command output budget is not configured")
	}
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.gh, Arguments: arguments, Directory: service.workingDirectory,
		Environment: service.environment(), Limits: isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
	if err != nil {
		return result, errors.New(service.redact(err.Error()))
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("GitHub CLI output exceeded safety limit")
	}
	if service.containsKnownSecret(result.Stdout) || service.containsKnownSecret(result.Stderr) {
		return isolation.Result{}, errors.New("GitHub CLI returned unsafe authentication material [REDACTED]")
	}
	if !result.Successful() {
		return result, fmt.Errorf("GitHub CLI exited with status %d: %s", result.ExitCode, service.redact(strings.TrimSpace(string(result.Stderr))))
	}
	return result, nil
}

func (service *Service) containsKnownSecret(payload []byte) bool {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		secret := service.getenv(key)
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

func validatePullRequestURL(value, hostRepository string, expectedNumber int) error {
	number, err := pullRequestNumber(value, hostRepository)
	if err != nil || expectedNumber > 0 && number != expectedNumber {
		return errors.New("GitHub CLI returned an invalid pull request URL")
	}
	return nil
}

func pullRequestNumber(value, hostRepository string) (int, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.Port() != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(value, "\x00\r\n\t ") {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" || strings.Contains(parts[0]+parts[1], "%") {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	number, err := strconv.Atoi(parts[3])
	requested := strings.Split(hostRepository, "/")
	if err != nil || number <= 0 || len(requested) != 2 || !strings.EqualFold(parts[0], requested[0]) ||
		!strings.EqualFold(parts[1], requested[1]) {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	return number, nil
}

func (service *Service) redact(value string) string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if secret := service.getenv(key); secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func (service *Service) environment() []string {
	result := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0"}
	for _, key := range []string{"HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN"} {
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

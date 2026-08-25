package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
	"github.com/blater/slopwatch/internal/publisher"
)

type runnerFunc func(context.Context, isolation.Request) (isolation.Result, error)

const testCommandOutputBytes = int64(4 << 20)

func (f runnerFunc) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	return f(ctx, request)
}

func TestRunRejectsSuccessfulRawAndJSONEscapedTokenEcho(t *testing.T) {
	const token = "github_pat_test"
	lookup := func(key string) string {
		if key == "GH_TOKEN" {
			return token
		}
		return ""
	}
	for _, output := range []string{
		"https://github.com/owner/repo/pull/1\n" + token,
		`[{"number":1,"url":"https://github.com/owner/repo/pull/1","headRefOid":"github_pa\u0074_test"}]`,
	} {
		service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: lookup, runner: runnerFunc(func(context.Context, isolation.Request) (isolation.Result, error) {
			return isolation.Result{ExitCode: 0, Stdout: []byte(output)}, nil
		})}
		result, err := service.run(withCommandOutput(t.Context(), testCommandOutputBytes), "pr", "list")
		encoded := fmt.Sprintf("%+v %v", result, err)
		if err == nil || strings.Contains(encoded, token) || !strings.Contains(encoded, "[REDACTED]") {
			t.Fatalf("successful secret echo was not safely rejected: %s", encoded)
		}
	}
}

func TestCreateRejectsKnownTokenBeforeGitHubCLIExecution(t *testing.T) {
	const token = "github_pat_request_secret"
	called := false
	service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(key string) string {
		if key == "GH_TOKEN" {
			return token
		}
		return ""
	}, runner: runnerFunc(func(context.Context, isolation.Request) (isolation.Result, error) {
		called = true
		return isolation.Result{}, nil
	})}
	request := publisher.Request{HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", Title: "do not publish " + token, CommandOutputBytes: testCommandOutputBytes}
	result, err := service.Create(t.Context(), request)
	if err == nil || called || result != (publisher.Result{}) || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("secret-bearing PR request was not rejected locally: result=%+v called=%v err=%v", result, called, err)
	}
}

func TestCreateAndLookupRejectHostilePullRequestURLs(t *testing.T) {
	request := publisher.Request{Job: "job", Candidate: fix.CandidateIdentity{RepositoryRoot: "/candidate"}, HostRepository: "owner/repo",
		BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", Draft: true, CommandOutputBytes: testCommandOutputBytes}
	for _, hostile := range []string{
		"http://github.com/owner/repo/pull/1",
		"https://evil.example/owner/repo/pull/1",
		"https://token@github.com/owner/repo/pull/1",
		"https://github.com/owner/repo/issues/1",
		"https://github.com/owner/repo/pull/1\x1b[2J",
	} {
		t.Run(hostile, func(t *testing.T) {
			runner := &sequenceRunner{results: []isolation.Result{
				{ExitCode: 0, Stdout: []byte("[]")},
				{ExitCode: 0, Stdout: []byte(hostile + "\n")},
			}}
			service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: runner}
			result, err := service.Create(t.Context(), request)
			if err == nil || !result.Ambiguous || result.URL != "" || result.ProviderID != "" {
				t.Fatalf("hostile create URL accepted: result=%+v err=%v", result, err)
			}

			lookupRunner := &sequenceRunner{results: []isolation.Result{{ExitCode: 0, Stdout: []byte(fmt.Sprintf(
				`[{"number":1,"url":%q,"isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`, hostile))}}}
			service.runner = lookupRunner
			if result, found, err := service.lookup(t.Context(), request, 0); err == nil || found || result.URL != "" {
				t.Fatalf("hostile lookup URL accepted: result=%+v found=%v err=%v", result, found, err)
			}
		})
	}
}

func TestValidatePullRequestURLAcceptsCanonicalGitHubURL(t *testing.T) {
	if err := validatePullRequestURL("https://github.com/owner/repo/pull/17", "owner/repo", 17); err != nil {
		t.Fatalf("canonical GitHub pull request URL rejected: %v", err)
	}
	for _, test := range []struct {
		url, repository string
		number          int
	}{
		{url: "https://github.com/other/repo/pull/17", repository: "owner/repo", number: 17},
		{url: "https://github.com/owner/repo/pull/18", repository: "owner/repo", number: 17},
	} {
		if err := validatePullRequestURL(test.url, test.repository, test.number); err == nil {
			t.Fatalf("hostile URL binding accepted: %+v", test)
		}
	}
}

func TestReconcileRejectsUnverifiedJournaledIdentity(t *testing.T) {
	service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: &sequenceRunner{results: []isolation.Result{{ExitCode: 0, Stdout: []byte("[]")}}}}
	request := publisher.Request{Candidate: fix.CandidateIdentity{RepositoryRoot: "/candidate"}, HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", CommandOutputBytes: testCommandOutputBytes}
	previous := publisher.Result{ProviderID: "unverified", URL: "https://github.com/owner/repo/pull/1", Ambiguous: true}
	result, err := service.Reconcile(t.Context(), request, previous)
	if err == nil || !result.Ambiguous || result.ProviderID != "unverified" || result.URL == "" {
		t.Fatalf("invalid journaled identity was not retained for intervention: result=%+v err=%v", result, err)
	}
}

func TestCreatePersistsExactNumberAndRejectsCrossNumberReconciliation(t *testing.T) {
	request := publisher.Request{HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", Draft: true, CommandOutputBytes: testCommandOutputBytes}
	for _, test := range []struct {
		name       string
		lookup     string
		wantError  bool
		wantNumber string
	}{
		{name: "exact", lookup: `[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`, wantNumber: "17"},
		{name: "cross number", lookup: `[{"number":18,"url":"https://github.com/owner/repo/pull/18","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`, wantError: true, wantNumber: "17"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: &sequenceRunner{results: []isolation.Result{
				{ExitCode: 0, Stdout: []byte("[]")},
				{ExitCode: 0, Stdout: []byte("https://github.com/owner/repo/pull/17\n")},
				{ExitCode: 0, Stdout: []byte(test.lookup)},
			}}}
			result, err := service.Create(t.Context(), request)
			if (err != nil) != test.wantError || result.ProviderID != test.wantNumber || test.wantError && !result.Ambiguous {
				t.Fatalf("Create()=%+v, %v", result, err)
			}
		})
	}
}

func TestCreateFailureIsAmbiguousAndReconcileBindsJournaledNumber(t *testing.T) {
	request := publisher.Request{HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", Draft: true, CommandOutputBytes: testCommandOutputBytes}
	calls := 0
	service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: runnerFunc(func(_ context.Context, launched isolation.Request) (isolation.Result, error) {
		calls++
		if calls == 1 {
			return isolation.Result{ExitCode: 0, Stdout: []byte("[]")}, nil
		}
		return isolation.Result{ExitCode: 130, Canceled: true}, context.Canceled
	})}
	result, err := service.Create(t.Context(), request)
	if err == nil || !result.Ambiguous || result.ProviderID != "" {
		t.Fatalf("canceled create=%+v, %v", result, err)
	}

	service.runner = &sequenceRunner{results: []isolation.Result{{ExitCode: 0, Stdout: []byte(
		`[{"number":18,"url":"https://github.com/owner/repo/pull/18","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`)}}}
	previous := publisher.Result{ProviderID: "17", URL: "https://github.com/owner/repo/pull/17", Draft: true, Ambiguous: true}
	reconciled, reconcileErr := service.Reconcile(t.Context(), request, previous)
	if reconcileErr == nil || !reconciled.Ambiguous || reconciled.ProviderID != "17" {
		t.Fatalf("cross-number reconcile=%+v, %v", reconciled, reconcileErr)
	}
}

func TestLookupRequiresExactUniqueOpenDraftSameRepositoryMatch(t *testing.T) {
	request := publisher.Request{HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "abc", Draft: true, CommandOutputBytes: testCommandOutputBytes}
	for _, records := range []string{
		`[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":false,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`,
		`[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"CLOSED"}]`,
		`[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":true,"baseRefName":"release","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`,
		`[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":true,"state":"OPEN"}]`,
		`[{"number":17,"url":"https://github.com/owner/repo/pull/17","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"},{"number":18,"url":"https://github.com/owner/repo/pull/18","isDraft":true,"baseRefName":"main","headRefName":"fix/test","headRefOid":"abc","isCrossRepository":false,"state":"OPEN"}]`,
	} {
		service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: &sequenceRunner{results: []isolation.Result{{ExitCode: 0, Stdout: []byte(records)}}}}
		if result, found, err := service.lookup(t.Context(), request, 0); err == nil || found || !result.Ambiguous {
			t.Fatalf("unsafe lookup accepted: result=%+v found=%v err=%v", result, found, err)
		}
	}
}

func TestLookupExpandsResultWindowWithoutCompiledPRCountCap(t *testing.T) {
	request := publisher.Request{HostRepository: "owner/repo", BaseBranch: "main", HeadBranch: "fix/test", Commit: "wanted", Draft: true, CommandOutputBytes: testCommandOutputBytes}
	records := make([]prRecord, 101)
	for index := range records {
		records[index] = prRecord{Number: index + 1, URL: fmt.Sprintf("https://github.com/owner/repo/pull/%d", index+1), Draft: true, BaseRefName: "main", HeadRefName: "fix/test", HeadRefOID: "other", State: "CLOSED"}
	}
	records[100] = prRecord{Number: 101, URL: "https://github.com/owner/repo/pull/101", Draft: true, BaseRefName: "main", HeadRefName: "fix/test", HeadRefOID: "wanted", State: "OPEN"}
	first, _ := json.Marshal(records[:100])
	second, _ := json.Marshal(records)
	runner := &sequenceRunner{results: []isolation.Result{{ExitCode: 0, Stdout: first}, {ExitCode: 0, Stdout: second}}}
	service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", getenv: func(string) string { return "" }, runner: runner}
	result, found, err := service.lookup(t.Context(), request, 0)
	if err != nil || !found || result.ProviderID != "101" {
		t.Fatalf("expanded lookup=%+v found=%v err=%v", result, found, err)
	}
	if len(runner.requests) != 2 || !containsArgumentPair(runner.requests[0].Arguments, "--limit", "100") || !containsArgumentPair(runner.requests[1].Arguments, "--limit", "200") {
		t.Fatalf("lookup windows=%#v", runner.requests)
	}
}

func containsArgumentPair(arguments []string, key, value string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == key && arguments[index+1] == value {
			return true
		}
	}
	return false
}

type sequenceRunner struct {
	mu       sync.Mutex
	results  []isolation.Result
	requests []isolation.Request
}

func (runner *sequenceRunner) Run(_ context.Context, request isolation.Request) (isolation.Result, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.requests = append(runner.requests, request)
	if len(runner.results) == 0 {
		return isolation.Result{}, errors.New("unexpected command")
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	return result, nil
}

func TestRunRedactsForwardedTokensFromStderrAndRunnerErrors(t *testing.T) {
	const token = "github_pat_secret_value"
	lookup := func(key string) string {
		if key == "GH_TOKEN" {
			return token
		}
		return ""
	}
	for _, runner := range []runnerFunc{
		func(context.Context, isolation.Request) (isolation.Result, error) {
			return isolation.Result{ExitCode: 1, Stderr: []byte("failure " + token)}, nil
		},
		func(context.Context, isolation.Request) (isolation.Result, error) {
			return isolation.Result{}, errors.New("launch failed with " + token)
		},
	} {
		service := &Service{gh: "/usr/bin/gh", workingDirectory: "/safe", runner: runner, getenv: lookup}
		_, err := service.run(withCommandOutput(t.Context(), testCommandOutputBytes), "auth", "status")
		if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[REDACTED]") {
			t.Fatalf("run error was not redacted: %v", err)
		}
	}
}

func TestRunPinsExecutableWorkingDirectoryChildPathAndHost(t *testing.T) {
	const token = "github_pat_boundary"
	var launched isolation.Request
	service := &Service{gh: "/trusted/bin/gh", workingDirectory: "/private/publisher", getenv: func(key string) string {
		switch key {
		case "PATH":
			return "/candidate/bin"
		case "GH_HOST":
			return "evil.example"
		case "GH_TOKEN":
			return token
		}
		return ""
	}, runner: runnerFunc(func(_ context.Context, request isolation.Request) (isolation.Result, error) {
		launched = request
		return isolation.Result{ExitCode: 0}, nil
	})}
	if _, err := service.run(withCommandOutput(t.Context(), testCommandOutputBytes), "auth", "status", "--hostname", "github.com"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(launched.Environment, "\x00")
	if launched.Executable != "/trusted/bin/gh" || launched.Directory != "/private/publisher" ||
		!strings.Contains(joined, "PATH=/usr/bin:/bin:/usr/sbin:/sbin") || strings.Contains(joined, "/candidate/bin") ||
		strings.Contains(joined, "evil.example") || !strings.Contains(joined, "GH_TOKEN="+token) {
		t.Fatalf("publisher trust boundary was not pinned: request=%+v", launched)
	}
}

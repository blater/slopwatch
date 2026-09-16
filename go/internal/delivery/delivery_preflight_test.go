package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestPullRequestPreflightRequiresResolvableBase(t *testing.T) {
	repository, _ := deliveryRepository(t)
	service, _ := NewGitService(testExecutor{})
	workspace := fix.WorkspaceIdentity{RepositoryRoot: repository}
	for _, base := range []string{"", "does-not-exist"} {
		_, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Plan: pullRequestNewBranch, Remote: "origin", BaseBranch: base, Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes})
		if err == nil {
			t.Fatalf("base %q accepted", base)
		}
	}
	if _, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Plan: pullRequestNewBranch, Remote: "origin", BaseBranch: "HEAD", Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes}); err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("local remote accepted for pull request: %v", err)
	}
}

func TestPullRequestPreflightRequiresLiteralBaseOnAdmittedRemote(t *testing.T) {
	for _, test := range []struct {
		name       string
		base       string
		remoteBase bool
		wantError  string
	}{
		{name: "local-only base", base: "main", wantError: "does not exist on the admitted remote"},
		{name: "revision expression", base: "main~1", remoteBase: true, wantError: "not a literal branch"},
		{name: "remote branch", base: "main", remoteBase: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &GitService{executor: gitExecutor{git: "/usr/bin/git", runner: remoteBasePreflightExecutor(test.remoteBase)}}
			result, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: "/repo"},
				Plan: pullRequestNewBranch, Remote: "origin", BaseBranch: test.base, Branch: "slopwatch/fix/test", Publication: true, CommandOutputBytes: testDeliveryOutputBytes})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Preflight() error = %v", err)
				}
				return
			}
			if err != nil || result.RemoteHost != "github.com" || result.HostRepository != "owner/repo" || !strings.HasPrefix(result.RemoteIdentity, "sha256:") {
				t.Fatalf("Preflight() = %+v, %v", result, err)
			}
		})
	}
}

func remoteBasePreflightExecutor(baseExists bool) executorFunc {
	return func(_ context.Context, request isolation.Request) (isolation.Result, error) {
		arguments := request.Arguments
		last := ""
		if len(arguments) > 0 {
			last = arguments[len(arguments)-1]
		}
		joined := strings.Join(arguments, "\x00")
		switch {
		case strings.Contains(joined, "remote\x00get-url\x00--push\x00origin"):
			return isolation.Result{Stdout: []byte("https://github.com/owner/repo.git\n")}, nil
		case strings.Contains(joined, "check-ref-format\x00--branch"):
			if strings.ContainsAny(last, "~^:") {
				return isolation.Result{ExitCode: 128, Stderr: []byte("invalid branch")}, nil
			}
			return isolation.Result{Stdout: []byte(last + "\n")}, nil
		case strings.Contains(joined, "rev-parse\x00--verify\x00--quiet"):
			return isolation.Result{ExitCode: 1}, nil
		case strings.Contains(joined, "ls-remote\x00--exit-code\x00--refs"):
			if last == "refs/heads/main" && baseExists {
				return isolation.Result{Stdout: []byte(strings.Repeat("a", 40) + "\t" + last + "\n")}, nil
			}
			return isolation.Result{ExitCode: 1}, nil
		default:
			return isolation.Result{ExitCode: 128, Stderr: []byte("unexpected git command")}, nil
		}
	}
}

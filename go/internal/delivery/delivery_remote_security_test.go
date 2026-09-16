package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestDeliveryRejectsOptionLikeRemoteAliasBeforeGitExecution(t *testing.T) {
	service := &GitService{executor: gitExecutor{git: "/usr/bin/git", runner: executorFunc(func(context.Context, isolation.Request) (isolation.Result, error) {
		t.Fatal("unsafe remote alias reached Git")
		return isolation.Result{}, nil
	})}}
	_, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: "/repo"}, Plan: pushNewBranch,
		Remote: "--upload-pack=touch-pwn", Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes})
	if err == nil || !strings.Contains(err.Error(), "safe configured remote alias") {
		t.Fatalf("unsafe remote alias error=%v", err)
	}
}

func TestDeliveryRejectsCredentialBearingAndUnsupportedRemoteURLsWithoutEcho(t *testing.T) {
	repository, _ := deliveryRepository(t)
	service, _ := NewGitService(testExecutor{})
	for _, test := range []struct {
		name, remoteURL, secret string
	}{
		{name: "https credentials", remoteURL: "https://user:super-secret-token@github.com/owner/repo.git", secret: "super-secret-token"},
		{name: "ssh password", remoteURL: "ssh://git:super-secret-token@github.com/owner/repo.git", secret: "super-secret-token"},
		{name: "http", remoteURL: "http://github.com/owner/repo.git"},
		{name: "ext", remoteURL: "ext::sh -c pwn"},
		{name: "ssh host option", remoteURL: "git@-oProxyCommand=touch-pwn:owner/repo.git"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gitCommand(t, repository, "remote", "set-url", "--push", "origin", test.remoteURL)
			_, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: repository}, Plan: pushNewBranch,
				Remote: "origin", Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes})
			if err == nil || test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Fatalf("unsafe URL error=%v", err)
			}
		})
	}
}

func TestValidateRemoteURLAllowsCredentialFreeSupportedForms(t *testing.T) {
	for _, value := range []string{
		"https://github.com/owner/repo.git",
		"ssh://git@github.com/owner/repo.git",
		"git@github.com:owner/repo.git",
		"file:///tmp/repository.git",
		"/tmp/repository.git",
	} {
		if err := validateRemoteURL(value); err != nil {
			t.Errorf("validateRemoteURL(%q) = %v", value, err)
		}
	}
}

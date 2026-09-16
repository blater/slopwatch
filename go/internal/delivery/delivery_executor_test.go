package delivery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestDeliveryGitNeutralizesRepositoryCredentialCommands(t *testing.T) {
	var launched isolation.Request
	service := &GitService{executor: gitExecutor{git: "/usr/bin/git", runner: executorFunc(func(_ context.Context, request isolation.Request) (isolation.Result, error) {
		launched = request
		return isolation.Result{}, nil
	})}}
	if _, err := service.executor.bytes(withCommandOutput(t.Context(), testDeliveryOutputBytes), "/candidate", false, "status", "--porcelain=v1"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-c", "credential.helper="}, {"-c", "credential.interactive=never"}, {"-c", "core.askPass="}} {
		if !containsArgumentPair(launched.Arguments, pair[0], pair[1]) {
			t.Fatalf("delivery Git omitted credential command guard %q: %#v", pair[1], launched.Arguments)
		}
	}
	joined := strings.Join(launched.Environment, "\x00")
	for _, unsafe := range []string{"GIT_ASKPASS=", "SSH_ASKPASS=", "SSH_AUTH_SOCK="} {
		if strings.Contains(joined, unsafe) {
			t.Fatalf("delivery Git inherited credential command environment %q: %#v", unsafe, launched.Environment)
		}
	}
}

func TestGitServiceLetsGitHandleAttributesAtRuntime(t *testing.T) {
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".gitattributes"), []byte("main.go filter=late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitService(testExecutor{})
	fingerprint, err := service.diffFingerprint(withCommandOutput(context.Background(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, identityErr := remoteIdentity(remote)
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	result, err := service.CreateCommit(context.Background(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)}, DiffHash: fingerprint, Plan: pushNewBranch, Paths: []fix.RepoPath{"main.go", ".gitattributes"}, Branch: "slopwatch/fix/attrs", Remote: "origin", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes})
	if err != nil || result.Commit == "" {
		t.Fatalf("Git could not apply configured attributes at runtime: result=%+v err=%v", result, err)
	}
	if head := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD")); head != base {
		t.Fatalf("publication moved HEAD: %s", head)
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

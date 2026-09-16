package delivery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

type publicationFixture struct {
	service    *GitService
	request    Request
	repository string
	remote     string
	base       string
}

func newPublicationFixture(t *testing.T) publicationFixture {
	t.Helper()
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewGitService(testExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := service.diffFingerprint(withCommandOutput(context.Background(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteIdentity(remote)
	if err != nil {
		t.Fatal(err)
	}
	return publicationFixture{service: service, repository: repository, remote: remote, base: base,
		request: Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)},
			DiffHash: fingerprint, Plan: pushNewBranch, Paths: []fix.RepoPath{"main.go"}, Branch: "slopwatch/fix/main-test", Remote: "origin", CommitTitle: "Refactor main.go", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes}}
}

func (fixture publicationFixture) assertPublished(t *testing.T, result Result) {
	t.Helper()
	if !result.Pushed || result.Commit == "" || result.LocalRef != "refs/heads/slopwatch/fix/main-test" {
		t.Fatalf("delivery result = %+v", result)
	}
	if head := strings.TrimSpace(gitResult(t, fixture.repository, "rev-parse", "HEAD")); head != fixture.base {
		t.Fatalf("publication moved candidate HEAD to %s, want pinned base %s", head, fixture.base)
	}
	if status := gitResult(t, fixture.repository, "status", "--porcelain=v1"); !strings.Contains(status, "main.go") {
		t.Fatalf("publication mutated candidate index/status: %q", status)
	}
	published := gitResult(t, fixture.repository, "show", string(result.Commit)+":main.go")
	if !strings.Contains(published, "func main") {
		t.Fatalf("published commit omitted candidate bytes: %q", published)
	}
	remoteOID := strings.Fields(gitResult(t, fixture.repository, "ls-remote", fixture.remote, result.RemoteRef))[0]
	if remoteOID != string(result.Commit) {
		t.Fatalf("remote oid = %s, want %s", remoteOID, result.Commit)
	}
}

func (fixture publicationFixture) assertRejectsReuse(t *testing.T) {
	t.Helper()
	request := fixture.request
	request.Job = "job-other"
	request.Candidate.Job = "job-other"
	request.CommitTitle = "Again"
	if _, err := fixture.service.PublishCommit(context.Background(), request); err == nil {
		t.Fatal("reused existing branch")
	}
}

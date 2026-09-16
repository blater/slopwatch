package delivery

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestDeliveryRejectsHostileGitHubPortAndChangedLocalRemote(t *testing.T) {
	if err := validateRemoteURL("https://github.com:8443/owner/repo.git"); err == nil {
		t.Fatal("explicit hostile GitHub port was accepted")
	}
	repository, firstRemote := deliveryRepository(t)
	_, secondRemote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitService(testExecutor{})
	fingerprint, err := service.diffFingerprint(withCommandOutput(t.Context(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteIdentity(firstRemote)
	if err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "remote", "set-url", "--push", "origin", secondRemote)
	_, err = service.CreateCommit(t.Context(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository,
		GitCommonDir: common, BaseCommit: fix.ObjectID(base)}, DiffHash: fingerprint, Plan: pushNewBranch, Paths: []fix.RepoPath{"main.go"}, Branch: "slopwatch/fix/remote-changed", Remote: "origin",
		ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes})
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("changed local remote accepted: %v", err)
	}
	if command := exec.Command("git", "-C", repository, "rev-parse", "--verify", "refs/heads/slopwatch/fix/remote-changed"); command.Run() == nil {
		t.Fatal("changed remote created a local branch")
	}
}

func TestCanonicalLocalRemoteRemovesSymlinkComponents(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	if err := os.Mkdir(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.git")
	if err := os.Symlink(remote, alias); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{alias, (&url.URL{Scheme: "file", Path: alias}).String()} {
		canonical, err := canonicalLocalRemote(value)
		if err != nil || strings.Contains(canonical, "alias.git") || !strings.Contains(canonical, "remote.git") {
			t.Fatalf("canonicalLocalRemote(%q) = %q, %v", value, canonical, err)
		}
	}
}

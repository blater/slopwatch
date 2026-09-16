package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitService) CreateLocalRef(ctx context.Context, request Request, result Result) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if result.Commit == "" || result.LocalRef != "" {
		return result, errors.New("local ref step requires only a committed object")
	}
	if request.Plan.Git == fix.GitCommitCurrent {
		ref := "refs/heads/" + request.Branch
		if result.LocalRef == ref {
			return result, nil
		}
		return result, errors.New("current branch commit did not record its local ref")
	}
	ref := "refs/heads/" + request.Branch
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if exists, oid, err := service.ref(ctx, request.Candidate.RepositoryRoot, ref); err != nil {
		return result, err
	} else if exists {
		if oid == string(result.Commit) {
			result.LocalRef = ref
			return result, nil
		}
		return result, errors.New("delivery branch appeared locally at a different commit")
	}
	format, err := service.executor.text(ctx, request.Candidate.RepositoryRoot, "rev-parse", "--show-object-format")
	if err != nil {
		return result, err
	}
	zeros := strings.Repeat("0", 40)
	if format == "sha256" {
		zeros = strings.Repeat("0", 64)
	}
	if _, err := service.executor.bytes(ctx, request.Candidate.RepositoryRoot, false, "update-ref", ref, string(result.Commit), zeros); err != nil {
		result.Ambiguous = true
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("create local delivery ref: %w", err)
	}
	result.LocalRef = ref
	return result, nil
}

func (service *GitService) verifyRequestRemote(ctx context.Context, request Request) error {
	if request.ExpectedRemoteIdentity == "" {
		return errors.New("delivery request lacks the exact admitted remote identity")
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return err
	}
	return verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository)
}

func (service *GitService) CreateRemoteRef(ctx context.Context, request Request, result Result) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if result.Commit == "" || result.LocalRef == "" || result.Pushed {
		return result, errors.New("remote ref step requires a committed local ref")
	}
	ref := result.LocalRef
	commit := string(result.Commit)
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return result, err
	}
	if err := verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository); err != nil {
		return result, err
	}
	exists, oid, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
	if err != nil {
		return result, err
	}
	if exists {
		if oid == commit {
			result.RemoteRef, result.Pushed = ref, true
			result.Repository = remoteRepositoryURL(remoteURL)
			return result, nil
		}
		if request.Plan.Git == fix.GitCommitNewBranch {
			return result, errors.New("delivery branch appeared remotely at a different commit")
		}
	}
	arguments := []string{"push", "--porcelain"}
	if request.Plan.Git == fix.GitCommitNewBranch {
		arguments = append(arguments, "--force-with-lease="+ref+":")
	}
	arguments = append(arguments, remoteURL, commit+":"+ref)
	if _, err := service.executor.bytes(ctx, request.Candidate.RepositoryRoot, false, arguments...); err != nil {
		result.Ambiguous = true
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("create remote delivery ref: %w", err)
	}
	exists, remoteCommit, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
	if err != nil || !exists || remoteCommit != commit {
		result.Ambiguous = true
		result.Diagnostic = "remote ref could not be verified at the committed object"
		return result, errors.New(result.Diagnostic)
	}
	result.RemoteRef = ref
	result.Pushed = true
	result.Repository = remoteRepositoryURL(remoteURL)
	return result, nil
}

func (service *GitService) Reconcile(ctx context.Context, request Request, previous Result) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if previous.Commit == "" {
		return previous, errors.New("delivery reconciliation requires the saved commit")
	}
	ref := previous.RemoteRef
	if ref == "" {
		ref = "refs/heads/" + request.Branch
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return previous, err
	}
	if err := verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository); err != nil {
		return previous, err
	}
	if previous.LocalRef == "" {
		localRef := "refs/heads/" + request.Branch
		exists, commit, err := service.ref(ctx, request.Candidate.RepositoryRoot, localRef)
		if err != nil {
			return previous, err
		}
		if !exists {
			previous.Ambiguous = false
			previous.Diagnostic = "local ref is absent"
			return previous, nil
		}
		if commit != string(previous.Commit) {
			previous.Ambiguous = true
			previous.Diagnostic = "local ref exists at a different commit"
			return previous, errors.New(previous.Diagnostic)
		}
		previous.LocalRef = localRef
		previous.Ambiguous = false
		previous.Diagnostic = ""
		return previous, nil
	}
	exists, commit, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
	if err != nil {
		return previous, err
	}
	if !exists {
		previous.Ambiguous = false
		previous.Pushed = false
		previous.Diagnostic = "remote ref is absent"
		return previous, nil
	}
	if commit != string(previous.Commit) {
		previous.Ambiguous = true
		previous.Diagnostic = "remote ref exists at a different commit"
		return previous, errors.New(previous.Diagnostic)
	}
	previous.RemoteRef = ref
	previous.Pushed = true
	previous.Repository = remoteRepositoryURL(remoteURL)
	previous.Ambiguous = false
	previous.Diagnostic = ""
	return previous, nil
}

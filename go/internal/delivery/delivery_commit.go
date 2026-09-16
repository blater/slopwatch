package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitService) PublishCommit(ctx context.Context, request Request) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	result, err := service.CreateCommit(ctx, request)
	if err != nil {
		return result, err
	}
	result, err = service.CreateLocalRef(ctx, request, result)
	if err != nil {
		return result, err
	}
	return service.CreateRemoteRef(ctx, request, result)
}

func (service *GitService) CreateCommit(ctx context.Context, request Request) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	result := Result{}
	if request.Job == "" || request.Candidate.RepositoryRoot == "" || request.Branch == "" || request.DiffHash == "" || len(request.Paths) == 0 || !request.Plan.Valid() || request.Plan.Git == fix.GitLeaveUncommitted {
		return result, errors.New("delivery request is incomplete")
	}
	if request.Plan.Publish != fix.PublishLocal {
		if !validRemoteAlias(request.Remote) {
			return result, errors.New("delivery remote must be a safe configured remote alias")
		}
		if err := service.verifyRequestRemote(ctx, request); err != nil {
			return result, err
		}
	}
	if err := validateLiteralBranch(service.executor, ctx, request.Candidate.RepositoryRoot, request.Branch); err != nil {
		return result, fmt.Errorf("invalid delivery branch: %w", err)
	}
	if request.Plan.Git == fix.GitCommitCurrent {
		return service.createCurrentBranchCommit(ctx, request)
	}
	ref := "refs/heads/" + request.Branch
	if exists, _, err := service.executor.ref(ctx, request.Candidate.RepositoryRoot, ref); err != nil {
		return result, err
	} else if exists {
		return result, errors.New("delivery branch already exists locally")
	}
	if request.Plan.Publish != fix.PublishLocal {
		if exists, _, err := service.executor.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref); err != nil {
			return result, err
		} else if exists {
			return result, errors.New("delivery branch already exists remotely")
		}
	}
	operation, err := beginPublicationOperation(service.executor, request.Candidate.RepositoryRoot, request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer operation.Close()
	if err := operation.readTree(ctx, string(request.Candidate.BaseCommit)); err != nil {
		return result, fmt.Errorf("initialize private publication index: %w", err)
	}
	if err := operation.stage(ctx, request.Paths); err != nil {
		return result, fmt.Errorf("stage candidate in private publication index: %w", err)
	}
	tree, err := operation.tree(ctx)
	if err != nil {
		return result, fmt.Errorf("write candidate tree: %w", err)
	}
	baseTree, err := service.executor.text(ctx, request.Candidate.RepositoryRoot, "rev-parse", string(request.Candidate.BaseCommit)+"^{tree}")
	if err != nil {
		return result, err
	}
	if tree == baseTree {
		return result, errors.New("candidate has no changes to publish")
	}
	commit, err := operation.commit(ctx, tree, string(request.Candidate.BaseCommit), request.CommitTitle, request.CommitBody)
	if err != nil {
		return result, fmt.Errorf("create candidate commit object: %w", err)
	}
	result.Commit = fix.ObjectID(commit)
	return result, nil
}

func (service *GitService) createCurrentBranchCommit(ctx context.Context, request Request) (Result, error) {
	result := Result{}
	if request.Candidate.WorkspaceMode != fix.WorkspaceCurrent {
		return result, errors.New("committing the current branch requires working in the current files")
	}
	branch, err := service.executor.text(ctx, request.Candidate.RepositoryRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != request.Branch {
		return result, errors.New("the current Git branch changed before commit")
	}
	head, err := service.executor.text(ctx, request.Candidate.RepositoryRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != request.Candidate.BaseCommit {
		return result, errors.New("the current Git commit changed before commit")
	}
	operation, err := beginPublicationOperation(service.executor, request.Candidate.RepositoryRoot, request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer operation.Close()
	if err := operation.readTree(ctx, head); err != nil {
		return result, fmt.Errorf("initialize current-branch commit: %w", err)
	}
	if err := operation.stage(ctx, request.Paths); err != nil {
		return result, fmt.Errorf("stage current-branch files: %w", err)
	}
	tree, err := operation.tree(ctx)
	if err != nil {
		return result, fmt.Errorf("write current-branch tree: %w", err)
	}
	commit, err := operation.commit(ctx, tree, head, request.CommitTitle, request.CommitBody)
	if err != nil {
		return result, fmt.Errorf("create current-branch commit: %w", err)
	}
	ref := "refs/heads/" + branch
	if _, err := service.executor.bytes(ctx, request.Candidate.RepositoryRoot, false, "update-ref", ref, commit, head); err != nil {
		result.Commit, result.Ambiguous, result.Diagnostic = fix.ObjectID(commit), true, err.Error()
		return result, fmt.Errorf("advance current branch: %w", err)
	}
	resetArguments := []string{"reset", "-q", commit, "--"}
	for _, path := range request.Paths {
		resetArguments = append(resetArguments, path.String())
	}
	if _, err := service.executor.bytes(ctx, request.Candidate.RepositoryRoot, false, resetArguments...); err != nil {
		result.Commit, result.LocalRef, result.Ambiguous, result.Diagnostic = fix.ObjectID(commit), ref, true, err.Error()
		return result, fmt.Errorf("refresh committed files in the Git index: %w", err)
	}
	result.Commit, result.LocalRef = fix.ObjectID(commit), ref
	return result, nil
}

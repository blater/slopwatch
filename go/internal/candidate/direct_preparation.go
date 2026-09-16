package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *DirectService) Prepare(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, error) {
	if !validJobID(request.Job) || request.Mode != fix.WorkspaceCurrent || len(request.Targets) == 0 {
		return fix.CandidateIdentity{}, errors.New("prepare current files: job and targets are required")
	}
	if existing, found, err := service.DiscoverPrepared(ctx, request); err != nil || found {
		return existing, err
	}
	root, analysis, err := canonicalCurrentRoots(request.Workspace)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	identity := fix.CandidateIdentity{Job: request.Job, WorkspaceMode: fix.WorkspaceCurrent, Repository: request.Workspace.Repository,
		RepositoryRoot: root, AnalysisRoot: analysis, GitCommonDir: request.Workspace.GitCommonDir, BaseCommit: request.Workspace.BaseCommit,
		StagingRoot: filepath.Join(service.stateRoot, string(request.Job), "staging")}
	baseline, err := scanDirectFiles(ctx, root)
	if err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("snapshot current files: %w", err)
	}
	record := directRecord{Version: 1, Identity: identity, Targets: sortedRepoPaths(request.Targets), Allowed: sortedRepoPaths(request.AllowedPaths), Scope: request.AllowedScope, Baseline: baseline}
	jobRoot := filepath.Join(service.stateRoot, string(request.Job))
	if err := os.Mkdir(jobRoot, 0o700); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("reserve current-file candidate: %w", err)
	}
	if err := os.Mkdir(identity.StagingRoot, 0o700); err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, fmt.Errorf("create current-file staging: %w", err)
	}
	if err := writeDirectRecord(filepath.Join(jobRoot, directRecordName), record); err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, err
	}
	return identity, nil
}

func (service *DirectService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if err := ctx.Err(); err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	path := filepath.Join(service.stateRoot, string(request.Job), directRecordName)
	record, err := readDirectRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return fix.CandidateIdentity{}, false, nil
	}
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	root, analysis, err := canonicalCurrentRoots(request.Workspace)
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	if record.Version != 1 || record.Identity.Job != request.Job || record.Identity.WorkspaceMode != fix.WorkspaceCurrent ||
		record.Identity.RepositoryRoot != root || record.Identity.AnalysisRoot != analysis || record.Scope != request.AllowedScope ||
		!sameRepoPaths(record.Targets, sortedRepoPaths(request.Targets)) || !sameRepoPaths(record.Allowed, sortedRepoPaths(request.AllowedPaths)) {
		return fix.CandidateIdentity{}, false, errors.New("current-file candidate state does not match the requested job")
	}
	return record.Identity, true, nil
}

func canonicalCurrentRoots(workspace fix.WorkspaceIdentity) (string, string, error) {
	root, err := filepath.EvalSymlinks(workspace.RepositoryRoot)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize workspace: %w", err)
	}
	analysis, err := filepath.EvalSymlinks(workspace.AnalysisRoot)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize analysis root: %w", err)
	}
	if !within(root, analysis) {
		return "", "", errors.New("analysis root is outside the workspace")
	}
	return root, analysis, nil
}

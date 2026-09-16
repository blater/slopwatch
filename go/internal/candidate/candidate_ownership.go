package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitWorktreeService) expectedOwnership(request PrepareRequest) (ownershipRecord, error) {
	analysisRoot, err := filepath.EvalSymlinks(request.Workspace.AnalysisRoot)
	if err != nil {
		return ownershipRecord{}, fmt.Errorf("canonicalize analysis root: %w", err)
	}
	analysisRelative, err := filepath.Rel(request.Workspace.RepositoryRoot, analysisRoot)
	if err != nil || analysisRelative == ".." || strings.HasPrefix(analysisRelative, ".."+string(filepath.Separator)) {
		return ownershipRecord{}, errors.New("candidate analysis root is outside repository")
	}
	worktree := filepath.Join(service.stateRoot, string(request.Job), "worktree")
	candidateAnalysisRoot := worktree
	if analysisRelative != "." {
		candidateAnalysisRoot = filepath.Join(worktree, analysisRelative)
	}
	targets := append([]fix.RepoPath(nil), request.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	allowed := append([]fix.RepoPath(nil), request.AllowedPaths...)
	if request.AllowedScope != "repository" && len(allowed) == 0 {
		allowed = append(allowed, targets...)
	}
	sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
	identity := fix.CandidateIdentity{Job: request.Job, Repository: request.Workspace.Repository, RepositoryRoot: worktree,
		WorkspaceMode: fix.WorkspaceWorktree,
		AnalysisRoot:  candidateAnalysisRoot, GitCommonDir: request.Workspace.GitCommonDir, BaseCommit: request.Workspace.BaseCommit,
		StagingRoot: filepath.Join(service.stateRoot, string(request.Job), "staging")}
	return ownershipRecord{Version: 1, Identity: identity, Targets: targets, Allowed: allowed, Scope: request.AllowedScope,
		CommandOutputBytes: request.CommandOutputBytes}, nil
}

func (service *GitWorktreeService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return err
	}
	jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
	for _, name := range []string{ownershipName, reservationName, seedManifestName, seedCompletedName} {
		if err := os.Remove(filepath.Join(jobRoot, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("release preserved worktree: %w", err)
		}
	}
	if err := os.RemoveAll(filepath.Join(jobRoot, "staging")); err != nil {
		return fmt.Errorf("release preserved worktree staging: %w", err)
	}
	service.registry.deletePolicy(identity.Job)
	return service.registry.release(identity.GitCommonDir, identity.Job)
}

func (service *GitWorktreeService) verifyReservedWorktree(ctx context.Context, identity fix.CandidateIdentity) error {
	common, err := service.executor.text(ctx, identity.RepositoryRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("verify reserved candidate Git directory: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil || common != identity.GitCommonDir {
		return errors.New("reserved candidate Git common directory does not match")
	}
	head, err := service.executor.text(ctx, identity.RepositoryRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != identity.BaseCommit {
		return errors.New("reserved candidate HEAD does not match its admitted base commit")
	}
	return nil
}

func sameOwnership(left, right ownershipRecord) bool {
	if left.Version != right.Version || left.Identity != right.Identity || left.Scope != right.Scope || left.CommandOutputBytes != right.CommandOutputBytes ||
		len(left.Targets) != len(right.Targets) || len(left.Allowed) != len(right.Allowed) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	for index := range left.Allowed {
		if left.Allowed[index] != right.Allowed[index] {
			return false
		}
	}
	return true
}

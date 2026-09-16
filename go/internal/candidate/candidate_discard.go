package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/gitmanifest"
)

func (service *GitWorktreeService) Diff(ctx context.Context, identity fix.CandidateIdentity) (DiffSnapshot, error) {
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return DiffSnapshot{}, err
	}
	ctx = withCommandOutputBytes(ctx, policy.commandOutputBytes)
	data, err := service.executor.bytes(ctx, identity.RepositoryRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return DiffSnapshot{}, err
	}
	manifest, err := gitmanifest.Build(identity.RepositoryRoot, data)
	if err != nil {
		return DiffSnapshot{}, err
	}
	files := make([]DiffFile, 0, len(manifest.Entries))
	for _, changed := range manifest.Entries {
		files = append(files, DiffFile{Path: changed.Path, Previous: changed.Previous, Status: changed.Status, Mode: changed.Mode, Kind: changed.Kind, DiffHash: changed.Hash})
	}
	return DiffSnapshot{Files: files, Fingerprint: manifest.Fingerprint, Scope: fix.ScopeClean}, nil
}

func (service *GitWorktreeService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath, maximum int64) (File, error) {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return File{}, err
	}
	return readCandidateFile(ctx, identity.RepositoryRoot, path, maximum)
}

func (service *GitWorktreeService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return err
	}
	ctx = withCommandOutputBytes(ctx, policy.commandOutputBytes)
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return err
	}
	_, commandErr := service.executor.bytes(ctx, identity.RepositoryRoot, "worktree", "remove", "--force", identity.RepositoryRoot)
	lockErr := lock.Close()
	if commandErr == nil {
		jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
		commandErr = os.RemoveAll(jobRoot)
	}
	service.registry.deletePolicy(identity.Job)
	if commandErr == nil {
		commandErr = service.registry.release(identity.GitCommonDir, identity.Job)
	}
	return errors.Join(commandErr, lockErr)
}

func (service *GitWorktreeService) ReconcileDiscard(ctx context.Context, identity fix.CandidateIdentity) error {
	if !validJobID(identity.Job) {
		return errors.New("candidate identity has an invalid job ID")
	}
	jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
	expected := filepath.Join(jobRoot, "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return errors.New("candidate identity does not name its exact managed worktree")
	}
	if _, err := os.Lstat(jobRoot); errors.Is(err, os.ErrNotExist) {
		ctx = withCommandOutputBytes(ctx, service.discoveryCommandOutputBytes)
		lock, lockErr := acquireRepositoryLock(identity.GitCommonDir)
		if lockErr != nil {
			return lockErr
		}
		_, commandErr := service.executor.bytes(ctx, identity.GitCommonDir, "worktree", "remove", "--force", expected)
		if commandErr != nil {
			listing, listErr := service.executor.bytes(ctx, identity.GitCommonDir, "worktree", "list", "--porcelain")
			if listErr == nil && !worktreeRegistered(listing, expected) {
				commandErr = nil
			} else {
				commandErr = errors.Join(commandErr, listErr)
			}
		}
		lockErr = lock.Close()
		var releaseErr error
		if commandErr == nil && lockErr == nil {
			service.registry.deletePolicy(identity.Job)
			releaseErr = service.registry.release(identity.GitCommonDir, identity.Job)
		}
		return errors.Join(commandErr, lockErr, releaseErr)
	} else if err != nil {
		return fmt.Errorf("inspect candidate discard state: %w", err)
	}
	record, err := readOwnership(jobRoot)
	if err != nil || record.Version != 1 || record.Identity != identity {
		return errors.New("candidate discard ownership marker does not match saved identity")
	}
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
	if err := service.registry.retain(identity.GitCommonDir, identity.Job); err != nil {
		return err
	}
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return err
	}
	_, commandErr := service.executor.bytes(ctx, identity.GitCommonDir, "worktree", "remove", "--force", expected)
	if commandErr != nil {
		listing, listErr := service.executor.bytes(ctx, identity.GitCommonDir, "worktree", "list", "--porcelain")
		if listErr == nil && !worktreeRegistered(listing, expected) {
			commandErr = nil
		} else {
			commandErr = errors.Join(commandErr, listErr)
		}
	}
	lockErr := lock.Close()
	if commandErr == nil && lockErr == nil {
		commandErr = os.RemoveAll(jobRoot)
	}
	service.registry.deletePolicy(identity.Job)
	if commandErr == nil && lockErr == nil {
		commandErr = service.registry.release(identity.GitCommonDir, identity.Job)
	}
	return errors.Join(commandErr, lockErr)
}

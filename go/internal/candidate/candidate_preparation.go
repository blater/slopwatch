package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

// candidatePreparation owns one admitted candidate's reservation, worktree
// lock, snapshot, and cleanup state. The public service only coordinates
// discovery and delegates this lifecycle after repository identity is fixed.
type candidatePreparation struct {
	executor     gitExecutor
	registry     *candidateRegistry
	request      PrepareRequest
	discovered   fix.WorkspaceIdentity
	ownership    ownershipRecord
	jobRoot      string
	worktree     string
	lock         *repositoryLock
	manifest     seedManifest
	manifestHash string
	retained     bool
}

func newCandidatePreparation(executor gitExecutor, registry *candidateRegistry, stateRoot string, request PrepareRequest, discovered fix.WorkspaceIdentity, ownership ownershipRecord) *candidatePreparation {
	return &candidatePreparation{executor: executor, registry: registry, request: request, discovered: discovered, ownership: ownership,
		jobRoot: filepath.Join(stateRoot, string(request.Job)), worktree: ownership.Identity.RepositoryRoot}
}

func (preparation *candidatePreparation) run(ctx context.Context) (fix.CandidateIdentity, error) {
	if err := preparation.registry.retain(preparation.discovered.GitCommonDir, preparation.request.Job); err != nil {
		return fix.CandidateIdentity{}, err
	}
	preparation.retained = true
	defer preparation.releaseIfNeeded()
	if err := preparation.reserveSnapshot(ctx); err != nil {
		return fix.CandidateIdentity{}, err
	}
	if err := preparation.createWorktree(ctx); err != nil {
		return fix.CandidateIdentity{}, err
	}
	if err := preparation.persistOwnership(); err != nil {
		return fix.CandidateIdentity{}, err
	}
	if err := preparation.closeLock(ctx); err != nil {
		return fix.CandidateIdentity{}, err
	}
	preparation.registry.setPolicy(preparation.request.Job, preparation.policy())
	preparation.retained = false
	return preparation.ownership.Identity, nil
}

func (preparation *candidatePreparation) reserveSnapshot(ctx context.Context) error {
	if err := os.MkdirAll(preparation.jobRoot, 0o700); err != nil {
		return fmt.Errorf("create job state directory: %w", err)
	}
	if err := os.Mkdir(filepath.Join(preparation.jobRoot, "staging"), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create candidate staging directory: %w", err)
	}
	if err := ensureCandidateRecord(preparation.jobRoot, reservationName, preparation.ownership); err != nil {
		return fmt.Errorf("persist candidate reservation: %w", err)
	}
	manifest, err := preparation.executor.snapshotWorkingChanges(ctx, preparation.discovered.RepositoryRoot, preparation.ownership.Identity.StagingRoot,
		preparation.request.AllowedScope, preparation.request.AllowedPaths)
	if err != nil {
		_ = os.RemoveAll(preparation.jobRoot)
		return err
	}
	manifestHash, err := writeSeedManifest(preparation.jobRoot, manifest)
	if err != nil {
		_ = os.RemoveAll(preparation.jobRoot)
		return fmt.Errorf("persist workspace snapshot: %w", err)
	}
	preparation.manifestHash = manifestHash
	preparation.manifest = manifest
	return nil
}

func (preparation *candidatePreparation) createWorktree(ctx context.Context) error {
	lock, err := acquireRepositoryLock(preparation.discovered.GitCommonDir)
	if err != nil {
		return err
	}
	preparation.lock = lock
	_, commandErr := preparation.executor.bytes(ctx, preparation.discovered.RepositoryRoot, "worktree", "add", "--detach", "--no-checkout", preparation.worktree, string(preparation.discovered.BaseCommit))
	if commandErr == nil {
		_, commandErr = preparation.executor.bytes(ctx, preparation.worktree, "checkout", "--detach", string(preparation.discovered.BaseCommit), "--")
	}
	if commandErr == nil {
		commandErr = applySeedManifest(ctx, preparation.ownership.Identity.StagingRoot, preparation.worktree, preparation.manifest)
	}
	if commandErr == nil {
		commandErr = writeSeedCompletion(preparation.jobRoot, seedCompletion{Version: 1, ManifestHash: preparation.manifestHash})
	}
	if commandErr == nil {
		return nil
	}
	return preparation.cleanupWorktree(commandErr)
}

func (preparation *candidatePreparation) persistOwnership() error {
	if err := writeOwnership(preparation.jobRoot, preparation.ownership); err != nil {
		return preparation.cleanupWorktree(fmt.Errorf("persist candidate ownership: %w", err))
	}
	return nil
}

func (preparation *candidatePreparation) cleanupWorktree(cause error) error {
	// Cleanup runs with a fresh context so cancellation cannot strand a
	// registered worktree; an error keeps the durable reservation for recovery.
	cleanupCtx := withCommandOutputBytes(context.Background(), preparation.request.CommandOutputBytes)
	_, cleanupErr := preparation.executor.bytes(cleanupCtx, preparation.discovered.RepositoryRoot, "worktree", "remove", "--force", preparation.worktree)
	if preparation.lock != nil {
		_ = preparation.lock.Close()
		preparation.lock = nil
	}
	if cleanupErr == nil {
		cleanupErr = os.RemoveAll(preparation.jobRoot)
	}
	return errors.Join(cause, cleanupErr)
}

func (preparation *candidatePreparation) closeLock(ctx context.Context) error {
	if preparation.lock == nil {
		return nil
	}
	if err := preparation.lock.Close(); err == nil {
		preparation.lock = nil
		return nil
	} else {
		cleanupLock, lockErr := acquireRepositoryLock(preparation.discovered.GitCommonDir)
		if lockErr == nil {
			_, _ = preparation.executor.bytes(ctx, preparation.discovered.RepositoryRoot, "worktree", "remove", "--force", preparation.worktree)
			lockErr = cleanupLock.Close()
		}
		_ = os.RemoveAll(preparation.jobRoot)
		preparation.lock = nil
		return errors.Join(err, lockErr)
	}
}

func (preparation *candidatePreparation) policy() candidatePolicy {
	allowed := make(map[fix.RepoPath]bool, len(preparation.ownership.Allowed))
	for _, path := range preparation.ownership.Allowed {
		allowed[path] = true
	}
	return candidatePolicy{scope: preparation.request.AllowedScope, allowed: allowed, commandOutputBytes: preparation.request.CommandOutputBytes}
}

func (preparation *candidatePreparation) releaseIfNeeded() {
	if preparation.retained {
		_ = preparation.registry.release(preparation.discovered.GitCommonDir, preparation.request.Job)
	}
}

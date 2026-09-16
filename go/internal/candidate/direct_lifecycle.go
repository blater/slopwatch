package candidate

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *DirectService) Recover(_ context.Context, identity fix.CandidateIdentity, targets []fix.RepoPath, scope string, allowed []fix.RepoPath) error {
	record, err := loadDirectRecord(service.stateRoot, identity)
	if err != nil {
		return err
	}
	if !record.matchesPolicy(targets, allowed, scope) {
		return errors.New("current-file candidate policy changed since admission")
	}
	return nil
}

func (service *DirectService) ReconcileDiscard(_ context.Context, identity fix.CandidateIdentity) error {
	if !validJobID(identity.Job) || identity.WorkspaceMode != fix.WorkspaceCurrent {
		return errors.New("invalid current-file candidate identity")
	}
	return os.RemoveAll(filepath.Join(service.stateRoot, string(identity.Job)))
}

func (service *DirectService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	if _, err := loadDirectRecord(service.stateRoot, identity); err != nil {
		return err
	}
	return service.ReconcileDiscard(ctx, identity)
}

func (service *DirectService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	return service.Discard(ctx, identity)
}

func (service *DirectService) Close() error { return nil }

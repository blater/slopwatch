package candidate

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/fix"
)

// StrategyService keeps candidate location policy out of the controller.
// Every operation is routed from the durable candidate identity.
type StrategyService struct {
	current  Strategy
	worktree Service
}

func NewStrategyService(current Strategy, worktree Service) (*StrategyService, error) {
	if current == nil || worktree == nil {
		return nil, errors.New("candidate strategies require current-file and worktree services")
	}
	return &StrategyService{current: current, worktree: worktree}, nil
}

func (service *StrategyService) selected(mode fix.WorkspaceMode) (Strategy, error) {
	switch mode {
	case fix.WorkspaceCurrent:
		return service.current, nil
	case fix.WorkspaceWorktree:
		return service.worktree, nil
	default:
		return nil, errors.New("candidate workspace mode is invalid")
	}
}

func (service *StrategyService) Prepare(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, error) {
	selected, err := service.selected(request.Mode)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	return selected.Prepare(ctx, request)
}
func (service *StrategyService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if request.Mode != fix.WorkspaceWorktree {
		return fix.CandidateIdentity{}, false, errors.New("candidate recovery requires a worktree")
	}
	return service.worktree.DiscoverPrepared(ctx, request)
}
func (service *StrategyService) Diff(ctx context.Context, identity fix.CandidateIdentity) (DiffSnapshot, error) {
	selected, err := service.selected(identity.WorkspaceMode)
	if err != nil {
		return DiffSnapshot{}, err
	}
	return selected.Diff(ctx, identity)
}
func (service *StrategyService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath, maximum int64) (File, error) {
	selected, err := service.selected(identity.WorkspaceMode)
	if err != nil {
		return File{}, err
	}
	return selected.ReadFile(ctx, identity, path, maximum)
}
func (service *StrategyService) Recover(ctx context.Context, identity fix.CandidateIdentity, targets []fix.RepoPath, scope string, allowed []fix.RepoPath) error {
	if identity.WorkspaceMode != fix.WorkspaceWorktree {
		return errors.New("candidate recovery requires a worktree")
	}
	return service.worktree.Recover(ctx, identity, targets, scope, allowed)
}
func (service *StrategyService) ReconcileDiscard(ctx context.Context, identity fix.CandidateIdentity) error {
	if identity.WorkspaceMode != fix.WorkspaceWorktree {
		return errors.New("candidate recovery requires a worktree")
	}
	return service.worktree.ReconcileDiscard(ctx, identity)
}
func (service *StrategyService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	selected, err := service.selected(identity.WorkspaceMode)
	if err != nil {
		return err
	}
	return selected.Discard(ctx, identity)
}
func (service *StrategyService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	selected, err := service.selected(identity.WorkspaceMode)
	if err != nil {
		return err
	}
	return selected.Release(ctx, identity)
}
func (service *StrategyService) Close() error {
	return errors.Join(service.current.Close(), service.worktree.Close())
}

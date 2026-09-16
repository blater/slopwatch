package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

// DiscoverPrepared returns only a candidate whose durable ownership marker,
// repository identity, base commit, targets, and frozen path policy exactly
// match the request. It removes only an exact matching reservation proven
// incomplete; it never adopts a partial worktree.
func (service *GitWorktreeService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if !validJobID(request.Job) || request.CommandOutputBytes <= 0 {
		return fix.CandidateIdentity{}, false, errors.New("discover prepared candidate: invalid job ID")
	}
	ctx = withCommandOutputBytes(ctx, request.CommandOutputBytes)
	jobRoot := filepath.Join(service.stateRoot, string(request.Job))
	if _, err := os.Lstat(jobRoot); errors.Is(err, os.ErrNotExist) {
		return fix.CandidateIdentity{}, false, nil
	} else if err != nil {
		return fix.CandidateIdentity{}, false, fmt.Errorf("inspect candidate state: %w", err)
	}
	want, err := service.expectedOwnership(request)
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	record, err := readOwnership(jobRoot)
	if errors.Is(err, os.ErrNotExist) {
		var found bool
		record, found, err = service.reconcileReservation(ctx, request, jobRoot, want)
		if err != nil {
			return fix.CandidateIdentity{}, false, err
		}
		if !found {
			return fix.CandidateIdentity{}, false, nil
		}
	} else if err != nil {
		return fix.CandidateIdentity{}, false, fmt.Errorf("read existing candidate ownership: %w", err)
	}
	if !sameOwnership(record, want) {
		return fix.CandidateIdentity{}, false, errors.New("existing candidate ownership conflicts with requested workspace or policy")
	}
	identity := record.Identity
	if err := service.Recover(ctx, identity, request.Targets, request.AllowedScope, request.AllowedPaths); err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	return identity, true, nil
}

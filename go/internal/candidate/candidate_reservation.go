package candidate

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// reconcileReservation promotes a complete, verified reservation. It returns
// found=false after removing an exact incomplete reservation so Prepare can
// create a fresh candidate in the same call.
func (service *GitWorktreeService) reconcileReservation(ctx context.Context, request PrepareRequest, jobRoot string, want ownershipRecord) (ownershipRecord, bool, error) {
	reservation, reservationErr := readCandidateRecord(jobRoot, reservationName)
	if reservationErr != nil || !sameOwnership(reservation, want) {
		return ownershipRecord{}, false, errors.New("candidate state exists without a matching durable reservation")
	}
	if _, statErr := os.Lstat(want.Identity.RepositoryRoot); errors.Is(statErr, os.ErrNotExist) {
		if err := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); err != nil {
			return ownershipRecord{}, false, fmt.Errorf("remove incomplete candidate reservation: %w", err)
		}
		return ownershipRecord{}, false, nil
	} else if statErr != nil {
		return ownershipRecord{}, false, fmt.Errorf("inspect reserved candidate worktree: %w", statErr)
	}
	if err := service.verifyReservedWorktree(ctx, want.Identity); err != nil {
		return ownershipRecord{}, false, err
	}
	manifest, manifestHash, err := readSeedManifest(jobRoot)
	if err != nil {
		return service.discardReservationAfterError(ctx, request, jobRoot, want, errors.New("candidate reservation has no complete workspace snapshot"))
	}
	completion, err := readSeedCompletion(jobRoot)
	if err != nil || completion.Version != 1 || completion.ManifestHash != manifestHash {
		return service.discardReservationAfterError(ctx, request, jobRoot, want, errors.New("candidate reservation has no durable completion marker"))
	}
	if err := verifySeedManifest(want.Identity.RepositoryRoot, manifest); err != nil {
		return service.discardReservationAfterError(ctx, request, jobRoot, want, fmt.Errorf("verify completed candidate snapshot: %w", err))
	}
	if err := writeOwnership(jobRoot, want); err != nil {
		return ownershipRecord{}, false, fmt.Errorf("promote candidate reservation: %w", err)
	}
	return want, true, nil
}

func (service *GitWorktreeService) discardReservationAfterError(ctx context.Context, request PrepareRequest, jobRoot string, want ownershipRecord, cause error) (ownershipRecord, bool, error) {
	if cleanupErr := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); cleanupErr != nil {
		return ownershipRecord{}, false, errors.Join(cause, cleanupErr)
	}
	return ownershipRecord{}, false, nil
}

package candidate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitWorktreeService) discardIncompleteReservation(ctx context.Context, repositoryRoot, jobRoot string, identity fix.CandidateIdentity) error {
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return fmt.Errorf("lock repository for incomplete candidate cleanup: %w", err)
	}
	_, cleanupErr := service.executor.bytes(ctx, repositoryRoot, "worktree", "remove", "--force", identity.RepositoryRoot)
	if cleanupErr != nil {
		listing, listErr := service.executor.bytes(ctx, repositoryRoot, "worktree", "list", "--porcelain")
		if listErr == nil && !worktreeRegistered(listing, identity.RepositoryRoot) {
			cleanupErr = nil
		} else {
			cleanupErr = errors.Join(cleanupErr, listErr)
		}
	}
	lockErr := lock.Close()
	if cleanupErr == nil && lockErr == nil {
		cleanupErr = os.RemoveAll(jobRoot)
	}
	return errors.Join(cleanupErr, lockErr)
}

func worktreeRegistered(listing []byte, expected string) bool {
	for _, line := range bytes.Split(listing, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("worktree ")) && filepath.Clean(string(bytes.TrimPrefix(line, []byte("worktree ")))) == expected {
			return true
		}
	}
	return false
}

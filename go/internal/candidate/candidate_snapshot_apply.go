package candidate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// applySeedManifest restores the frozen files into the detached worktree and
// verifies every copied file before the candidate becomes visible.
func applySeedManifest(ctx context.Context, staging, destination string, manifest seedManifest) error {
	stagingRoot, err := os.OpenRoot(staging)
	if err != nil {
		return fmt.Errorf("open candidate staging area: %w", err)
	}
	defer stagingRoot.Close()
	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open isolated worktree: %w", err)
	}
	defer destinationRoot.Close()
	for _, entry := range manifest.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Path.String()
		if entry.Deleted {
			if err := destinationRoot.RemoveAll(name); err != nil {
				return fmt.Errorf("apply deleted workspace file %s: %w", entry.Path, err)
			}
			continue
		}
		parent := filepath.Dir(name)
		if parent != "." {
			if err := destinationRoot.MkdirAll(parent, 0o700); err != nil {
				return fmt.Errorf("create isolated parent for %s: %w", entry.Path, err)
			}
		}
		if existing, statErr := destinationRoot.Lstat(name); statErr == nil && existing.Mode()&os.ModeSymlink != 0 {
			if err := destinationRoot.Remove(name); err != nil {
				return fmt.Errorf("replace isolated symlink %s: %w", entry.Path, err)
			}
		}
		input, err := stagingRoot.Open(entry.Staged)
		if err != nil {
			return fmt.Errorf("open staged workspace file %s: %w", entry.Path, err)
		}
		output, err := destinationRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(entry.Mode))
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("create isolated workspace file %s: %w", entry.Path, err)
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, entry.Size+1))
		if written != entry.Size {
			copyErr = errors.Join(copyErr, fmt.Errorf("staged size changed from %d to %d", entry.Size, written))
		}
		chmodErr := output.Chmod(os.FileMode(entry.Mode))
		syncErr := output.Sync()
		closeErr := errors.Join(input.Close(), output.Close())
		if err := errors.Join(copyErr, chmodErr, syncErr, closeErr); err != nil {
			return fmt.Errorf("apply workspace snapshot for %s: %w", entry.Path, err)
		}
	}
	if err := syncDirectory(destination); err != nil {
		return fmt.Errorf("sync isolated workspace snapshot: %w", err)
	}
	return verifySeedManifest(destination, manifest)
}

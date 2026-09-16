package delivery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

// publicationOperation owns the private index and lock for one commit saga.
// Keeping both resources together makes cleanup happen in the same scope as
// the commands that can observe them.
type publicationOperation struct {
	executor gitExecutor
	root     string
	index    string
	lock     *deliveryLock
}

func beginPublicationOperation(executor gitExecutor, root, commonDir string) (*publicationOperation, error) {
	lock, err := acquireDeliveryLock(commonDir)
	if err != nil {
		return nil, err
	}
	index, err := privateIndexPath(root)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &publicationOperation{executor: executor, root: root, index: index, lock: lock}, nil
}

func (operation *publicationOperation) Close() error {
	removeErr := os.Remove(operation.index)
	lockErr := operation.lock.Close()
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return lockErr
}

func (operation *publicationOperation) environment() []string {
	return []string{"GIT_INDEX_FILE=" + operation.index}
}

func (operation *publicationOperation) readTree(ctx context.Context, tree string) error {
	_, err := operation.executor.bytesEnv(ctx, operation.root, operation.environment(), false, "read-tree", tree)
	return err
}

func (operation *publicationOperation) stage(ctx context.Context, paths []fix.RepoPath) error {
	arguments := []string{"add", "-A", "--"}
	for _, path := range paths {
		arguments = append(arguments, path.String())
	}
	_, err := operation.executor.bytesEnv(ctx, operation.root, operation.environment(), false, arguments...)
	return err
}

func (operation *publicationOperation) tree(ctx context.Context) (string, error) {
	return operation.executor.textEnv(ctx, operation.root, operation.environment(), "write-tree")
}

func (operation *publicationOperation) commit(ctx context.Context, tree, parent, title, body string) (string, error) {
	arguments := []string{"commit-tree", tree, "-p", parent, "-m", normalizedTitle(title)}
	if body = strings.TrimSpace(body); body != "" {
		arguments = append(arguments, "-m", body)
	}
	return operation.executor.textEnv(ctx, operation.root, operation.environment(), arguments...)
}

func normalizedTitle(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	return "Refactor with Slopwatch"
}

func privateIndexPath(root string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(root), ".slopwatch-publish-index-")
	if err != nil {
		return "", fmt.Errorf("reserve private publication index: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

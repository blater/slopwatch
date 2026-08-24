package analysiscache

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SnapshotFile maps a workspace-relative path to a verified source blob.
type SnapshotFile struct {
	Path   string `json:"path"`
	Digest Digest `json:"digest"`
}

// MaterializeSnapshot builds a private, read-only temporary tree exclusively
// from checksum-verified CAS blobs. It never opens the live workspace. The
// returned cleanup function is idempotent and must be called by the owner.
func (store *Store) MaterializeSnapshot(ctx context.Context, files []SnapshotFile) (root string, cleanup func() error, err error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return "", nil, contextErr
	}
	canonical, err := canonicalSnapshotFiles(files)
	if err != nil {
		return "", nil, err
	}
	root, err = os.MkdirTemp("", "slopwatch-snapshot-*")
	if err != nil {
		return "", nil, fmt.Errorf("create analysis snapshot: %w", err)
	}
	materializedRoot := root
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup = func() error {
		cleanupOnce.Do(func() {
			cleanupErr = removeSnapshot(materializedRoot)
		})
		return cleanupErr
	}
	cleanupOnFailure := cleanup
	success := false
	defer func() {
		if !success {
			_ = cleanupOnFailure()
			root, cleanup = "", nil
		}
	}()

	for _, file := range canonical {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", nil, contextErr
		}
		contents, ok := store.LoadSource(file.Digest)
		if !ok {
			return "", nil, fmt.Errorf("snapshot source %s is not available", file.Digest)
		}
		destination := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return "", nil, fmt.Errorf("create snapshot path %s: %w", file.Path, err)
		}
		output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", nil, fmt.Errorf("create snapshot file %s: %w", file.Path, err)
		}
		if _, err := output.Write(contents); err != nil {
			output.Close()
			return "", nil, fmt.Errorf("write snapshot file %s: %w", file.Path, err)
		}
		if err := output.Sync(); err != nil {
			output.Close()
			return "", nil, fmt.Errorf("sync snapshot file %s: %w", file.Path, err)
		}
		if err := output.Close(); err != nil {
			return "", nil, fmt.Errorf("close snapshot file %s: %w", file.Path, err)
		}
		if err := os.Chmod(destination, 0o444); err != nil {
			return "", nil, fmt.Errorf("make snapshot file %s read-only: %w", file.Path, err)
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", nil, contextErr
	}
	if err := makeSnapshotDirectoriesReadOnly(root); err != nil {
		return "", nil, err
	}
	success = true
	return root, cleanup, nil
}

func canonicalSnapshotFiles(files []SnapshotFile) ([]SnapshotFile, error) {
	result := make([]SnapshotFile, len(files))
	seen := make(map[string]bool, len(files))
	for index, file := range files {
		if !validDigest(string(file.Digest)) {
			return nil, fmt.Errorf("snapshot path %q has invalid digest", file.Path)
		}
		if strings.TrimSpace(file.Path) == "" || filepath.IsAbs(file.Path) || filepath.VolumeName(file.Path) != "" {
			return nil, fmt.Errorf("snapshot path %q is not workspace-relative", file.Path)
		}
		clean := filepath.Clean(filepath.FromSlash(file.Path))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("snapshot path %q escapes its root", file.Path)
		}
		canonical := filepath.ToSlash(clean)
		if seen[canonical] {
			return nil, fmt.Errorf("duplicate snapshot path %q", canonical)
		}
		seen[canonical] = true
		result[index] = SnapshotFile{Path: canonical, Digest: file.Digest}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func makeSnapshotDirectoriesReadOnly(root string) error {
	directories := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("inspect analysis snapshot: %w", err)
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index], 0o555); err != nil {
			return fmt.Errorf("make analysis snapshot read-only: %w", err)
		}
	}
	return nil
}

func removeSnapshot(root string) error {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

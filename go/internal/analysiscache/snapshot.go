package analysiscache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// ErrWorkspaceSnapshotChanged means a live input no longer matches the
// digest captured before snapshot construction. Callers should re-plan from a
// new workspace view rather than analyze a mixed snapshot.
var ErrWorkspaceSnapshotChanged = errors.New("workspace changed while materializing analysis snapshot")

// SnapshotFile maps a workspace-relative path to a verified source blob.
type SnapshotFile struct {
	Path   string `json:"path"`
	Digest Digest `json:"digest"`
}

// MaterializeSnapshot builds a private, read-only temporary tree exclusively
// from checksum-verified CAS blobs. It never opens the live workspace. The
// returned cleanup function is idempotent and must be called by the owner.
func (store *Store) MaterializeSnapshot(ctx context.Context, files []SnapshotFile) (root string, cleanup func() error, err error) {
	canonical, err := canonicalSnapshotFiles(files)
	if err != nil {
		return "", nil, err
	}
	return materializeSnapshot(ctx, canonical, func(file SnapshotFile) ([]byte, error) {
		contents, ok := store.LoadSource(file.Digest)
		if !ok {
			return nil, fmt.Errorf("snapshot source %s is not available", file.Digest)
		}
		return contents, nil
	})
}

func materializeSnapshot(ctx context.Context, files []SnapshotFile, load func(SnapshotFile) ([]byte, error)) (root string, cleanup func() error, err error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return "", nil, contextErr
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

	directories := snapshotDirectories(root, files)
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return "", nil, fmt.Errorf("create snapshot directory: %w", err)
		}
	}
	errorsByFile := make([]error, len(files))
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var workersDone sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		workersDone.Add(1)
		go func() {
			defer workersDone.Done()
			for index := range jobs {
				file := files[index]
				if contextErr := ctx.Err(); contextErr != nil {
					errorsByFile[index] = contextErr
					continue
				}
				contents, loadErr := load(file)
				if loadErr != nil {
					errorsByFile[index] = fmt.Errorf("load snapshot source %s: %w", file.Path, loadErr)
					continue
				}
				destination := filepath.Join(root, filepath.FromSlash(file.Path))
				output, openErr := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if openErr != nil {
					errorsByFile[index] = fmt.Errorf("create snapshot file %s: %w", file.Path, openErr)
					continue
				}
				if _, writeErr := output.Write(contents); writeErr != nil {
					_ = output.Close()
					errorsByFile[index] = fmt.Errorf("write snapshot file %s: %w", file.Path, writeErr)
					continue
				}
				if closeErr := output.Close(); closeErr != nil {
					errorsByFile[index] = fmt.Errorf("close snapshot file %s: %w", file.Path, closeErr)
					continue
				}
				if chmodErr := os.Chmod(destination, 0o444); chmodErr != nil {
					errorsByFile[index] = fmt.Errorf("make snapshot file %s read-only: %w", file.Path, chmodErr)
				}
			}
		}()
	}
	for index := range files {
		jobs <- index
	}
	close(jobs)
	workersDone.Wait()
	for _, fileErr := range errorsByFile {
		if fileErr != nil {
			return "", nil, fileErr
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", nil, contextErr
	}
	if err := makeSnapshotDirectoriesReadOnly(directories); err != nil {
		return "", nil, err
	}
	success = true
	return root, cleanup, nil
}

// MaterializeWorkspaceSnapshot builds a private snapshot directly from live
// workspace files and verifies each file against its previously captured
// digest. Snapshot files are disposable and need no durable cache write; any
// interruption or mismatch is retried as a fresh workspace view.
func (store *Store) MaterializeWorkspaceSnapshot(ctx context.Context, workspace string, files []SnapshotFile) (root string, cleanup func() error, err error) {
	canonical, err := canonicalSnapshotFiles(files)
	if err != nil {
		return "", nil, err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return "", nil, fmt.Errorf("canonicalize snapshot workspace: %w", err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", nil, fmt.Errorf("resolve snapshot workspace: %w", err)
	}
	return materializeSnapshot(ctx, canonical, func(file SnapshotFile) ([]byte, error) {
		path := filepath.Join(workspace, filepath.FromSlash(file.Path))
		metadata, readErr := os.Lstat(path)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return nil, ErrWorkspaceSnapshotChanged
			}
			return nil, readErr
		}
		if metadata.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("snapshot input is symlinked: %s", file.Path)
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if DigestBytes(contents) != file.Digest {
			return nil, ErrWorkspaceSnapshotChanged
		}
		return contents, nil
	})
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

func snapshotDirectories(root string, files []SnapshotFile) []string {
	seen := map[string]bool{root: true}
	for _, file := range files {
		for directory := filepath.Dir(filepath.Join(root, filepath.FromSlash(file.Path))); directory != root; directory = filepath.Dir(directory) {
			seen[directory] = true
		}
	}
	directories := make([]string, 0, len(seen))
	for directory := range seen {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		leftDepth := strings.Count(directories[i], string(filepath.Separator))
		rightDepth := strings.Count(directories[j], string(filepath.Separator))
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return directories[i] < directories[j]
	})
	return directories
}

func makeSnapshotDirectoriesReadOnly(directories []string) error {
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

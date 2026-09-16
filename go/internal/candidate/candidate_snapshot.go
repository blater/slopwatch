package candidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

// snapshotWorkingChanges freezes the caller's selected changes before Git
// creates the detached worktree. The manifest and staged blobs are later
// applied and verified as one cohesive snapshot operation.
func (executor gitExecutor) snapshotWorkingChanges(ctx context.Context, source, staging, scope string, allowed []fix.RepoPath) (seedManifest, error) {
	status, err := executor.bytes(ctx, source, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return seedManifest{}, fmt.Errorf("list current workspace changes: %w", err)
	}
	allowedSet := make(map[fix.RepoPath]bool, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = true
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return seedManifest{}, fmt.Errorf("open source workspace: %w", err)
	}
	defer sourceRoot.Close()
	stagingRoot, err := os.OpenRoot(staging)
	if err != nil {
		return seedManifest{}, fmt.Errorf("open candidate staging area: %w", err)
	}
	defer stagingRoot.Close()
	specs := map[fix.RepoPath]seedSpec{}
	entries := bytes.Split(status, []byte{0})
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		statusCode := string(entry[:2])
		path, parseErr := fix.ParseRepoPath(string(entry[3:]))
		if parseErr != nil {
			return seedManifest{}, fmt.Errorf("current workspace contains an unsupported changed path: %w", parseErr)
		}
		var original fix.RepoPath
		if strings.ContainsAny(statusCode, "RC") && index+1 < len(entries) {
			index++
			original, parseErr = fix.ParseRepoPath(string(entries[index]))
			if parseErr != nil {
				return seedManifest{}, fmt.Errorf("current workspace contains an unsupported rename/copy source: %w", parseErr)
			}
		}
		pathAllowed := scope == "repository" || allowedSet[path]
		originalAllowed := original == "" || scope == "repository" || allowedSet[original]
		if original != "" && pathAllowed != originalAllowed {
			return seedManifest{}, fmt.Errorf("current workspace rename/copy crosses the allowed scope: %s and %s", original, path)
		}
		if !pathAllowed {
			continue
		}
		specs[path] = seedSpec{path: path}
		if original != "" {
			specs[original] = seedSpec{path: original, forceDeletion: strings.Contains(statusCode, "R")}
		}
	}
	paths := make([]fix.RepoPath, 0, len(specs))
	for path := range specs {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	manifest := seedManifest{Version: 1, Entries: make([]seedEntry, 0, len(paths))}
	for index, path := range paths {
		entry, err := snapshotWorkingFile(ctx, sourceRoot, stagingRoot, specs[path], index)
		if err != nil {
			return seedManifest{}, err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	return manifest, nil
}

func snapshotWorkingFile(ctx context.Context, source, staging *os.Root, spec seedSpec, index int) (seedEntry, error) {
	if err := ctx.Err(); err != nil {
		return seedEntry{}, err
	}
	name := spec.path.String()
	if spec.forceDeletion {
		return seedEntry{Path: spec.path, Deleted: true}, nil
	}
	info, err := source.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return seedEntry{Path: spec.path, Deleted: true}, nil
	}
	if err != nil {
		return seedEntry{}, fmt.Errorf("inspect changed workspace file %s: %w", spec.path, err)
	}
	if !info.Mode().IsRegular() {
		return seedEntry{}, fmt.Errorf("changed workspace path %s is not a regular file", spec.path)
	}
	input, err := source.Open(name)
	if err != nil {
		return seedEntry{}, fmt.Errorf("open changed workspace file %s: %w", spec.path, err)
	}
	defer input.Close()
	staged := fmt.Sprintf("blob-%06d", index)
	output, err := staging.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return seedEntry{}, fmt.Errorf("stage changed workspace file %s: %w", spec.path, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return seedEntry{}, fmt.Errorf("snapshot changed workspace file %s: %w", spec.path, errors.Join(copyErr, syncErr, closeErr))
	}
	return seedEntry{Path: spec.path, Staged: staged, Mode: uint32(info.Mode().Perm()), Size: written, Hash: hex.EncodeToString(hash.Sum(nil))}, ctx.Err()
}

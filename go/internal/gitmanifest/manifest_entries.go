package gitmanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

func inspectEntries(worktree string, parsed []statusEntry) ([]Entry, error) {
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return nil, fmt.Errorf("open manifest root: %w", err)
	}
	defer root.Close()
	entries := make([]Entry, 0, len(parsed))
	for _, item := range parsed {
		entry, err := inspectEntry(root, item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		if entries[i].Previous != entries[j].Previous {
			return entries[i].Previous < entries[j].Previous
		}
		return entries[i].Status < entries[j].Status
	})
	return entries, nil
}

func inspectEntry(root *os.Root, item statusEntry) (Entry, error) {
	path, err := fix.ParseRepoPath(item.path)
	if err != nil {
		return Entry{}, fmt.Errorf("unsafe Git status path %q: %w", item.path, err)
	}
	entry := Entry{Status: item.status, Path: path, Kind: "deleted"}
	if item.previous != "" {
		previous, err := fix.ParseRepoPath(item.previous)
		if err != nil {
			return Entry{}, fmt.Errorf("unsafe Git rename source %q: %w", item.previous, err)
		}
		entry.Previous = previous
	}
	if isDeleted(item.status) {
		return entry, nil
	}
	return inspectLiveEntry(root, item.path, entry)
}

func inspectLiveEntry(root *os.Root, name string, entry Entry) (Entry, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return Entry{}, fmt.Errorf("inspect changed path %q: %w", name, err)
	}
	switch mode := info.Mode(); {
	case mode.IsRegular():
		entry.Kind = "regular"
		entry.Mode = 0o100644
		if mode.Perm()&0o111 != 0 {
			entry.Mode = 0o100755
		}
		file, err := root.Open(name)
		if err != nil {
			return Entry{}, fmt.Errorf("open changed path %q: %w", name, err)
		}
		hasher := sha256.New()
		_, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return Entry{}, fmt.Errorf("hash changed path %q: %w", name, errors.Join(copyErr, closeErr))
		}
		entry.Hash = hex.EncodeToString(hasher.Sum(nil))
	case mode&os.ModeSymlink != 0:
		entry.Kind = "symlink"
		entry.Mode = 0o120000
		target, err := root.Readlink(name)
		if err != nil {
			return Entry{}, fmt.Errorf("read changed symlink %q: %w", name, err)
		}
		hash := sha256.Sum256([]byte(target))
		entry.Hash = hex.EncodeToString(hash[:])
	default:
		return Entry{}, fmt.Errorf("changed path %q is an unsupported special file", name)
	}
	return entry, nil
}

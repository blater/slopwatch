// Package gitmanifest creates a canonical, content-complete inventory of a
// Git porcelain status. It deliberately does not inspect Git metadata: callers
// obtain status through their trusted Git runner and supply only the worktree.
package gitmanifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/blater/slopmochi/internal/fix"
)

type Entry struct {
	Status   string
	Path     fix.RepoPath
	Previous fix.RepoPath
	Mode     uint32
	Kind     string
	Hash     string
}

type Manifest struct {
	Entries     []Entry
	Fingerprint string
}

// Build parses `git status --porcelain=v1 -z`. In -z format Git emits a
// rename's destination first and source second. Both are security-relevant.
func Build(worktree string, status []byte) (Manifest, error) {
	parsed, err := parse(status)
	if err != nil {
		return Manifest{}, err
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest root: %w", err)
	}
	defer root.Close()

	entries := make([]Entry, 0, len(parsed))
	for _, item := range parsed {
		path, err := fix.ParseRepoPath(item.path)
		if err != nil {
			return Manifest{}, fmt.Errorf("unsafe Git status path %q: %w", item.path, err)
		}
		entry := Entry{Status: item.status, Path: path, Kind: "deleted"}
		if item.previous != "" {
			previous, err := fix.ParseRepoPath(item.previous)
			if err != nil {
				return Manifest{}, fmt.Errorf("unsafe Git rename source %q: %w", item.previous, err)
			}
			entry.Previous = previous
		}
		if !isDeleted(item.status) {
			info, err := root.Lstat(item.path)
			if err != nil {
				return Manifest{}, fmt.Errorf("inspect changed path %q: %w", item.path, err)
			}
			switch mode := info.Mode(); {
			case mode.IsRegular():
				entry.Kind = "regular"
				entry.Mode = 0o100644
				if mode.Perm()&0o111 != 0 {
					entry.Mode = 0o100755
				}
				file, err := root.Open(item.path)
				if err != nil {
					return Manifest{}, fmt.Errorf("open changed path %q: %w", item.path, err)
				}
				hasher := sha256.New()
				_, copyErr := io.Copy(hasher, file)
				closeErr := file.Close()
				if copyErr != nil || closeErr != nil {
					return Manifest{}, fmt.Errorf("hash changed path %q: %w", item.path, errors.Join(copyErr, closeErr))
				}
				entry.Hash = hex.EncodeToString(hasher.Sum(nil))
			case mode&os.ModeSymlink != 0:
				entry.Kind = "symlink"
				entry.Mode = 0o120000
				target, err := root.Readlink(item.path)
				if err != nil {
					return Manifest{}, fmt.Errorf("read changed symlink %q: %w", item.path, err)
				}
				hash := sha256.Sum256([]byte(target))
				entry.Hash = hex.EncodeToString(hash[:])
			default:
				return Manifest{}, fmt.Errorf("changed path %q is an unsupported special file", item.path)
			}
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
	hasher := sha256.New()
	for _, entry := range entries {
		writeField(hasher, entry.Status)
		writeField(hasher, entry.Path.String())
		writeField(hasher, entry.Previous.String())
		writeField(hasher, entry.Kind)
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], entry.Mode)
		_, _ = hasher.Write(mode[:])
		writeField(hasher, entry.Hash)
	}
	return Manifest{Entries: entries, Fingerprint: hex.EncodeToString(hasher.Sum(nil))}, nil
}

type statusEntry struct{ status, path, previous string }

func parse(data []byte) ([]statusEntry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, errors.New("Git porcelain status is not NUL terminated")
	}
	parts := bytes.Split(data, []byte{0})
	result := make([]statusEntry, 0, len(parts)-1)
	for index := 0; index < len(parts)-1; index++ {
		part := parts[index]
		if len(part) < 4 || part[2] != ' ' || part[0] == ' ' && part[1] == ' ' {
			return nil, errors.New("malformed Git porcelain status")
		}
		item := statusEntry{status: string(part[:2]), path: string(part[3:])}
		if item.path == "" {
			return nil, errors.New("empty Git status path")
		}
		if isRename(item.status) {
			index++
			if index >= len(parts)-1 || len(parts[index]) == 0 {
				return nil, errors.New("malformed Git rename status")
			}
			item.previous = string(parts[index])
		}
		result = append(result, item)
	}
	return result, nil
}

func isRename(status string) bool {
	return len(status) == 2 && (status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C')
}

func isDeleted(status string) bool {
	return len(status) == 2 && (status[0] == 'D' || status[1] == 'D')
}

func writeField(writer io.Writer, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = io.WriteString(writer, value)
}

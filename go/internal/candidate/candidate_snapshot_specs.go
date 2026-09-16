package candidate

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

// snapshotChangeSpecs decodes Git's NUL-delimited status records and applies
// the candidate scope before any source files are opened. Keeping status
// interpretation here makes rename/copy pairing and scope checks one
// operation, while snapshotWorkingChanges owns the filesystem snapshot.
func snapshotChangeSpecs(status []byte, scope string, allowed []fix.RepoPath) (map[fix.RepoPath]seedSpec, error) {
	allowedSet := make(map[fix.RepoPath]bool, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = true
	}
	specs := map[fix.RepoPath]seedSpec{}
	entries := bytes.Split(status, []byte{0})
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		statusCode := string(entry[:2])
		path, err := fix.ParseRepoPath(string(entry[3:]))
		if err != nil {
			return nil, fmt.Errorf("current workspace contains an unsupported changed path: %w", err)
		}
		original, next, err := snapshotOriginal(entries, index, statusCode)
		if err != nil {
			return nil, err
		}
		index = next
		if err := addSnapshotSpec(specs, allowedSet, scope, statusCode, path, original); err != nil {
			return nil, err
		}
	}
	return specs, nil
}

func snapshotOriginal(entries [][]byte, index int, statusCode string) (fix.RepoPath, int, error) {
	if !strings.ContainsAny(statusCode, "RC") || index+1 >= len(entries) {
		return "", index, nil
	}
	original, err := fix.ParseRepoPath(string(entries[index+1]))
	if err != nil {
		return "", index, fmt.Errorf("current workspace contains an unsupported rename/copy source: %w", err)
	}
	return original, index + 1, nil
}

func addSnapshotSpec(specs map[fix.RepoPath]seedSpec, allowed map[fix.RepoPath]bool, scope, statusCode string, path, original fix.RepoPath) error {
	pathAllowed := scope == "repository" || allowed[path]
	originalAllowed := original == "" || scope == "repository" || allowed[original]
	if original != "" && pathAllowed != originalAllowed {
		return fmt.Errorf("current workspace rename/copy crosses the allowed scope: %s and %s", original, path)
	}
	if !pathAllowed {
		return nil
	}
	specs[path] = seedSpec{path: path}
	if original != "" {
		specs[original] = seedSpec{path: original, forceDeletion: strings.Contains(statusCode, "R")}
	}
	return nil
}

// Package gitmanifest creates a canonical, content-complete inventory of a
// Git porcelain status. It deliberately does not inspect Git metadata: callers
// obtain status through their trusted Git runner and supply only the worktree.
package gitmanifest

import "github.com/blater/slopwatch/internal/fix"

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
	entries, err := inspectEntries(worktree, parsed)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{Entries: entries, Fingerprint: fingerprint(entries)}, nil
}

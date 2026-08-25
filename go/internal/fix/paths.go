package fix

import (
	"errors"
	"path"
	"strings"
)

var ErrInvalidRepoPath = errors.New("invalid repository path")

// RepoPath is a slash-separated path relative to the canonical Git root.
// Construction rejects traversal and platform-dependent separators so the
// value can safely cross package and process boundaries.
type RepoPath string

func ParseRepoPath(value string) (RepoPath, error) {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || path.IsAbs(value) {
		return "", ErrInvalidRepoPath
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidRepoPath
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", ErrInvalidRepoPath
		}
	}
	return RepoPath(cleaned), nil
}

func (value RepoPath) String() string { return string(value) }

type WorkspaceIdentity struct {
	Repository     RepositoryID
	RepositoryRoot string
	AnalysisRoot   string
	GitCommonDir   string
	BaseCommit     ObjectID
}

type CandidateIdentity struct {
	Job            JobID
	Repository     RepositoryID
	RepositoryRoot string
	AnalysisRoot   string
	GitCommonDir   string
	BaseCommit     ObjectID
	// StagingRoot is candidate-service-owned, private, and on the same
	// filesystem as RepositoryRoot. Adapters may use it for crash-safe atomic
	// staging; it is never part of the candidate diff inventory.
	StagingRoot string
}

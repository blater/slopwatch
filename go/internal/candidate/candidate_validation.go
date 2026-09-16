package candidate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitWorktreeService) validateIdentity(ctx context.Context, identity fix.CandidateIdentity) (candidatePolicy, error) {
	if !validJobID(identity.Job) {
		return candidatePolicy{}, errors.New("candidate identity has an invalid job ID")
	}
	expected := filepath.Join(service.stateRoot, string(identity.Job), "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return candidatePolicy{}, errors.New("candidate identity does not name its exact managed worktree")
	}
	record, err := readOwnership(filepath.Dir(expected))
	if err != nil {
		return candidatePolicy{}, fmt.Errorf("read candidate ownership: %w", err)
	}
	if record.Version != 1 || record.Identity != identity {
		return candidatePolicy{}, errors.New("candidate identity does not match its ownership record")
	}
	if record.CommandOutputBytes <= 0 {
		return candidatePolicy{}, errors.New("candidate ownership lacks a command output budget")
	}
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
	leasedCommon, leased := service.registry.leaseFor(identity.Job)
	if !leased || leasedCommon != identity.GitCommonDir {
		return candidatePolicy{}, errors.New("candidate repository ownership lease is not held")
	}
	common, err := service.executor.text(ctx, expected, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return candidatePolicy{}, fmt.Errorf("verify candidate Git directory: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil || common != identity.GitCommonDir {
		return candidatePolicy{}, errors.New("candidate Git common directory does not match ownership")
	}
	head, err := service.executor.text(ctx, expected, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != identity.BaseCommit {
		return candidatePolicy{}, errors.New("candidate HEAD moved from its owned base commit")
	}
	policy := candidatePolicy{scope: record.Scope, allowed: make(map[fix.RepoPath]bool, len(record.Allowed)), commandOutputBytes: record.CommandOutputBytes}
	for _, target := range record.Allowed {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return candidatePolicy{}, errors.New("candidate ownership contains an invalid target")
		}
		policy.allowed[target] = true
	}
	service.registry.setPolicy(identity.Job, policy)
	return policy, nil
}

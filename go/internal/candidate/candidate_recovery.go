package candidate

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

// Recover re-establishes an owned candidate after process restart. Durable
// marker policy and saved policy must agree; neither source is trusted alone.
func (service *GitWorktreeService) Recover(ctx context.Context, identity fix.CandidateIdentity, targets []fix.RepoPath, scope string, allowed []fix.RepoPath) error {
	if !validJobID(identity.Job) {
		return errors.New("candidate identity has an invalid job ID")
	}
	expected := filepath.Join(service.stateRoot, string(identity.Job), "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return errors.New("candidate identity does not name its exact managed worktree")
	}
	record, err := readOwnership(filepath.Dir(expected))
	if err != nil || record.Version != 1 || record.Identity != identity {
		return errors.New("candidate identity does not match its ownership record")
	}
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
	wantTargets := append([]fix.RepoPath(nil), targets...)
	sort.Slice(wantTargets, func(i, j int) bool { return wantTargets[i] < wantTargets[j] })
	if len(record.Targets) != len(wantTargets) {
		return errors.New("saved candidate targets do not match its ownership record")
	}
	for index := range wantTargets {
		if record.Targets[index] != wantTargets[index] {
			return errors.New("saved candidate targets do not match its ownership record")
		}
	}
	if err := service.registry.retain(record.Identity.GitCommonDir, identity.Job); err != nil {
		return err
	}
	recovered := false
	defer func() {
		if !recovered {
			_ = service.registry.release(record.Identity.GitCommonDir, identity.Job)
		}
	}()
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return err
	}
	if scope != "repository" && len(allowed) == 0 {
		allowed = wantTargets
	}
	want := candidatePolicy{scope: scope, allowed: make(map[fix.RepoPath]bool, len(allowed)), commandOutputBytes: record.CommandOutputBytes}
	for _, target := range allowed {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return err
		}
		want.allowed[target] = true
	}
	if policy.scope != want.scope || len(policy.allowed) != len(want.allowed) {
		return errors.New("saved candidate policy does not match its ownership record")
	}
	for target := range want.allowed {
		if !policy.allowed[target] {
			return errors.New("saved candidate targets do not match its ownership record")
		}
	}
	recovered = true
	return nil
}

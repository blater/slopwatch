package follow

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

type fixTargetSelection struct {
	Marked         []string
	Selected       string
	Workspace      string
	RepositoryRoot string
}

func selectedFixTargets(input fixTargetSelection) ([]fix.RepoPath, error) {
	paths := input.Marked
	if len(paths) == 0 {
		paths = []string{input.Selected}
	}
	targets := make([]fix.RepoPath, 0, len(paths))
	for _, path := range paths {
		target, err := selectedRepoPath(path, input.Workspace, input.RepositoryRoot)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func selectedRepoPath(value, workspace, repositoryRoot string) (fix.RepoPath, error) {
	if repositoryRoot == "" {
		return fix.ParseRepoPath(filepath.ToSlash(value))
	}
	absolute := value
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(workspace, value)
	}
	absolute, err := filepath.Abs(absolute)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(repositoryRoot, absolute)
	if err != nil {
		return "", err
	}
	return fix.ParseRepoPath(filepath.ToSlash(relative))
}

func existingFixForTarget(jobs []fix.JobPresentation, path fix.RepoPath) (fix.JobPresentation, bool) {
	candidates := make([]fix.JobPresentation, 0, 2)
	for _, job := range jobs {
		if eligibleExistingFix(job) && jobTargetsPath(job, path) {
			candidates = append(candidates, job)
		}
	}
	if len(candidates) == 0 {
		return fix.JobPresentation{}, false
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		leftPriority, rightPriority := agentJobPriority(candidates[left]), agentJobPriority(candidates[right])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return candidates[left].UpdatedAt.After(candidates[right].UpdatedAt)
	})
	return candidates[0], true
}

func eligibleExistingFix(job fix.JobPresentation) bool {
	if job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseCanceled || job.Phase == fix.PhaseDiscarded {
		return false
	}
	return job.Issue == nil || job.Issue.Code != "canceled" || job.Phase != fix.PhaseCanceling && job.Phase != fix.PhaseDiscarding
}

func jobTargetsPath(job fix.JobPresentation, path fix.RepoPath) bool {
	for _, target := range job.Targets {
		if target.Path == path {
			return true
		}
	}
	return false
}

func fixMarkerForPath(jobs []fix.JobPresentation, path fix.RepoPath) string {
	selected, found := selectJobForPath(jobs, path)
	if !found {
		return ""
	}
	if selected.Phase == fix.PhaseFailed && selected.Issue != nil && strings.EqualFold(selected.Issue.Code, "canceled") {
		return "×"
	}
	return fixPhaseMarker(selected)
}

func selectJobForPath(jobs []fix.JobPresentation, path fix.RepoPath) (fix.JobPresentation, bool) {
	var selected fix.JobPresentation
	found := false
	for _, job := range jobs {
		if !jobTargetsPath(job, path) {
			continue
		}
		if !found || agentJobPriority(job) < agentJobPriority(selected) || agentJobPriority(job) == agentJobPriority(selected) && job.UpdatedAt.After(selected.UpdatedAt) {
			selected, found = job, true
		}
	}
	return selected, found
}

func fixPhaseMarker(job fix.JobPresentation) string {
	switch job.Phase {
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing:
		return "…"
	case fix.PhaseRunning:
		return "▶"
	case fix.PhaseWaitingVerifier:
		return "◷"
	case fix.PhaseVerifying:
		return "◆"
	case fix.PhaseFailed:
		return "!"
	case fix.PhasePublishing:
		return "↑"
	case fix.PhaseCanceling, fix.PhaseCanceled, fix.PhaseDiscarding, fix.PhaseDiscarded:
		return "×"
	case fix.PhaseReconciling:
		return "↻"
	case fix.PhaseCompleted:
		return "✓"
	default:
		return ""
	}
}

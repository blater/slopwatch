package follow

import (
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func filterAgentJobs(jobs []fix.JobPresentation, query string, showAll bool, sortKey string, reverse bool) []fix.JobPresentation {
	query = strings.ToLower(strings.TrimSpace(query))
	visible := make([]fix.JobPresentation, 0, len(jobs))
	for _, job := range jobs {
		if job.Phase == fix.PhaseDiscarded || !showAll && (job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseCanceled) {
			continue
		}
		if query != "" && !agentJobMatches(job, query) {
			continue
		}
		visible = append(visible, job)
	}
	sort.SliceStable(visible, func(left, right int) bool {
		return agentJobLess(sortKey, reverse, visible[left], visible[right])
	})
	return visible
}

func agentJobLess(sortKey string, reverse bool, left, right fix.JobPresentation) bool {
	if sortKey == "" {
		sortKey = "attention"
	}
	less, equal := agentSortComparison(sortKey, left, right)
	if equal {
		less = left.ID < right.ID
	}
	if reverse && !equal {
		return !less
	}
	return less
}

func agentSortComparison(key string, left, right fix.JobPresentation) (bool, bool) {
	switch key {
	case "state":
		return compareAgentText(agentPhaseText(left), agentPhaseText(right))
	case "agent":
		return compareAgentText(left.ProfileLabel, right.ProfileLabel)
	case "goal":
		return compareAgentText(left.Goal, right.Goal)
	case "target":
		leftTarget, rightTarget := firstAgentTarget(left), firstAgentTarget(right)
		return leftTarget < rightTarget, leftTarget == rightTarget
	case "time":
		return left.UpdatedAt.Before(right.UpdatedAt), left.UpdatedAt.Equal(right.UpdatedAt)
	case "activity":
		return compareAgentText(left.CurrentAction, right.CurrentAction)
	default:
		leftPriority, rightPriority := agentJobPriority(left), agentJobPriority(right)
		return leftPriority < rightPriority, leftPriority == rightPriority
	}
}

func compareAgentText(left, right string) (bool, bool) {
	return strings.ToLower(left) < strings.ToLower(right), strings.EqualFold(left, right)
}

func firstAgentTarget(job fix.JobPresentation) string {
	if len(job.Targets) == 0 {
		return ""
	}
	return strings.ToLower(job.Targets[0].Path.String())
}

func projectAgentRows(jobs []fix.JobPresentation, expanded map[fix.JobID]bool, query string) []agentLogicalRow {
	query = strings.ToLower(strings.TrimSpace(query))
	rows := make([]agentLogicalRow, 0, len(jobs)*2)
	for _, job := range jobs {
		rows = append(rows, agentLogicalRow{ID: AgentRowID{JobID: job.ID}, Job: job})
		matchingFiles := query != "" && agentTargetMatches(job, query)
		if !expanded[job.ID] && !matchingFiles {
			continue
		}
		for index := range job.Targets {
			file := job.Targets[index]
			if query != "" && matchingFiles && !strings.Contains(strings.ToLower(file.Path.String()), query) {
				continue
			}
			rows = append(rows, agentLogicalRow{ID: AgentRowID{JobID: job.ID, Path: file.Path}, Job: job, File: &file})
		}
	}
	return rows
}

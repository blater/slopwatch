package fixapp

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (state *controllerState) jobList(filter JobFilter) JobListSnapshot {
	presentations := make([]fix.JobPresentation, 0, len(state.jobs))
	for _, id := range state.order {
		record := state.jobs[id]
		if record == nil || !includeJob(record.presentation.Phase, filter) {
			continue
		}
		presentations = append(presentations, clonePresentation(record.presentation))
	}
	sortPresentations(presentations)
	return JobListSnapshot{Jobs: presentations}
}

func includeJob(phase fix.Phase, filter JobFilter) bool {
	if !filter.IncludeFinished && phase == fix.PhaseCompleted {
		return false
	}
	return !filter.ActiveOnly || !isQuiescent(phase)
}

func (manager *controller) transcriptPage(id fix.JobID, cursor LogCursor, limit int) transcriptResponse {
	state := manager.state
	record := state.jobs[id]
	if record == nil {
		return transcriptResponse{err: ErrJobNotFound}
	}
	limit = boundedLogLimit(limit)
	if manager.options.JobIndexPath != "" {
		page, err := manager.logging.transcriptFromFile(id, cursor, limit)
		if err == nil {
			return transcriptResponse{page: page}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return transcriptResponse{err: err}
		}
	}
	start := max(0, min(int(cursor), len(record.logs)))
	end := min(len(record.logs), start+limit)
	return transcriptResponse{page: LogPage{Entries: append([]LogEntry(nil), record.logs[start:end]...), Next: LogCursor(end), Complete: end == len(record.logs)}}
}

func boundedLogLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func (manager *loggingOwner) transcriptFromFile(id fix.JobID, cursor LogCursor, limit int) (LogPage, error) {
	contents, err := os.ReadFile(manager.jobTextLogPath(id))
	if err != nil {
		return LogPage{}, err
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	start := max(0, min(int(cursor), len(lines)))
	end := min(len(lines), start+limit)
	entries := make([]LogEntry, 0, end-start)
	for _, line := range lines[start:end] {
		entries = append(entries, LogEntry{Text: line})
	}
	return LogPage{Entries: entries, Next: LogCursor(end), Complete: end == len(lines)}, nil
}

func reservationKeys(input FixInput) []string {
	if input.DeliveryPlan.Workspace == fix.WorkspaceCurrent {
		return []string{reservationRoot(input.Workspace) + "\x00*"}
	}
	keys := make([]string, 0, len(input.Targets))
	for _, path := range input.Targets {
		keys = append(keys, reservationRoot(input.Workspace)+"\x00"+path.String())
	}
	sort.Strings(keys)
	return keys
}

func reservationKey(workspace fix.WorkspaceIdentity, target fix.RepoPath) string {
	return reservationRoot(workspace) + "\x00" + target.String()
}

func reservationRoot(workspace fix.WorkspaceIdentity) string {
	if workspace.Repository != "" {
		return string(workspace.Repository)
	}
	return workspace.RepositoryRoot
}

func baselineTargets(contract fix.ScoringContract) []fix.FilePresentation {
	result := make([]fix.FilePresentation, 0, len(contract.Targets))
	for _, target := range contract.Targets {
		metrics := metricValues(target.Metrics)
		result = append(result, fix.FilePresentation{Path: target.Path, Classification: "target", BaselineScore: target.Score, BaselineScoreUnavailable: !target.Complete && len(target.Metrics) == 0, AfterScoreUnmeasured: true, BaselineMetrics: metrics, Metrics: metrics})
	}
	return result
}

func metricValues(values map[fix.MetricID]fix.MetricValue) []fix.MetricValue {
	result := make([]fix.MetricValue, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func goalLabel(input FixInput) string { return fmt.Sprintf("score ≤ %.1f", input.TargetScore) }

func allowedActions(value fix.JobPresentation) []fix.JobAction {
	switch value.Phase {
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing, fix.PhaseRunning, fix.PhaseWaitingVerifier, fix.PhaseVerifying, fix.PhasePublishing, fix.PhaseReconciling:
		return []fix.JobAction{fix.ActionCancel}
	case fix.PhaseFailed:
		return []fix.JobAction{fix.ActionCancel}
	}
	return nil
}

func (record *jobRecord) refreshActions() {
	actions := allowedActions(record.presentation)
	if record.presentation.Phase == fix.PhaseCanceled && record.candidate != nil {
		actions = append(actions, fix.ActionCancel)
	}
	record.presentation.AllowedActions = actions
}

func hasAction(values []fix.JobAction, action fix.JobAction) bool {
	for _, value := range values {
		if value == action {
			return true
		}
	}
	return false
}

func clonePresentation(value fix.JobPresentation) fix.JobPresentation {
	result := value
	result.Targets = make([]fix.FilePresentation, len(value.Targets))
	for index, target := range value.Targets {
		result.Targets[index] = target
		result.Targets[index].Metrics = append([]fix.MetricValue(nil), target.Metrics...)
		result.Targets[index].BaselineMetrics = append([]fix.MetricValue(nil), target.BaselineMetrics...)
		result.Targets[index].VerifiedMetrics = append([]fix.MetricValue(nil), target.VerifiedMetrics...)
		if target.VerifiedScore != nil {
			score := *target.VerifiedScore
			result.Targets[index].VerifiedScore = &score
		}
	}
	result.AllowedActions = append([]fix.JobAction(nil), value.AllowedActions...)
	result.Actors = append([]fix.ActorPresentation(nil), value.Actors...)
	if value.Issue != nil {
		issue := *value.Issue
		result.Issue = &issue
	}
	return result
}

func nonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "agent did not complete"
}

func sanitizeSummary(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	if len(value) > 500 {
		value = value[:500] + "…"
	}
	return value
}

package fixapp

import (
	"context"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

func sortedDiffPaths(values map[fix.RepoPath]bool) []fix.RepoPath {
	result := make([]fix.RepoPath, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (manager *controller) failRecord(record *jobRecord, code string, err error) {
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionError
	record.presentation.CurrentAction = "Failed"
	record.presentation.Issue = &fix.JobIssue{Code: code, Summary: sanitizeSummary(err.Error())}
	manager.state.releaseReservations(record)
	manager.bump(record)
}

func (manager *controller) bump(record *jobRecord) bool {
	record.presentation.UpdatedAt = manager.options.Clock()
	record.refreshActions()
	if err := manager.persistence.saveRecord(context.Background(), record); err != nil {
		if record.cancel != nil {
			record.cancel()
		}
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionBlocking
		record.presentation.Issue = &fix.JobIssue{Code: "state_save_failed", Summary: "Job state could not be saved", Detail: err.Error()}
		manager.changed()
		return false
	}
	manager.changed()
	return true
}

func (manager *controller) changedRecord(record *jobRecord) {
	record.presentation.UpdatedAt = manager.options.Clock()
	record.refreshActions()
	if !record.runsElsewhere && manager.deps.Store != nil {
		_ = manager.persistence.saveRecord(context.Background(), record)
	}
	manager.changed()
}

func (manager *controller) changed() {
	for _, record := range manager.state.jobs {
		manager.logging.logJobResult(record)
		if isQuiescent(record.presentation.Phase) && record.runLock != nil {
			_ = record.runLock.Close()
			record.runLock = nil
		}
	}
	manager.notifyMu.Lock()
	close(manager.notify)
	manager.notify = make(chan struct{})
	manager.notifyMu.Unlock()
}

func (state *controllerState) releaseReservations(record *jobRecord) {
	for _, key := range reservationKeys(record.input) {
		if state.reservations[key] == record.presentation.ID {
			delete(state.reservations, key)
		}
	}
}

func (state *controllerState) reserveRecord(record *jobRecord) {
	for _, key := range reservationKeys(record.input) {
		state.reservations[key] = record.presentation.ID
	}
}

func (state *controllerState) rebuildReservations() {
	state.reservations = map[string]fix.JobID{}
	for _, record := range state.jobs {
		if !isQuiescent(record.presentation.Phase) {
			state.reserveRecord(record)
		}
	}
}

func (state *controllerState) reservedOwner(input FixInput) fix.JobID {
	for _, key := range reservationKeys(input) {
		if owner := state.reservations[key]; owner != "" {
			return owner
		}
	}
	return ""
}

func (state *controllerState) firstInPhase(phase fix.Phase) *jobRecord {
	for _, id := range state.order {
		if record := state.jobs[id]; record != nil && !record.runsElsewhere && record.presentation.Phase == phase {
			return record
		}
	}
	return nil
}

func (state *controllerState) finishRestoredRecord(record *jobRecord) {
	if !isQuiescent(record.presentation.Phase) {
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionError
		record.presentation.CurrentAction = "Interrupted by previous shutdown"
		record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job stopped during the previous shutdown"}
	}
	record.refreshActions()
	if recoveryDisablesActions(record.presentation.Issue) {
		record.presentation.AllowedActions = nil
	}
	if !isQuiescent(record.presentation.Phase) {
		state.reserveRecord(record)
	}
}

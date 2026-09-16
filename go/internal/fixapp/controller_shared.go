package fixapp

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func (manager *controller) refreshSharedJobs() {
	state := manager.state
	storedJobs, err := manager.deps.Store.Load(context.Background())
	if err != nil {
		return
	}
	changed := false
	for _, stored := range storedJobs {
		fresh, ok := jobRecordFromStored(stored)
		if !ok || !state.shouldRefresh(fresh, stored) {
			continue
		}
		fresh, ok = manager.claimSharedJob(fresh)
		if !ok {
			continue
		}
		state.replaceSharedJob(fresh)
		changed = true
	}
	if !changed {
		return
	}
	state.rebuildReservations()
	manager.changed()
}

func (state *controllerState) shouldRefresh(fresh *jobRecord, stored jobstore.Record) bool {
	current := state.jobs[fresh.presentation.ID]
	if current != nil && current.runsHere {
		return false
	}
	return current == nil || stored.UpdatedAt.After(current.storedAt) || current.runsElsewhere && !isQuiescent(current.presentation.Phase)
}

func (manager *controller) claimSharedJob(record *jobRecord) (*jobRecord, bool) {
	if isQuiescent(record.presentation.Phase) {
		return record, true
	}
	runLock, err := manager.deps.Store.Lock(record.presentation.ID)
	if errors.Is(err, jobstore.ErrJobRunning) {
		record.runsElsewhere = true
		return record, true
	}
	if err != nil {
		return nil, false
	}
	latest, found := manager.latestSharedJob(record.presentation.ID)
	if found {
		record = latest
	}
	if isQuiescent(record.presentation.Phase) {
		_ = runLock.Close()
		return record, true
	}
	record.markSharedInterrupted(runLock, manager.options.Clock)
	_ = manager.persistence.saveRecord(context.Background(), record)
	return record, true
}

func (record *jobRecord) markSharedInterrupted(runLock jobstore.Lock, now func() time.Time) {
	record.runLock, record.runsHere = runLock, true
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionError
	record.presentation.CurrentAction = "Interrupted"
	record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "The Slopwatch process running this job stopped"}
	record.presentation.FinishedAt = now()
	record.presentation.UpdatedAt = record.presentation.FinishedAt
	record.refreshActions()
}

func (state *controllerState) replaceSharedJob(record *jobRecord) {
	if _, exists := state.jobs[record.presentation.ID]; !exists {
		state.order = append(state.order, record.presentation.ID)
	}
	state.jobs[record.presentation.ID] = record
}

func (manager *controller) latestSharedJob(id fix.JobID) (*jobRecord, bool) {
	storedJobs, err := manager.deps.Store.Load(context.Background())
	if err != nil {
		return nil, false
	}
	for _, stored := range storedJobs {
		fresh, ok := jobRecordFromStored(stored)
		if ok && fresh.presentation.ID == id {
			return fresh, true
		}
	}
	return nil, false
}

package fixapp

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

type recoveryFlags struct {
	resumeDelivery bool
	cancelCleanup  bool
}

type recoveryOwner struct {
	store       jobstore.Store
	candidates  candidate.Service
	persistence *persistenceOwner
	logging     *loggingOwner
	clock       func() time.Time
}

func (owner *recoveryOwner) restore(state *controllerState, initial []jobstore.Record) []*jobRecord {
	owner.restoreRecords(state, initial)
	var resumeDelivery []*jobRecord
	for _, record := range state.jobs {
		flags, continueRecord := owner.prepareRestoredRecord(state, record)
		if continueRecord {
			continue
		}
		if flags.cancelCleanup {
			owner.restoreCanceled(record)
			continue
		}
		if owner.restoreCleanup(record) {
			continue
		}
		owner.restoreCandidate(record)
		state.finishRestoredRecord(record)
		if flags.resumeDelivery && canResumeDelivery(record) {
			record.presentation.Attention = fix.AttentionNone
			record.presentation.Issue = nil
			resumeDelivery = append(resumeDelivery, record)
		}
	}
	for _, record := range state.jobs {
		owner.logging.logJobResult(record)
	}
	return resumeDelivery
}

func (owner *recoveryOwner) restoreRecords(state *controllerState, initial []jobstore.Record) {
	for _, stored := range initial {
		record, ok := jobRecordFromStored(stored)
		if !ok {
			continue
		}
		id := record.presentation.ID
		state.jobs[id], state.order = record, append(state.order, id)
	}
}

func (owner *recoveryOwner) prepareRestoredRecord(state *controllerState, record *jobRecord) (recoveryFlags, bool) {
	if !owner.claimRestoredJob(state, record) {
		return recoveryFlags{}, true
	}
	flags := recoveryFlags{
		resumeDelivery: record.presentation.Phase == fix.PhasePublishing || record.presentation.Phase == fix.PhaseReconciling ||
			(record.presentation.Phase == fix.PhaseFailed && (record.delivery.Ambiguous || record.published.Ambiguous)),
		cancelCleanup: canceledNeedsCleanup(record),
	}
	record.markCanceledPublication(&flags)
	if record.candidate == nil && record.presentation.Phase != fix.PhaseDiscarded {
		owner.discoverRestoredCandidate(record, flags.cancelCleanup)
	}
	return flags, false
}

func (owner *recoveryOwner) claimRestoredJob(state *controllerState, record *jobRecord) bool {
	if isQuiescent(record.presentation.Phase) {
		return true
	}
	runLock, err := owner.store.Lock(record.presentation.ID)
	if errors.Is(err, jobstore.ErrJobRunning) {
		record.runsElsewhere = true
		state.reserveRecord(record)
		return false
	}
	if err != nil {
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionError
		record.presentation.CurrentAction = "Could not check running job"
		record.presentation.Issue = &fix.JobIssue{Code: "job_lock", Summary: sanitizeSummary(err.Error())}
		return false
	}
	record.runLock, record.runsHere = runLock, true
	return true
}

func canceledNeedsCleanup(record *jobRecord) bool {
	if record.presentation.Issue == nil {
		return false
	}
	issue := record.presentation.Issue.Code
	return issue == "cancel_cleanup" && record.presentation.Phase == fix.PhaseCanceled ||
		issue == "canceled" && (record.presentation.Phase == fix.PhaseCanceling || record.presentation.Phase == fix.PhaseDiscarding)
}

func (record *jobRecord) markCanceledPublication(flags *recoveryFlags) {
	if !record.canceled {
		return
	}
	switch record.publicationStep {
	case publicationLocalRef, publicationRemoteRef, publicationReconcile:
		record.delivery.Ambiguous, flags.resumeDelivery, flags.cancelCleanup = true, true, false
	case publicationPullRequest, publicationPRReconcile:
		record.published.Ambiguous, flags.resumeDelivery, flags.cancelCleanup = true, true, false
	}
}

func (owner *recoveryOwner) discoverRestoredCandidate(record *jobRecord, cancelCleanup bool) {
	request := candidate.PrepareRequest{Job: record.presentation.ID, Workspace: record.input.Workspace, Targets: record.input.Targets,
		Mode: record.input.DeliveryPlan.Workspace, AllowedScope: record.input.ChangeScope, AllowedPaths: record.input.AllowedPaths,
		CommandOutputBytes: record.input.Preferences.Delivery.CommandOutputBytes}
	identity, found, err := owner.candidates.DiscoverPrepared(context.Background(), request)
	if err != nil {
		record.recordDiscoveryFailure(cancelCleanup, err)
		return
	}
	if !found {
		return
	}
	record.candidate = &identity
	record.presentation.UpdatedAt = owner.clock()
	if err := owner.persistence.saveRecord(context.Background(), record); err != nil {
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionBlocking
		record.presentation.CurrentAction = "Recovered candidate could not be saved"
		record.presentation.Issue = &fix.JobIssue{Code: "state_save_failed", Summary: "Candidate recovery was not saved", Detail: sanitizeSummary(err.Error())}
	}
}

func (record *jobRecord) recordDiscoveryFailure(cancelCleanup bool, err error) {
	if cancelCleanup {
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Canceled; cleanup could not be checked"
		record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(err.Error())}
		return
	}
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionBlocking
	record.presentation.CurrentAction = "Prepared candidate discovery failed"
	record.presentation.Issue = &fix.JobIssue{Code: "candidate_discovery", Summary: sanitizeSummary(err.Error())}
}

func (owner *recoveryOwner) restoreCanceled(record *jobRecord) {
	var err error
	if record.candidate != nil {
		err = owner.candidates.ReconcileDiscard(context.Background(), *record.candidate)
	}
	record.applyCanceledRecovery(err, owner.clock)
	_ = owner.persistence.saveRecord(context.Background(), record)
}

func (owner *recoveryOwner) restoreCleanup(record *jobRecord) bool {
	if record.presentation.Issue == nil || (record.presentation.Issue.Code != "cleanup_failed" && record.presentation.Issue.Code != "cleanup_interrupted") {
		return false
	}
	var err error
	if record.candidate != nil {
		err = owner.candidates.ReconcileDiscard(context.Background(), *record.candidate)
	}
	record.applyCleanupRecovery(err, owner.clock)
	_ = owner.persistence.saveRecord(context.Background(), record)
	return true
}

func (owner *recoveryOwner) restoreCandidate(record *jobRecord) {
	if record.candidate == nil {
		return
	}
	if record.presentation.Phase == fix.PhaseDiscarding {
		owner.restoreDiscard(record)
		return
	}
	if err := owner.candidates.Recover(context.Background(), *record.candidate, record.input.Targets, record.input.ChangeScope, record.input.AllowedPaths); err != nil {
		record.markCandidateRecoveryFailure(err)
		return
	}
	diff, err := owner.candidates.Diff(context.Background(), *record.candidate)
	if err != nil {
		record.markCandidateInventoryFailure(err)
		return
	}
	record.applyDiffInventory(diff)
}

func (owner *recoveryOwner) restoreDiscard(record *jobRecord) {
	if err := owner.candidates.ReconcileDiscard(context.Background(), *record.candidate); err != nil {
		record.markDiscardRecoveryFailure(err)
		return
	}
	record.markDiscardRecovered(owner.clock())
}

func (record *jobRecord) applyCanceledRecovery(err error, now func() time.Time) {
	record.presentation.Phase = fix.PhaseCanceled
	record.presentation.Attention = fix.AttentionNone
	record.presentation.CurrentAction = "Canceled"
	if err != nil {
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Canceled; cleanup will retry on startup"
		record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(err.Error())}
	} else {
		record.candidate, record.diffHash, record.diffPaths = nil, "", nil
	}
	if record.presentation.FinishedAt.IsZero() {
		record.presentation.FinishedAt = now()
	}
	record.presentation.UpdatedAt = now()
	record.refreshActions()
}

func (record *jobRecord) applyCleanupRecovery(err error, now func() time.Time) {
	record.presentation.Phase = fix.PhaseCompleted
	if record.presentation.FinishedAt.IsZero() {
		record.presentation.FinishedAt = now()
	}
	if err == nil {
		record.candidate, record.diffHash, record.diffPaths = nil, "", nil
		record.presentation.Attention, record.presentation.CurrentAction, record.presentation.Issue = fix.AttentionNone, "Done", nil
	} else {
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Done; worktree cleanup failed"
		record.presentation.Issue = &fix.JobIssue{Code: "cleanup_failed", Summary: sanitizeSummary(err.Error())}
	}
	record.presentation.UpdatedAt = now()
	record.refreshActions()
}

func (record *jobRecord) markCandidateRecoveryFailure(err error) {
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionError
	record.presentation.CurrentAction = "Candidate recovery failed"
	record.presentation.Issue = &fix.JobIssue{Code: "candidate_recovery", Summary: err.Error()}
}

func (record *jobRecord) markCandidateInventoryFailure(err error) {
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionBlocking
	record.presentation.CurrentAction = "Candidate inventory recovery failed"
	record.presentation.Issue = &fix.JobIssue{Code: "candidate_inventory", Summary: sanitizeSummary(err.Error())}
}

func (record *jobRecord) markDiscardRecoveryFailure(err error) {
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionBlocking
	record.presentation.CurrentAction = "Candidate discard recovery failed"
	record.presentation.Issue = &fix.JobIssue{Code: "discard_recovery", Summary: sanitizeSummary(err.Error())}
}

func (record *jobRecord) markDiscardRecovered(now time.Time) {
	record.candidate, record.diffHash, record.diffPaths = nil, "", nil
	record.presentation.Phase = fix.PhaseDiscarded
	record.presentation.Attention = fix.AttentionNone
	record.presentation.CurrentAction = "Candidate discard recovered"
	record.presentation.FinishedAt = now
}

func recoveryDisablesActions(issue *fix.JobIssue) bool {
	if issue == nil {
		return false
	}
	switch issue.Code {
	case "candidate_recovery", "candidate_inventory", "candidate_discovery", "discard_recovery", "state_save_failed":
		return true
	default:
		return false
	}
}

func canResumeDelivery(record *jobRecord) bool {
	if record.candidate == nil {
		return false
	}
	if record.presentation.Issue == nil {
		return true
	}
	switch record.presentation.Issue.Code {
	case "interrupted", "publication_ambiguous", "publication_canceled":
		return true
	default:
		return false
	}
}

package fixapp

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/fix"
)

func (manager *controller) finishCancellation(record *jobRecord) {
	state := manager.state
	record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
	record.presentation.Attention = fix.AttentionNone
	state.releaseReservations(record)
	if record.candidate == nil {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.CurrentAction = "Canceled"
		record.presentation.FinishedAt = manager.options.Clock()
		manager.bump(record)
		return
	}
	record.presentation.Phase = fix.PhaseDiscarding
	record.presentation.CurrentAction = "Canceling; cleaning up"
	if manager.bump(record) {
		manager.startCandidateDiscard(record, *record.candidate)
	}
}

func (manager *controller) startCandidateDiscard(record *jobRecord, identity fix.CandidateIdentity) {
	state := manager.state
	if manager.deps.Candidates == nil {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Canceled; cleanup unavailable"
		record.presentation.FinishedAt = manager.options.Clock()
		state.releaseReservations(record)
		manager.bump(record)
		return
	}
	state.otherRunning++
	discardCtx, discardCancel := context.WithCancel(context.Background())
	record.cancel = discardCancel
	go func(job fix.JobID, attempt fix.AttemptID) {
		err := manager.deps.Candidates.Discard(discardCtx, identity)
		manager.results <- workerResult{kind: workerDiscard, job: job, attempt: attempt, err: err}
	}(record.presentation.ID, record.attempt)
}

func (manager *controller) startCandidateCleanup(record *jobRecord, identity fix.CandidateIdentity) {
	state := manager.state
	state.otherRunning++
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	record.cancel = cleanupCancel
	preserve := record.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && record.input.DeliveryPlan.Git == fix.GitLeaveUncommitted
	go func(job fix.JobID, attempt fix.AttemptID) {
		var err error
		if preserve {
			err = manager.deps.Candidates.Release(cleanupCtx, identity)
		} else {
			err = manager.deps.Candidates.Discard(cleanupCtx, identity)
		}
		manager.results <- workerResult{kind: workerCleanup, job: job, attempt: attempt, err: err}
	}(record.presentation.ID, record.attempt)
}

// handleCandidatePrepared is the durable boundary between trusted candidate
// construction and untrusted runtime launch. The identity is saved before any adapter can execute.
func (manager *controller) handleCandidatePrepared(record *jobRecord, result workerResult) {
	state := manager.state
	record.cancel = nil
	if result.err != nil {
		state.agentsRunning--
		if record.presentation.Phase == fix.PhaseCanceling && errors.Is(result.err, context.Canceled) {
			manager.finishCancellation(record)
			return
		}
		manager.failRecord(record, "candidate_prepare", result.err)
		return
	}
	if result.candidate == nil {
		state.agentsRunning--
		manager.failRecord(record, "candidate_prepare", errors.New("candidate service returned no identity"))
		return
	}
	identity := *result.candidate
	record.candidate = &identity
	record.presentation.WorkspacePath = identity.RepositoryRoot
	record.presentation.CurrentAction = "Candidate prepared; recording ownership"
	if !manager.bump(record) {
		state.agentsRunning--
		return
	}
	if record.presentation.Phase == fix.PhaseCanceling {
		state.agentsRunning--
		manager.finishCancellation(record)
		return
	}
	if state.shuttingDown {
		state.agentsRunning--
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionError
		record.presentation.CurrentAction = "Interrupted during shutdown; candidate retained for recovery"
		record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job was interrupted"}
		manager.bump(record)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel = cancel
	record.presentation.CurrentAction = "Starting agent"
	if !manager.bump(record) {
		state.agentsRunning--
		record.cancel = nil
		cancel()
		return
	}
	go manager.workers.runAgent(ctx, record.input, record.nextAttemptNotes, record.presentation.ID, record.attempt, identity)
}

func workerPhaseMatches(kind workerKind, phase fix.Phase) bool {
	if phase == fix.PhaseCanceling {
		return true
	}
	switch kind {
	case workerCandidate:
		return phase == fix.PhasePreparing
	case workerAgent:
		return phase == fix.PhasePreparing || phase == fix.PhaseRunning
	case workerVerifier:
		return phase == fix.PhaseVerifying
	case workerDiscard:
		return phase == fix.PhaseDiscarding
	case workerCleanup:
		return phase == fix.PhasePublishing
	case workerPublish:
		return phase == fix.PhasePublishing || phase == fix.PhaseReconciling
	}
	return false
}

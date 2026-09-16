package fixapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func (manager *controller) handleResult(result workerResult) {
	state := manager.state
	record := state.jobs[result.job]
	if record == nil || record.attempt != result.attempt {
		return
	}
	if result.kind == workerCandidate {
		manager.handleCandidatePrepared(record, result)
		return
	}
	manager.finishWorker(record, result)
	record.captureWorkerCandidate(result)
	recordAgentInventory(record, result)
	if !workerPhaseMatches(result.kind, record.presentation.Phase) {
		manager.changed()
		return
	}
	record.recordAgentResult(result, &manager.logging, manager.options.Clock)
	switch result.kind {
	case workerPublish:
		manager.handlePublicationResult(record, result)
	case workerCleanup:
		manager.handleCleanupResult(record, result)
	case workerDiscard:
		manager.handleDiscardResult(record, result)
	default:
		manager.handleActiveWorkerResult(record, result)
	}
}

func (manager *controller) finishWorker(record *jobRecord, result workerResult) {
	state := manager.state
	switch result.kind {
	case workerAgent:
		state.agentsRunning--
	case workerVerifier:
		state.verifiersRunning--
	case workerDiscard, workerCleanup, workerPublish:
		state.otherRunning--
	}
	record.cancel = nil
}

func (record *jobRecord) captureWorkerCandidate(result workerResult) {
	if result.candidate != nil {
		identity := *result.candidate
		record.candidate = &identity
	}
}

func recordAgentInventory(record *jobRecord, result workerResult) {
	if result.kind == workerAgent && result.diff.Fingerprint != "" {
		record.applyDiffInventory(result.diff)
	}
}

func (record *jobRecord) recordAgentResult(result workerResult, logging *loggingOwner, clock func() time.Time) {
	if result.kind != workerAgent {
		return
	}
	if reference := strings.TrimSpace(result.agent.SessionReference); reference != "" && !containsText(record.agentReferences, reference) {
		record.agentReferences = append(record.agentReferences, reference)
	}
	detail := nonempty(result.agent.Summary, result.agent.Diagnostic, string(result.agent.Status))
	logging.appendJobText(record.presentation.ID, fmt.Sprintf("%s  %-16s %s\n", clock().Format(time.RFC3339Nano), "agent_result", cleanLogText(detail)))
}

func (manager *controller) handleCleanupResult(record *jobRecord, result workerResult) {
	state := manager.state
	if record.presentation.Phase == fix.PhaseCanceling {
		manager.finishCancellation(record)
		return
	}
	if errors.Is(result.err, context.Canceled) && issueCode(record) == "interrupted" {
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Stopped while finishing"
		record.presentation.Issue = &fix.JobIssue{Code: "cleanup_interrupted", Summary: "Finishing stopped during shutdown"}
		state.releaseReservations(record)
		manager.bump(record)
		return
	}
	record.presentation.Phase = fix.PhaseCompleted
	record.presentation.FinishedAt = manager.options.Clock()
	state.releaseReservations(record)
	if result.err == nil {
		record.candidate, record.diffHash, record.diffPaths = nil, "", nil
		record.presentation.Attention, record.presentation.CurrentAction, record.presentation.Issue = fix.AttentionNone, "Done", nil
		manager.bump(record)
		return
	}
	record.presentation.Attention = fix.AttentionInfo
	record.presentation.CurrentAction = "Done; worktree cleanup failed"
	record.presentation.Issue = &fix.JobIssue{Code: "cleanup_failed", Summary: sanitizeSummary(result.err.Error())}
	manager.bump(record)
}

func (manager *controller) handleDiscardResult(record *jobRecord, result workerResult) {
	state := manager.state
	if result.err == nil {
		manager.discarded(record)
		return
	}
	if issueCode(record) == "canceled" {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Canceled; cleanup can be retried"
		record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(result.err.Error())}
		record.presentation.FinishedAt = manager.options.Clock()
		state.releaseReservations(record)
		manager.bump(record)
		return
	}
	if errors.Is(result.err, context.Canceled) {
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionError
		record.presentation.CurrentAction = "Discard interrupted; candidate state will be reconciled"
		record.presentation.Issue = &fix.JobIssue{Code: "discard_interrupted", Summary: "Candidate discard was interrupted"}
		manager.bump(record)
		return
	}
	manager.failRecord(record, "discard_failed", result.err)
}

func (manager *controller) discarded(record *jobRecord) {
	state := manager.state
	wasCanceled := issueCode(record) == "canceled"
	record.candidate, record.diffHash, record.diffPaths = nil, "", nil
	record.presentation.Phase = fix.PhaseDiscarded
	record.presentation.Attention = fix.AttentionNone
	record.presentation.CurrentAction = "Candidate discarded"
	if wasCanceled {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.CurrentAction = "Canceled"
	}
	record.presentation.FinishedAt = manager.options.Clock()
	state.releaseReservations(record)
	manager.bump(record)
}

func (manager *controller) handleActiveWorkerResult(record *jobRecord, result workerResult) {
	if record.presentation.Phase == fix.PhaseCanceling && result.kind != workerDiscard {
		manager.finishCancellation(record)
		return
	}
	if result.err != nil {
		manager.failRecord(record, "worker_failed", result.err)
		return
	}
	switch result.kind {
	case workerAgent:
		manager.completeAgent(record, result)
	case workerVerifier:
		manager.completeVerification(record, result)
	}
}

func (manager *controller) completeAgent(record *jobRecord, result workerResult) {
	if result.inventoryErr != nil {
		manager.failRecord(record, "candidate_inventory", fmt.Errorf("inventory retained candidate: %w", result.inventoryErr))
		return
	}
	if result.agent.Status != agent.ResultCompleted {
		manager.failRecord(record, "agent_"+string(result.agent.Failure), errors.New(nonempty(result.agent.Diagnostic, result.agent.Summary)))
		return
	}
	record.presentation.Phase = fix.PhaseWaitingVerifier
	record.presentation.CurrentAction = "Waiting for a verifier slot"
	manager.bump(record)
}

func (manager *controller) completeVerification(record *jobRecord, result workerResult) {
	record.applyVerification(result)
	if record.presentation.TargetStatus != fix.TargetMet {
		manager.queueRetry(record)
		return
	}
	record.presentation.Phase = fix.PhasePublishing
	record.presentation.Attention = fix.AttentionNone
	record.presentation.CurrentAction = publicationAction(record)
	record.presentation.Issue = nil
	if !manager.bump(record) {
		return
	}
	if record.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
		manager.startCandidateCleanup(record, *record.candidate)
		return
	}
	manager.startNextPublication(record)
}

func (manager *controller) queueRetry(record *jobRecord) {
	record.presentation.AttemptOrdinal++
	record.presentation.Phase = fix.PhaseQueued
	record.presentation.Attention = fix.AttentionNone
	activity := fmt.Sprintf("Retry attempt %d queued: target score not met", record.presentation.AttemptOrdinal)
	record.presentation.CurrentAction = activity
	entry := LogEntry{At: manager.options.Clock(), Kind: agent.EventActivity, Summary: activity}
	record.logs = append(record.logs, entry)
	manager.logging.appendJobText(record.presentation.ID, formatJobActivity(entry))
	record.presentation.Issue = nil
	record.presentation.TargetStatus = fix.ScorePending
	manager.bump(record)
}

func publicationAction(record *jobRecord) string {
	if record.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
		return "Finishing"
	}
	return "Committing changes"
}

func issueCode(record *jobRecord) string {
	if record.presentation.Issue == nil {
		return ""
	}
	return record.presentation.Issue.Code
}

func containsText(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

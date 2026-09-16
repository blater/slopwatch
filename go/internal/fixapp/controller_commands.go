package fixapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

type cancelPlan struct {
	worker    context.CancelFunc
	discard   *fix.CandidateIdentity
	reconcile bool
}

func (manager *controller) handleCommand(call commandCall) {
	state := manager.state
	record := state.jobs[call.command.JobID]
	if record == nil {
		call.response <- commandResponse{err: ErrJobNotFound}
		return
	}
	if record.runsElsewhere {
		call.response <- commandResponse{err: errors.New("job is running in another Slopwatch window")}
		return
	}
	if receipt, exists := record.commands[call.command.RequestID]; exists {
		receipt.Duplicate = true
		call.response <- commandResponse{receipt: receipt}
		return
	}
	if err := validateCommand(record, call.command); err != nil {
		call.response <- commandResponse{err: err}
		return
	}
	previousPresentation, previousCanceled := clonePresentation(record.presentation), record.canceled
	plan := record.applyCommand(call.command, manager.options.Clock)
	receipt := CommandReceipt{RequestID: call.command.RequestID, JobID: call.command.JobID, Accepted: true}
	record.presentation.UpdatedAt = manager.options.Clock()
	record.refreshActions()
	record.commands[call.command.RequestID] = receipt
	if err := manager.persistence.saveRecord(call.ctx, record); err != nil {
		record.presentation, record.canceled = previousPresentation, previousCanceled
		delete(record.commands, call.command.RequestID)
		manager.changed()
		call.response <- commandResponse{err: fmt.Errorf("persist job command: %w", err)}
		return
	}
	state.releaseReservations(record)
	manager.changed()
	if plan.worker != nil {
		plan.worker()
	}
	if plan.discard != nil {
		manager.startCandidateDiscard(record, *plan.discard)
	}
	if plan.reconcile {
		manager.startNextPublication(record)
	}
	call.response <- commandResponse{receipt: receipt}
}

func validateCommand(record *jobRecord, command fix.JobCommand) error {
	if command.RequestID == "" {
		return errors.New("job command requires request id")
	}
	if !hasAction(record.presentation.AllowedActions, command.Action) {
		return ErrActionNotAllowed
	}
	return nil
}

func (record *jobRecord) applyCommand(command fix.JobCommand, now func() time.Time) cancelPlan {
	plan := cancelPlan{}
	if command.Action != fix.ActionCancel {
		return plan
	}
	record.canceled = true
	record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
	switch {
	case record.cancel != nil:
		record.presentation.Phase = fix.PhaseCanceling
		record.presentation.CurrentAction = "Canceling"
		plan.worker = record.cancel
	case record.delivery.Ambiguous || record.published.Ambiguous:
		record.presentation.Phase = fix.PhaseReconciling
		record.presentation.CurrentAction = "Checking canceled delivery"
		plan.reconcile = true
	case record.candidate != nil:
		record.presentation.Phase = fix.PhaseDiscarding
		record.presentation.CurrentAction = "Canceling; cleaning up"
		identity := *record.candidate
		plan.discard = &identity
	default:
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.Attention = fix.AttentionNone
		record.presentation.CurrentAction = "Canceled"
		record.presentation.FinishedAt = now()
	}
	return plan
}

func (manager *controller) handleShutdown(call shutdownCall) {
	state := manager.state
	state.shutdownWaiters = append(state.shutdownWaiters, call.response)
	if state.shuttingDown {
		return
	}
	state.shuttingDown = true
	for _, record := range state.jobs {
		record.stopForShutdown()
	}
	manager.changed()
}

func (record *jobRecord) stopForShutdown() {
	if record.cancel != nil {
		record.presentation.CurrentAction = "Stopping for shutdown"
		record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job was interrupted by shutdown"}
		record.cancel()
		return
	}
	if record.presentation.Phase != fix.PhaseQueued && record.presentation.Phase != fix.PhaseWaitingVerifier {
		return
	}
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionError
	record.presentation.CurrentAction = "Stopped before shutdown"
}

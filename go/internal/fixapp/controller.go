package fixapp

import (
	"context"
	"fmt"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func (manager *controller) handleReconfigure(call reconfigureCall) {
	state := manager.state
	if state.shuttingDown {
		call.response <- ErrClosed
		return
	}
	if err := call.ctx.Err(); err != nil {
		call.response <- err
		return
	}
	manager.options.MaxAgents = call.limits.MaxAgents
	manager.options.MaxVerifiers = call.limits.MaxVerifiers
	call.response <- nil
}

func (manager *controller) restore() {
	for _, record := range manager.recovery.restore(manager.state, manager.initial) {
		manager.startNextPublication(record)
	}
	manager.initial = nil
}

func (manager *controller) handleRun(call runCall) {
	state := manager.state
	if state.shuttingDown {
		call.response <- runResponse{err: ErrClosed}
		return
	}
	if err := call.ctx.Err(); err != nil {
		call.response <- runResponse{err: err}
		return
	}
	admission, err := manager.deps.Store.Lock("job-admission")
	if err != nil {
		call.response <- runResponse{err: fmt.Errorf("start fix: %w", err)}
		return
	}
	defer admission.Close()
	manager.refreshSharedJobs()
	if owner := state.reservedOwner(call.input); owner != "" {
		call.response <- runResponse{err: fmt.Errorf("%w: selected files overlap running job %s", ErrTargetReserved, owner)}
		return
	}
	id, err := fix.NewJobID()
	if err != nil {
		call.response <- runResponse{err: err}
		return
	}
	runLock, err := manager.deps.Store.Lock(id)
	if err != nil {
		call.response <- runResponse{err: fmt.Errorf("start fix job: %w", err)}
		return
	}
	record := newQueuedRecord(call.input, id, manager.options.Clock(), runLock)
	record.refreshActions()
	if err := manager.persistence.saveRecord(call.ctx, record); err != nil {
		_ = runLock.Close()
		call.response <- runResponse{err: fmt.Errorf("persist fix start: %w", err)}
		return
	}
	state.jobs[id], state.order = record, append(state.order, id)
	manager.logging.logJobStart(record)
	state.reserveRecord(record)
	manager.changed()
	call.response <- runResponse{id: id}
}

func newQueuedRecord(input FixInput, id fix.JobID, now time.Time, runLock jobstore.Lock) *jobRecord {
	presentation := fix.JobPresentation{ID: id, Phase: fix.PhaseQueued, Attention: fix.AttentionNone,
		ProfileLabel: input.Profile.Label, ProfileID: string(input.Profile.ID), ModelLabel: string(input.Model), EffortLabel: string(input.Effort),
		Goal: goalLabel(input), Targets: baselineTargets(input.Baseline.Contract), CurrentAction: "Waiting for an agent slot", AttemptOrdinal: 1,
		CreatedAt: now, UpdatedAt: now, TargetStatus: fix.ScorePending, Scope: fix.ScopeUnknown, Delivery: fix.DeliveryNone,
		DeliveryPlan: input.DeliveryPlan, BranchName: input.BranchName}
	return &jobRecord{input: cloneFixInput(input), presentation: presentation, commands: map[fix.CommandID]CommandReceipt{}, runLock: runLock, runsHere: true}
}

func (manager *controller) schedule() {
	state := manager.state
	if state.shuttingDown {
		return
	}
	manager.scheduleAgents()
	manager.scheduleVerifiers()
}

func (manager *controller) scheduleAgents() {
	state := manager.state
	for state.agentsRunning < manager.options.MaxAgents {
		record := state.firstInPhase(fix.PhaseQueued)
		if record == nil {
			return
		}
		attempt, err := fix.NewAttemptID()
		if err != nil {
			manager.failRecord(record, "attempt_id", err)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		record.prepareAgentSlot(attempt, cancel)
		if record.candidate != nil {
			record.presentation.CurrentAction = "Starting agent"
		}
		state.agentsRunning++
		if !manager.bump(record) {
			state.agentsRunning--
			record.cancel = nil
			cancel()
			continue
		}
		if record.candidate == nil {
			go manager.workers.runCandidatePrepare(ctx, record.input, record.presentation.ID, attempt)
			continue
		}
		go manager.workers.runAgent(ctx, record.input, record.nextAttemptNotes, record.presentation.ID, attempt, *record.candidate)
	}
}

func (record *jobRecord) prepareAgentSlot(attempt fix.AttemptID, cancel context.CancelFunc) {
	record.attempt, record.cancel = attempt, cancel
	record.actors = map[string]bool{}
	record.presentation.ActorCount = 0
	record.presentation.Phase = fix.PhasePreparing
	record.presentation.CurrentAction = "Creating worktree"
	if record.input.DeliveryPlan.Workspace == fix.WorkspaceCurrent {
		record.presentation.CurrentAction = "Preparing current files"
	}
	record.presentation.Attention = fix.AttentionNone
	record.presentation.Issue = nil
}

func (manager *controller) scheduleVerifiers() {
	state := manager.state
	for state.verifiersRunning < manager.options.MaxVerifiers {
		record := state.firstInPhase(fix.PhaseWaitingVerifier)
		if record == nil {
			return
		}
		record.presentation.Phase = fix.PhaseVerifying
		record.presentation.CurrentAction = "Collecting changed files"
		ctx, cancel := context.WithCancel(context.Background())
		record.cancel = cancel
		state.verifiersRunning++
		if !manager.bump(record) {
			state.verifiersRunning--
			record.cancel = nil
			cancel()
			continue
		}
		go manager.workers.runVerifier(ctx, record.input, record.presentation.ID, record.attempt, *record.candidate)
	}
}

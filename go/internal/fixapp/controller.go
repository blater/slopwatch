package fixapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
	"github.com/blater/slopwatch/internal/validation"
)

func validateInitialJournal(records []jobstore.Record, maxTranscriptItems int) error {
	seen := make(map[fix.JobID]uint64)
	for index, stored := range records {
		if stored.Version != jobstore.RecordVersion || stored.Sequence != uint64(index+1) {
			return fmt.Errorf("fix journal record %d has invalid version or sequence", index+1)
		}
		if stored.JobID == "" || stored.Revision == 0 || stored.Kind == "" {
			return fmt.Errorf("fix journal record %d is incomplete", index+1)
		}
		var envelope journalEnvelope
		if err := decodeStrict(stored.Data, &envelope); err != nil {
			return fmt.Errorf("decode fix journal record %d: %w", index+1, err)
		}
		if envelope.Presentation.ID != stored.JobID || envelope.Presentation.Revision != stored.Revision {
			return fmt.Errorf("fix journal record %d identity/revision mismatch", index+1)
		}
		previous, exists := seen[stored.JobID]
		if !exists && stored.Kind != "admitted" && stored.Kind != "checkpoint" {
			return fmt.Errorf("fix journal job %s does not begin with admission or checkpoint", stored.JobID)
		}
		if exists && stored.Revision < previous {
			return fmt.Errorf("fix journal job %s revision regressed", stored.JobID)
		}
		seen[stored.JobID] = stored.Revision
		if envelope.Candidate != nil && envelope.Candidate.Job != stored.JobID {
			return fmt.Errorf("fix journal job %s candidate identity mismatch", stored.JobID)
		}
		if envelope.Transcript != nil {
			if len(envelope.Transcript.Entries) > maxTranscriptItems {
				return fmt.Errorf("fix journal job %s transcript exceeds configured bound", stored.JobID)
			}
			for _, entry := range envelope.Transcript.Entries {
				if len(entry.Summary) > 504 || strings.ContainsRune(entry.Summary, '\x00') {
					return fmt.Errorf("fix journal job %s contains an invalid transcript entry", stored.JobID)
				}
			}
		}
		for requestID, receipt := range envelope.Commands {
			if requestID == "" || receipt.RequestID != requestID || receipt.JobID != stored.JobID || !receipt.Accepted {
				return fmt.Errorf("fix journal job %s contains an invalid command receipt", stored.JobID)
			}
		}
		if stored.Kind == "agent_event" {
			var entry LogEntry
			if err := decodeStrict(envelope.Payload, &entry); err != nil || len(entry.Summary) > 504 || strings.ContainsRune(entry.Summary, '\x00') {
				return fmt.Errorf("fix journal job %s contains an invalid agent event", stored.JobID)
			}
		}
		if stored.Kind == "command_applied" || stored.Kind == "command_intent" {
			var command fix.JobCommand
			if err := decodeStrict(envelope.Payload, &command); err != nil || command.RequestID == "" || command.JobID != stored.JobID {
				return fmt.Errorf("fix journal job %s contains an invalid command payload", stored.JobID)
			}
		}
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (manager *Manager) run() {
	state := &controllerState{
		jobs: map[fix.JobID]*jobRecord{}, reservations: map[string]fix.JobID{},
	}
	manager.restore(state)
	manager.publish(state)
	close(manager.ready)
	defer func() {
		manager.closed.Store(true)
		manager.publish(state)
		closeErr := errors.Join(manager.deps.Store.Close(), manager.deps.Candidates.Close())
		manager.notifyMu.Lock()
		close(manager.notify)
		manager.notify = make(chan struct{})
		manager.notifyMu.Unlock()
		close(manager.done)
		for _, waiter := range state.shutdownWaiters {
			waiter <- closeErr
		}
	}()

	for {
		manager.schedule(state)
		if state.shuttingDown && state.agentsRunning == 0 && state.verifiersRunning == 0 && state.otherRunning == 0 {
			return
		}
		select {
		case request := <-manager.requests:
			switch value := request.(type) {
			case submitCall:
				manager.handleSubmit(state, value)
			case commandCall:
				manager.handleCommand(state, value)
			case candidateCall:
				record, found := state.jobs[value.id]
				if found && record.candidate != nil {
					value.response <- candidateResponse{identity: *record.candidate, ok: true}
				} else {
					value.response <- candidateResponse{}
				}
			case shutdownCall:
				manager.handleShutdown(state, value)
			}
		case update := <-manager.events:
			if update.barrier != nil {
				manager.flushTranscript(state, update.job, update.attempt)
				close(update.barrier)
			} else {
				manager.handleEvent(state, update.event)
			}
		case result := <-manager.results:
			manager.handleResult(state, result)
		}
	}
}

func (manager *Manager) handleSubmit(state *controllerState, call submitCall) {
	if state.shuttingDown || manager.journalFailed {
		call.response <- submitResponse{err: ErrClosed}
		return
	}
	if err := call.ctx.Err(); err != nil {
		call.response <- submitResponse{err: err}
		return
	}
	draft := call.request.Draft
	if err := validateDraft(draft); err != nil {
		call.response <- submitResponse{err: err}
		return
	}
	for _, target := range draft.Targets {
		key := reservationKey(draft.Workspace, target)
		if owner, exists := state.reservations[key]; exists {
			call.response <- submitResponse{err: fmt.Errorf("%w: %s is owned by %s", ErrTargetReserved, target, owner)}
			return
		}
	}
	id, err := fix.NewJobID()
	if err != nil {
		call.response <- submitResponse{err: err}
		return
	}
	now := manager.options.Clock()
	presentation := fix.JobPresentation{
		ID: id, Revision: 1, Phase: fix.PhaseQueued, Attention: fix.AttentionNone,
		ProfileLabel: draft.Profile.Label, ModelLabel: string(draft.Model), EffortLabel: string(draft.Effort),
		Goal: goalLabel(draft), Targets: baselineTargets(draft.Baseline.Contract), CurrentAction: "Waiting for an agent slot",
		CreatedAt: now, UpdatedAt: now, Compliance: fix.ComplianceUnknown,
		Validation: fix.ValidationNotRun, Scope: fix.ScopeUnknown, Delivery: fix.DeliveryNone,
		DeliveryMode: draft.DeliveryMode, BranchName: draft.BranchName,
	}
	record := &jobRecord{draft: cloneSubmit(SubmitRequest{Draft: draft}).Draft, presentation: presentation, commands: map[fix.CommandID]CommandReceipt{}}
	manager.refreshActions(record)
	if err := manager.appendRecord(call.ctx, record, "admitted", admissionPayload(draft)); err != nil {
		call.response <- submitResponse{err: fmt.Errorf("persist fix admission: %w", err)}
		return
	}
	state.jobs[id] = record
	state.order = append(state.order, id)
	for _, target := range draft.Targets {
		state.reservations[reservationKey(draft.Workspace, target)] = id
	}
	manager.changed(state)
	call.response <- submitResponse{id: id}
}

func validateDraft(draft FixDraft) error {
	if draft.ID == "" || draft.Workspace.RepositoryRoot == "" || len(draft.Targets) == 0 {
		return errors.New("submit fix: draft is incomplete")
	}
	if !draft.Preflight.Clean || !draft.Preflight.Supported {
		return fmt.Errorf("submit fix: workspace preflight failed: %s", draft.Preflight.Diagnostic)
	}
	if math.IsNaN(draft.TargetScore) || math.IsInf(draft.TargetScore, 0) || draft.TargetScore < 0 || draft.Baseline.Contract.Goal.MaximumScore != draft.TargetScore {
		return errors.New("submit fix: score goal is invalid or inconsistent")
	}
	if len(draft.Baseline.Contract.Targets) != len(draft.Targets) || draft.Instructions.Envelope == "" {
		return errors.New("submit fix: scoring contract or safety envelope is incomplete")
	}
	for index, target := range draft.Targets {
		if draft.Baseline.Contract.Targets[index].Path != target || !draft.Baseline.Contract.Targets[index].Complete {
			return errors.New("submit fix: scoring contract targets do not match the prepared targets")
		}
	}
	if draft.Probe.State != agent.ProbeReady {
		return fmt.Errorf("submit fix: agent profile is %s: %s", draft.Probe.State, draft.Probe.Diagnostic)
	}
	if !draft.Probe.Capabilities.Isolation.EligibleForMutation() {
		return errors.New("submit fix: agent runtime did not prove required write, read, auth, and crash confinement")
	}
	if !containsOption(draft.Probe.Capabilities.Models, draft.Model) || !containsOption(draft.Probe.Capabilities.Efforts, draft.Effort) ||
		!containsOption(draft.Probe.Capabilities.Delegation, draft.Delegation) {
		return errors.New("submit fix: selected model, effort, or delegation mode is unsupported by this profile")
	}
	return nil
}

func containsOption[T ~string](values []agent.Option[T], selected T) bool {
	for _, value := range values {
		if value.ID == selected {
			return true
		}
	}
	return false
}

func (manager *Manager) schedule(state *controllerState) {
	if state.shuttingDown || manager.journalFailed {
		return
	}
	for state.agentsRunning < manager.options.MaxAgents {
		record := firstInPhase(state, fix.PhaseQueued)
		if record == nil {
			break
		}
		attempt, err := fix.NewAttemptID()
		if err != nil {
			manager.failRecord(state, record, "attempt_id", err)
			continue
		}
		record.attempt = attempt
		record.actors = map[string]bool{}
		record.presentation.ActorCount = 0
		record.presentation.Phase = fix.PhasePreparing
		record.presentation.CurrentAction = "Creating isolated candidate worktree"
		record.presentation.Attention = fix.AttentionNone
		record.presentation.Issue = nil
		ctx, cancel := context.WithCancel(context.Background())
		record.cancel = cancel
		state.agentsRunning++
		if record.candidate == nil {
			if !manager.bump(state, record, "candidate_prepare_started", nil) {
				state.agentsRunning--
				record.cancel = nil
				cancel()
				continue
			}
			go manager.runCandidatePrepare(ctx, record.draft, record.presentation.ID, attempt)
			continue
		}
		record.presentation.CurrentAction = "Starting agent"
		if !manager.bump(state, record, "agent_started", nil) {
			state.agentsRunning--
			record.cancel = nil
			cancel()
			continue
		}
		go manager.runAgent(ctx, record.draft, record.presentation.ID, attempt, *record.candidate)
	}
	for state.verifiersRunning < manager.options.MaxVerifiers {
		record := firstInPhase(state, fix.PhaseWaitingVerifier)
		if record == nil {
			break
		}
		record.presentation.Phase = fix.PhaseVerifying
		record.presentation.CurrentAction = "Re-analyzing candidate and running validation"
		ctx, cancel := context.WithCancel(context.Background())
		record.cancel = cancel
		state.verifiersRunning++
		if !manager.bump(state, record, "verification_started", nil) {
			state.verifiersRunning--
			record.cancel = nil
			cancel()
			continue
		}
		go manager.runVerifier(ctx, record.draft, record.presentation.ID, record.attempt, *record.candidate)
	}
}

func (manager *Manager) runCandidatePrepare(ctx context.Context, draft FixDraft, job fix.JobID, attempt fix.AttemptID) {
	identity, err := manager.deps.Candidates.Prepare(ctx, candidate.PrepareRequest{Job: job, Workspace: draft.Workspace,
		Targets: draft.Targets, AllowedScope: draft.ChangeScope, AllowedPaths: draft.AllowedPaths})
	if err != nil {
		err = fmt.Errorf("prepare candidate: %w", err)
	}
	manager.results <- workerResult{kind: workerCandidate, job: job, attempt: attempt, candidate: candidatePointer(identity, err), err: err}
}

func candidatePointer(identity fix.CandidateIdentity, err error) *fix.CandidateIdentity {
	if err != nil {
		return nil
	}
	return &identity
}

func firstInPhase(state *controllerState, phase fix.Phase) *jobRecord {
	for _, id := range state.order {
		if record := state.jobs[id]; record != nil && record.presentation.Phase == phase {
			return record
		}
	}
	return nil
}

func (manager *Manager) runAgent(ctx context.Context, draft FixDraft, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	select {
	case manager.events <- agentUpdate{event: agent.Event{JobID: job, AttemptID: attempt, At: manager.options.Clock(), Kind: agent.EventActivity, Summary: "Candidate ready; starting agent"}}:
	case <-manager.done:
		return
	}
	strategy, err := manager.deps.Agents.Strategy(draft.Profile.Runtime)
	if err != nil {
		manager.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, err: err})
		return
	}
	request := agent.Request{
		JobID: job, AttemptID: attempt, Workspace: identity, Model: draft.Model, Effort: draft.Effort, Delegation: draft.Delegation,
		Task: agent.RemediationTask{Targets: cloneContract(draft.Baseline.Contract).Targets, Goal: draft.Baseline.Contract.Goal,
			Instructions: draft.Instructions, Validation: agent.ValidationContract{PlanID: draft.ValidationPlanID, Required: draft.ValidationPlanID != ""}},
		Write: agent.WritePolicy{Allowed: append([]fix.RepoPath(nil), draft.AllowedPaths...), Scope: draft.ChangeScope},
		Limits: agent.Limits{WallTime: draft.Preferences.Fix.AttemptTimeout, MaxOutputBytes: draft.Preferences.Concurrency.MaxTranscriptBytes,
			MaxEvents: manager.options.MaxTranscriptItems, MaxActors: 32},
	}
	result := strategy.Execute(ctx, draft.Profile, request, agent.EventSinkFunc(func(event agent.Event) error {
		if event.JobID != job || event.AttemptID != attempt {
			return errors.New("agent event identity mismatch")
		}
		select {
		case manager.events <- agentUpdate{event: event}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-manager.done:
			return ErrClosed
		}
	}))
	inventoryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	diff, inventoryErr := manager.deps.Candidates.Diff(inventoryCtx, identity)
	cancel()
	manager.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, agent: result,
		diff: diff, inventoryErr: inventoryErr})
}

func (manager *Manager) finishAgentWorker(job fix.JobID, attempt fix.AttemptID, result workerResult) {
	barrier := make(chan struct{})
	select {
	case manager.events <- agentUpdate{barrier: barrier, job: job, attempt: attempt}:
		select {
		case <-barrier:
		case <-manager.done:
			return
		}
	case <-manager.done:
		return
	}
	select {
	case manager.results <- result:
	case <-manager.done:
	}
}

func (manager *Manager) runVerifier(ctx context.Context, draft FixDraft, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	diff, err := manager.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, err: fmt.Errorf("inventory candidate diff: %w", err)}
		return
	}
	verified, err := manager.deps.Analysis.Verify(ctx, fixanalysis.VerificationRequest{Candidate: identity, Contract: draft.Baseline.Contract})
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, err: fmt.Errorf("verify candidate scores: %w", err)}
		return
	}
	var validationResult validation.Result
	if draft.ValidationPlanID != "" {
		plan, found := validationPlan(draft, draft.ValidationPlanID)
		if !found || manager.deps.Validation == nil {
			manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, verify: verified, err: errors.New("required validation plan is unavailable")}
			return
		}
		validationResult, err = manager.deps.Validation.Validate(ctx, identity, plan)
		if err != nil {
			manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, verify: verified, err: fmt.Errorf("validate candidate: %w", err)}
			return
		}
	}
	finalDiff, err := manager.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, verify: verified, validation: validationResult, err: fmt.Errorf("re-inventory candidate after verification: %w", err)}
		return
	}
	if finalDiff.Fingerprint != diff.Fingerprint {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: finalDiff, verify: verified, validation: validationResult, err: errors.New("candidate changed during analysis or validation")}
		return
	}
	manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: finalDiff, verify: verified, validation: validationResult}
}

func publicationRequests(draft FixDraft, job fix.JobID, identity fix.CandidateIdentity, diffHash string, delivered delivery.Result) (delivery.Request, publisher.Request) {
	request := delivery.Request{Job: job, Candidate: identity, DiffHash: diffHash, Branch: draft.BranchName,
		Remote: draft.Preferences.Delivery.Remote, CommitTitle: "Refactor " + targetLabel(draft.Targets) + " with Slopwatch",
		CommitBody: "Automated remediation for " + goalLabel(draft) + "."}
	pullRequest := publisher.Request{Job: job, Repository: identity.Repository, Candidate: identity, HostRepository: delivered.Repository,
		Remote: draft.Preferences.Delivery.Remote, BaseBranch: draft.Preferences.Delivery.BaseBranch, HeadBranch: draft.BranchName,
		Commit: delivered.Commit, Title: request.CommitTitle, Body: request.CommitBody, Draft: draft.Preferences.Delivery.DraftPullRequests}
	return request, pullRequest
}

func (manager *Manager) runPublicationStep(ctx context.Context, step publicationStep, draft FixDraft, job fix.JobID, attempt fix.AttemptID,
	identity fix.CandidateIdentity, diffHash string, delivered delivery.Result, published publisher.Result) {
	request, pullRequest := publicationRequests(draft, job, identity, diffHash, delivered)
	var err error
	switch step {
	case publicationCommit:
		delivered, err = manager.deps.Delivery.CreateCommit(ctx, request)
	case publicationLocalRef:
		delivered, err = manager.deps.Delivery.CreateLocalRef(ctx, request, delivered)
	case publicationRemoteRef:
		delivered, err = manager.deps.Delivery.CreateRemoteRef(ctx, request, delivered)
	case publicationReconcile:
		delivered, err = manager.deps.Delivery.Reconcile(ctx, request, delivered)
	case publicationPullRequest:
		if manager.deps.Publisher == nil {
			err = errors.New("pull request publisher is unavailable")
		} else {
			pullRequest.Commit = delivered.Commit
			published, err = manager.deps.Publisher.Create(ctx, pullRequest)
		}
	}
	manager.results <- workerResult{kind: workerPublish, step: step, job: job, attempt: attempt, delivery: delivered, published: published, err: err}
}

func targetLabel(targets []fix.RepoPath) string {
	if len(targets) == 0 {
		return "candidate"
	}
	if len(targets) == 1 {
		return targets[0].String()
	}
	return fmt.Sprintf("%d files", len(targets))
}

func validationPlan(draft FixDraft, id string) (validation.Plan, bool) {
	for _, plan := range draft.Preferences.Validation {
		if plan.ID == id {
			return plan, true
		}
	}
	return validation.Plan{}, false
}

func (manager *Manager) handleResult(state *controllerState, result workerResult) {
	record := state.jobs[result.job]
	if record == nil || record.attempt != result.attempt {
		return
	}
	if result.kind == workerCandidate {
		manager.handleCandidatePrepared(state, record, result)
		return
	}
	switch result.kind {
	case workerAgent:
		state.agentsRunning--
	case workerVerifier:
		state.verifiersRunning--
	case workerDiscard:
		state.otherRunning--
	case workerPublish:
		state.otherRunning--
	}
	record.cancel = nil
	if result.candidate != nil {
		identity := *result.candidate
		record.candidate = &identity
	}
	if result.kind == workerAgent && result.diff.Fingerprint != "" {
		manager.applyDiffInventory(record, result.diff)
		manager.reconcileConflicts(state)
	}
	if !workerPhaseMatches(result.kind, record.presentation.Phase) {
		manager.changed(state)
		return
	}
	if result.kind == workerPublish {
		manager.handlePublicationResult(state, record, result)
		return
	}
	if result.kind == workerDiscard {
		if result.err == nil {
			record.candidate = nil
			record.diffHash, record.diffPaths, record.conflicts = "", nil, nil
			record.presentation.Phase = fix.PhaseDiscarded
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Candidate discarded"
			record.presentation.FinishedAt = manager.options.Clock()
			manager.releaseReservations(state, record)
			manager.reconcileConflicts(state)
			manager.bump(state, record, "discarded", nil)
			return
		}
		if errors.Is(result.err, context.Canceled) {
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.Attention = fix.AttentionRequired
			record.presentation.CurrentAction = "Discard interrupted; candidate state will be reconciled"
			record.presentation.Issue = &fix.JobIssue{Code: "discard_interrupted", Summary: "Candidate discard was interrupted"}
			manager.bump(state, record, "discard_interrupted", nil)
			return
		}
		manager.failRecord(state, record, "discard_failed", result.err)
		return
	}
	if record.presentation.Phase == fix.PhaseCanceling {
		record.presentation.Phase = fix.PhaseAwaitingAction
		record.presentation.Attention = fix.AttentionRequired
		record.presentation.CurrentAction = "Canceled; candidate retained for review"
		record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
		manager.bump(state, record, "canceled", nil)
		return
	}
	if result.err != nil {
		manager.failRecord(state, record, "worker_failed", result.err)
		return
	}
	switch result.kind {
	case workerAgent:
		if result.inventoryErr != nil {
			manager.failRecord(state, record, "candidate_inventory", fmt.Errorf("inventory retained candidate: %w", result.inventoryErr))
			return
		}
		if result.agent.Status != agent.ResultCompleted {
			manager.failRecord(state, record, "agent_"+string(result.agent.Failure), errors.New(nonempty(result.agent.Diagnostic, result.agent.Summary)))
			return
		}
		record.presentation.Phase = fix.PhaseWaitingVerifier
		record.presentation.CurrentAction = "Waiting for a verifier slot"
		manager.bump(state, record, "agent_completed", nil)
	case workerVerifier:
		manager.applyVerification(record, result)
		manager.reconcileConflicts(state)
		manager.bump(state, record, "verification_completed", nil)
	}
}

// handleCandidatePrepared is the durable boundary between trusted candidate
// construction and untrusted runtime launch. The identity is journaled before
// any adapter can execute.
func (manager *Manager) handleCandidatePrepared(state *controllerState, record *jobRecord, result workerResult) {
	record.cancel = nil
	if result.err != nil {
		state.agentsRunning--
		if record.presentation.Phase == fix.PhaseCanceling && errors.Is(result.err, context.Canceled) {
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.Attention = fix.AttentionRequired
			record.presentation.CurrentAction = "Canceled before candidate creation"
			record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
			manager.bump(state, record, "canceled", nil)
			return
		}
		manager.failRecord(state, record, "candidate_prepare", result.err)
		return
	}
	if result.candidate == nil {
		state.agentsRunning--
		manager.failRecord(state, record, "candidate_prepare", errors.New("candidate service returned no identity"))
		return
	}
	identity := *result.candidate
	record.candidate = &identity
	record.presentation.CurrentAction = "Candidate prepared; recording ownership"
	if !manager.bump(state, record, "candidate_prepared", nil) {
		state.agentsRunning--
		return
	}
	if record.presentation.Phase == fix.PhaseCanceling || state.shuttingDown {
		state.agentsRunning--
		record.presentation.Phase = fix.PhaseAwaitingAction
		record.presentation.Attention = fix.AttentionRequired
		record.presentation.CurrentAction = "Canceled; candidate retained for review"
		record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
		manager.bump(state, record, "canceled", nil)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel = cancel
	record.presentation.CurrentAction = "Starting agent"
	if !manager.bump(state, record, "agent_started", nil) {
		state.agentsRunning--
		record.cancel = nil
		cancel()
		return
	}
	go manager.runAgent(ctx, record.draft, record.presentation.ID, record.attempt, identity)
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
	case workerPublish:
		return phase == fix.PhasePublishing || phase == fix.PhaseReconciling
	}
	return false
}

func (manager *Manager) applyDiffInventory(record *jobRecord, diff candidate.DiffSnapshot) {
	record.baseScope = diff.Scope
	record.presentation.Scope = diff.Scope
	record.diffHash = diff.Fingerprint
	record.diffPaths = make(map[fix.RepoPath]bool, len(diff.Files)*2)
	for _, file := range diff.Files {
		if file.Path != "" {
			record.diffPaths[file.Path] = true
		}
		if file.Previous != "" {
			record.diffPaths[file.Previous] = true
		}
	}
}

func (manager *Manager) handlePublicationResult(state *controllerState, record *jobRecord, result workerResult) {
	record.cancel = nil
	record.delivery = result.delivery
	record.published = result.published
	if result.delivery.Ambiguous || result.published.Ambiguous {
		record.presentation.Delivery = fix.DeliveryAmbiguous
	} else if result.published.URL != "" {
		record.presentation.Delivery = fix.DeliveryPullRequest
	} else if result.delivery.Pushed {
		record.presentation.Delivery = fix.DeliveryPushed
	} else if result.delivery.Commit != "" {
		record.presentation.Delivery = fix.DeliveryCommitted
	}
	if record.presentation.Phase == fix.PhaseCanceling {
		record.presentation.Phase = fix.PhaseAwaitingAction
		record.presentation.Attention = fix.AttentionRequired
		record.presentation.CurrentAction = "Publication canceled; known external state retained"
		record.presentation.Issue = &fix.JobIssue{Code: "publication_canceled", Summary: "Publication canceled and can be reconciled"}
		manager.bump(state, record, "publication_canceled", publicationPayload(record, result.step))
		return
	}
	if result.err != nil {
		if result.delivery.Ambiguous || result.published.Ambiguous {
			record.presentation.Attention = fix.AttentionBlocking
			record.presentation.Issue = &fix.JobIssue{Code: "publication_ambiguous", Summary: "Publication state is ambiguous", Detail: result.err.Error()}
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.CurrentAction = "Publication requires reconciliation"
			manager.bump(state, record, "publication_ambiguous", publicationPayload(record, result.step))
			return
		}
		manager.failRecord(state, record, "publication_failed", result.err)
		return
	}
	if !manager.bump(state, record, "publication_step_completed", publicationPayload(record, result.step)) {
		return
	}
	manager.startNextPublication(state, record)
}

func publicationPayload(record *jobRecord, step publicationStep) json.RawMessage {
	return mustJSON(map[string]any{"step": step, "commit": record.delivery.Commit, "local_ref": record.delivery.LocalRef,
		"remote_ref": record.delivery.RemoteRef, "repository": record.delivery.Repository, "pushed": record.delivery.Pushed, "pull_request": record.published.ProviderID,
		"ambiguous": record.delivery.Ambiguous || record.published.Ambiguous})
}

func (manager *Manager) applyVerification(record *jobRecord, result workerResult) {
	manager.applyDiffInventory(record, result.diff)
	if result.diff.Scope == fix.ScopeViolated || result.diff.Scope == fix.ScopeConflicted {
		record.presentation.Attention = fix.AttentionBlocking
	}
	for index := range record.presentation.Targets {
		for _, verified := range result.verify.Files {
			if record.presentation.Targets[index].Path != verified.Path {
				continue
			}
			score := verified.Score
			record.presentation.Targets[index].VerifiedScore = &score
			record.presentation.Targets[index].Verification = verified.Diagnostic
			record.presentation.Targets[index].Metrics = metricValues(verified.Metrics)
		}
	}
	if result.verify.Complete && result.verify.Compliant && result.verify.Stable() {
		record.presentation.Compliance = fix.ComplianceCompliant
	} else {
		record.presentation.Compliance = fix.ComplianceNoncompliant
		record.presentation.Attention = fix.AttentionRequired
	}
	if record.draft.ValidationPlanID == "" {
		record.presentation.Validation = fix.ValidationNotConfigured
	} else if result.validation.Passed && result.validation.Stable() {
		record.presentation.Validation = fix.ValidationPassed
	} else {
		record.presentation.Validation = fix.ValidationFailed
		record.presentation.Attention = fix.AttentionRequired
	}
	record.presentation.Phase = fix.PhaseAwaitingReview
	record.presentation.CurrentAction = "Candidate ready for review"
}

func (manager *Manager) handleEvent(state *controllerState, event agent.Event) {
	record := state.jobs[event.JobID]
	if record == nil || record.attempt != event.AttemptID || (record.presentation.Phase != fix.PhasePreparing && record.presentation.Phase != fix.PhaseRunning) {
		return
	}
	if event.Kind == agent.EventStarted || record.presentation.Phase == fix.PhasePreparing {
		record.presentation.Phase = fix.PhaseRunning
	}
	if event.Summary != "" {
		record.presentation.CurrentAction = sanitizeSummary(event.Summary)
	}
	if event.Kind == agent.EventWarning {
		record.presentation.WarningCount++
	}
	if event.ActorID != "" {
		if record.actors == nil {
			record.actors = map[string]bool{}
		}
		if len(record.actors) < 32 {
			record.actors[event.ActorID] = true
		}
		record.presentation.ActorCount = len(record.actors)
	}
	entry := LogEntry{At: event.At, Kind: event.Kind, Summary: sanitizeSummary(event.Summary)}
	record.logs = append(record.logs, entry)
	if overflow := len(record.logs) - manager.options.MaxTranscriptItems; overflow > 0 {
		record.logs = append([]LogEntry(nil), record.logs[overflow:]...)
		record.truncated = true
	}
	record.eventsSinceCheckpoint++
	if record.eventsSinceCheckpoint >= manager.options.TranscriptCheckpointEvents {
		manager.flushTranscript(state, event.JobID, event.AttemptID)
	} else {
		manager.changedRecord(state, record)
	}
}

func (manager *Manager) flushTranscript(state *controllerState, job fix.JobID, attempt fix.AttemptID) {
	record := state.jobs[job]
	if record == nil || record.attempt != attempt || record.eventsSinceCheckpoint == 0 {
		return
	}
	record.eventsSinceCheckpoint = 0
	manager.bump(state, record, "transcript_checkpoint", nil)
}

func (manager *Manager) handleCommand(state *controllerState, call commandCall) {
	if manager.journalFailed {
		call.response <- commandResponse{err: errors.New("fix job journal is unavailable")}
		return
	}
	record := state.jobs[call.command.JobID]
	if record == nil {
		call.response <- commandResponse{err: ErrJobNotFound}
		return
	}
	if receipt, exists := record.commands[call.command.RequestID]; exists {
		receipt.Duplicate = true
		call.response <- commandResponse{receipt: receipt}
		return
	}
	if call.command.RequestID == "" {
		call.response <- commandResponse{err: errors.New("job command requires request id")}
		return
	}
	if call.command.ExpectedRevision != 0 && call.command.ExpectedRevision != record.presentation.Revision {
		call.response <- commandResponse{err: ErrStaleRevision}
		return
	}
	if !hasAction(record.presentation.AllowedActions, call.command.Action) {
		call.response <- commandResponse{err: ErrActionNotAllowed}
		return
	}
	if err := manager.appendRecord(call.ctx, record, "command_intent", mustJSON(call.command)); err != nil {
		call.response <- commandResponse{err: fmt.Errorf("persist job command intent: %w", err)}
		return
	}
	previousPresentation := clonePresentation(record.presentation)
	previousDiffHash, previousBaseScope := record.diffHash, record.baseScope
	previousAcknowledged := cloneAcknowledgements(record.acknowledged)
	previousConflicts := cloneAcknowledgements(record.conflicts)
	previousDiffPaths := make(map[fix.RepoPath]bool, len(record.diffPaths))
	for path := range record.diffPaths {
		previousDiffPaths[path] = true
	}
	var cancelWorker context.CancelFunc
	var discardIdentity *fix.CandidateIdentity
	startPublication := false
	releaseTargets := false
	switch call.command.Action {
	case fix.ActionCancel:
		record.presentation.Phase = fix.PhaseCanceling
		record.presentation.CurrentAction = "Canceling"
		if record.cancel != nil {
			cancelWorker = record.cancel
		} else {
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.Attention = fix.AttentionRequired
			record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled before execution"}
		}
	case fix.ActionRetry, fix.ActionResume:
		if record.delivery.Commit != "" {
			record.presentation.Phase = fix.PhasePublishing
			record.presentation.CurrentAction = "Resuming publication from journaled state"
			startPublication = true
		} else {
			record.presentation.Phase = fix.PhaseQueued
			record.presentation.CurrentAction = "Waiting for an agent slot"
			record.presentation.Compliance = fix.ComplianceUnknown
			record.presentation.Validation = fix.ValidationNotRun
			record.presentation.Scope = fix.ScopeUnknown
			record.diffHash = ""
			record.diffPaths = nil
			record.baseScope = fix.ScopeUnknown
			for index := range record.presentation.Targets {
				record.presentation.Targets[index].VerifiedScore = nil
				record.presentation.Targets[index].Verification = ""
			}
		}
		record.presentation.Attention = fix.AttentionNone
		record.presentation.Issue = nil
	case fix.ActionKeep:
		record.presentation.Phase = fix.PhaseCompleted
		record.presentation.CurrentAction = "Candidate retained"
		record.presentation.FinishedAt = manager.options.Clock()
		releaseTargets = true
	case fix.ActionArchive:
		record.presentation.Phase = fix.PhaseArchived
		record.presentation.CurrentAction = "Archived"
		releaseTargets = true
	case fix.ActionDiscard, fix.ActionCleanup:
		if record.candidate == nil {
			record.presentation.Phase = fix.PhaseDiscarded
			record.presentation.CurrentAction = "No candidate to discard"
			releaseTargets = true
		} else {
			record.presentation.Phase = fix.PhaseDiscarding
			record.presentation.CurrentAction = "Discarding candidate"
			identity := *record.candidate
			discardIdentity = &identity
		}
	case fix.ActionPublish:
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.CurrentAction = "Starting journaled publication"
		startPublication = true
	case fix.ActionAcknowledgeConflict:
		if record.acknowledged == nil {
			record.acknowledged = map[fix.JobID]string{}
		}
		for other, fingerprintPair := range record.conflicts {
			record.acknowledged[other] = fingerprintPair
		}
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Current overlapping candidate manifests acknowledged"
	}
	record.presentation.Revision++
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	if err := manager.appendRecord(call.ctx, record, "command_applied", mustJSON(call.command)); err != nil {
		record.presentation = previousPresentation
		record.diffHash, record.baseScope, record.diffPaths = previousDiffHash, previousBaseScope, previousDiffPaths
		record.acknowledged = previousAcknowledged
		record.conflicts = previousConflicts
		manager.changed(state)
		call.response <- commandResponse{err: fmt.Errorf("persist applied job command: %w", err)}
		return
	}
	receipt := CommandReceipt{RequestID: call.command.RequestID, JobID: call.command.JobID, Accepted: true, Revision: record.presentation.Revision}
	record.commands[call.command.RequestID] = receipt
	if releaseTargets {
		manager.releaseReservations(state, record)
		manager.reconcileConflicts(state)
	}
	manager.changed(state)
	if cancelWorker != nil {
		cancelWorker()
	}
	if discardIdentity != nil {
		state.otherRunning++
		identity := *discardIdentity
		discardCtx, discardCancel := context.WithCancel(context.Background())
		record.cancel = discardCancel
		go func(job fix.JobID, attempt fix.AttemptID) {
			err := manager.deps.Candidates.Discard(discardCtx, identity)
			manager.results <- workerResult{kind: workerDiscard, job: job, attempt: attempt, err: err}
		}(record.presentation.ID, record.attempt)
	}
	if startPublication {
		manager.startNextPublication(state, record)
	}
	call.response <- commandResponse{receipt: receipt}
}

func (manager *Manager) handleShutdown(state *controllerState, call shutdownCall) {
	state.shutdownWaiters = append(state.shutdownWaiters, call.response)
	if state.shuttingDown {
		return
	}
	state.shuttingDown = true
	for _, record := range state.jobs {
		if record.cancel != nil {
			record.presentation.Phase = fix.PhaseCanceling
			record.presentation.CurrentAction = "Canceling for shutdown"
			record.cancel()
		} else if record.presentation.Phase == fix.PhaseQueued || record.presentation.Phase == fix.PhaseWaitingVerifier {
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.Attention = fix.AttentionRequired
			record.presentation.CurrentAction = "Stopped before shutdown"
		}
	}
	manager.changed(state)
}

func (manager *Manager) startNextPublication(state *controllerState, record *jobRecord) {
	if manager.deps.Delivery == nil || record.candidate == nil {
		manager.failRecord(state, record, "publication_unavailable", errors.New("publication service or candidate is unavailable"))
		return
	}
	var step publicationStep
	switch {
	case record.delivery.Ambiguous:
		step = publicationReconcile
		record.presentation.Phase = fix.PhaseReconciling
		record.presentation.CurrentAction = "Reconciling exact remote ref"
	case record.delivery.Commit == "":
		step = publicationCommit
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.CurrentAction = "Creating candidate commit"
	case record.delivery.LocalRef == "":
		step = publicationLocalRef
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.CurrentAction = "Creating absent local branch"
	case !record.delivery.Pushed:
		step = publicationRemoteRef
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.CurrentAction = "Creating and verifying absent remote branch"
	case record.draft.DeliveryMode == fix.DeliveryModePullRequest && record.published.URL == "":
		step = publicationPullRequest
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.CurrentAction = "Creating or reconciling draft pull request"
	default:
		record.presentation.Phase = fix.PhaseCompleted
		record.presentation.Attention = fix.AttentionNone
		record.presentation.CurrentAction = "Publication completed"
		record.presentation.FinishedAt = manager.options.Clock()
		manager.releaseReservations(state, record)
		manager.bump(state, record, "publication_completed", publicationPayload(record, step))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel = cancel
	state.otherRunning++
	if !manager.bump(state, record, "publication_step_started", mustJSON(map[string]any{"step": step})) {
		state.otherRunning--
		record.cancel = nil
		cancel()
		return
	}
	draft := cloneSubmit(SubmitRequest{Draft: record.draft}).Draft
	identity := *record.candidate
	delivered, published := record.delivery, record.published
	go manager.runPublicationStep(ctx, step, draft, record.presentation.ID, record.attempt, identity, record.diffHash, delivered, published)
}

func (manager *Manager) failRecord(state *controllerState, record *jobRecord, code string, err error) {
	record.presentation.Phase = fix.PhaseAwaitingAction
	record.presentation.Attention = fix.AttentionRequired
	record.presentation.CurrentAction = "Action required"
	record.presentation.Issue = &fix.JobIssue{Code: code, Summary: sanitizeSummary(err.Error())}
	manager.bump(state, record, code, mustJSON(map[string]string{"error": sanitizeSummary(err.Error())}))
}

func (manager *Manager) bump(state *controllerState, record *jobRecord, kind string, data json.RawMessage) bool {
	record.presentation.Revision++
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	if err := manager.appendRecord(context.Background(), record, kind, data); err != nil {
		if record.cancel != nil {
			record.cancel()
		}
		record.presentation.Phase = fix.PhaseAwaitingAction
		record.presentation.Attention = fix.AttentionBlocking
		record.presentation.Issue = &fix.JobIssue{Code: "journal_failed", Summary: "Job journal failed", Detail: err.Error()}
		manager.changed(state)
		return false
	}
	manager.changed(state)
	return !manager.journalFailed
}

func (manager *Manager) changedRecord(state *controllerState, record *jobRecord) {
	record.presentation.Revision++
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	manager.changed(state)
}

func (manager *Manager) changed(state *controllerState) {
	state.globalRevision++
	if !manager.journalFailed && manager.journalRecords >= len(state.jobs)+manager.options.JournalCompactRecords {
		if err := manager.compact(state); err != nil {
			manager.journalFailed = true
			for _, record := range state.jobs {
				if record.cancel != nil {
					record.cancel()
				}
				if !isQuiescent(record.presentation.Phase) {
					record.presentation.Phase = fix.PhaseAwaitingAction
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Paused because durable checkpoint failed"
					record.presentation.Issue = &fix.JobIssue{Code: "journal_failed", Summary: "Job journal checkpoint failed", Detail: sanitizeSummary(err.Error())}
					record.presentation.Revision++
				}
			}
		}
	}
	manager.publish(state)
	manager.notifyMu.Lock()
	close(manager.notify)
	manager.notify = make(chan struct{})
	manager.notifyMu.Unlock()
}

func (manager *Manager) publish(state *controllerState) {
	presentations := make([]fix.JobPresentation, 0, len(state.jobs))
	logValues := make(map[fix.JobID]logSnapshot, len(state.jobs))
	for _, id := range state.order {
		if record := state.jobs[id]; record != nil {
			presentations = append(presentations, clonePresentation(record.presentation))
			logValues[id] = logSnapshot{entries: append([]LogEntry(nil), record.logs...), truncated: record.truncated}
		}
	}
	sortPresentations(presentations)
	manager.current.Store(JobListSnapshot{Revision: state.globalRevision, Jobs: presentations})
	manager.logs.Store(logValues)
}

func (manager *Manager) appendRecord(ctx context.Context, record *jobRecord, kind string, data json.RawMessage) error {
	if manager.journalFailed {
		return errors.New("fix job journal is unavailable")
	}
	envelope := manager.journalEnvelope(record, kind, data)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = manager.deps.Store.Append(ctx, jobstore.Record{At: manager.options.Clock(), JobID: record.presentation.ID,
		Revision: record.presentation.Revision, Kind: kind, Data: encoded})
	if err == nil {
		manager.journalRecords++
	}
	return err
}

func (manager *Manager) journalEnvelope(record *jobRecord, kind string, data json.RawMessage) journalEnvelope {
	diffPaths := make([]fix.RepoPath, 0, len(record.diffPaths))
	for path := range record.diffPaths {
		diffPaths = append(diffPaths, path)
	}
	sort.Slice(diffPaths, func(i, j int) bool { return diffPaths[i] < diffPaths[j] })
	envelope := journalEnvelope{Payload: data, Presentation: clonePresentation(record.presentation), Draft: cloneSubmit(SubmitRequest{Draft: record.draft}).Draft,
		Candidate: record.candidate, DiffHash: record.diffHash, DiffPaths: diffPaths, BaseScope: record.baseScope,
		Acknowledged: cloneAcknowledgements(record.acknowledged), Delivery: record.delivery, Published: record.published,
		Attempt: record.attempt, Commands: cloneReceipts(record.commands)}
	if kind != "agent_event" {
		envelope.Transcript = &transcriptCheckpoint{Entries: append([]LogEntry(nil), record.logs...), Truncated: record.truncated}
	}
	return envelope
}

func (manager *Manager) compact(state *controllerState) error {
	records := make([]jobstore.Record, 0, len(state.order))
	for _, id := range state.order {
		record := state.jobs[id]
		if record == nil {
			continue
		}
		envelope := manager.journalEnvelope(record, "checkpoint", nil)
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return fmt.Errorf("encode job checkpoint: %w", err)
		}
		records = append(records, jobstore.Record{At: manager.options.Clock(), JobID: id,
			Revision: record.presentation.Revision, Kind: "checkpoint", Data: encoded})
	}
	if err := manager.deps.Store.Compact(context.Background(), records); err != nil {
		return fmt.Errorf("compact fix job journal: %w", err)
	}
	manager.journalRecords = len(records)
	return nil
}

type journalEnvelope struct {
	Payload      json.RawMessage                  `json:"payload,omitempty"`
	Presentation fix.JobPresentation              `json:"presentation"`
	Draft        FixDraft                         `json:"draft"`
	Candidate    *fix.CandidateIdentity           `json:"candidate,omitempty"`
	DiffHash     string                           `json:"diff_hash,omitempty"`
	DiffPaths    []fix.RepoPath                   `json:"diff_paths,omitempty"`
	BaseScope    fix.ScopeState                   `json:"base_scope,omitempty"`
	Acknowledged map[fix.JobID]string             `json:"acknowledged,omitempty"`
	Delivery     delivery.Result                  `json:"delivery,omitempty"`
	Published    publisher.Result                 `json:"published,omitempty"`
	Attempt      fix.AttemptID                    `json:"attempt,omitempty"`
	Commands     map[fix.CommandID]CommandReceipt `json:"commands,omitempty"`
	Transcript   *transcriptCheckpoint            `json:"transcript,omitempty"`
}

type transcriptCheckpoint struct {
	Entries   []LogEntry `json:"entries,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

func cloneReceipts(source map[fix.CommandID]CommandReceipt) map[fix.CommandID]CommandReceipt {
	if len(source) == 0 {
		return nil
	}
	result := make(map[fix.CommandID]CommandReceipt, len(source))
	for id, receipt := range source {
		result[id] = receipt
	}
	return result
}

func (manager *Manager) restore(state *controllerState) {
	for _, stored := range manager.initial {
		var envelope journalEnvelope
		if json.Unmarshal(stored.Data, &envelope) != nil || envelope.Presentation.ID == "" {
			continue
		}
		record := state.jobs[stored.JobID]
		if record == nil {
			record = &jobRecord{commands: map[fix.CommandID]CommandReceipt{}}
			state.jobs[stored.JobID] = record
			state.order = append(state.order, stored.JobID)
		}
		record.presentation = clonePresentation(envelope.Presentation)
		record.draft = cloneSubmit(SubmitRequest{Draft: envelope.Draft}).Draft
		record.attempt = envelope.Attempt
		if envelope.Candidate != nil {
			identity := *envelope.Candidate
			record.candidate = &identity
		}
		record.diffHash = envelope.DiffHash
		record.baseScope = envelope.BaseScope
		record.diffPaths = make(map[fix.RepoPath]bool, len(envelope.DiffPaths))
		for _, path := range envelope.DiffPaths {
			record.diffPaths[path] = true
		}
		record.acknowledged = make(map[fix.JobID]string, len(envelope.Acknowledged))
		for other, pair := range envelope.Acknowledged {
			record.acknowledged[other] = pair
		}
		record.delivery = envelope.Delivery
		record.published = envelope.Published
		if envelope.Commands != nil {
			record.commands = cloneReceipts(envelope.Commands)
		}
		if envelope.Transcript != nil {
			record.logs = append([]LogEntry(nil), envelope.Transcript.Entries...)
			record.truncated = envelope.Transcript.Truncated
		}
		if stored.Kind == "agent_event" {
			var entry LogEntry
			if json.Unmarshal(envelope.Payload, &entry) == nil {
				record.logs = append(record.logs, entry)
				if overflow := len(record.logs) - manager.options.MaxTranscriptItems; overflow > 0 {
					record.logs = append([]LogEntry(nil), record.logs[overflow:]...)
					record.truncated = true
				}
			}
		}
		if stored.Kind == "command_applied" {
			var command fix.JobCommand
			if json.Unmarshal(envelope.Payload, &command) == nil && command.RequestID != "" {
				record.commands[command.RequestID] = CommandReceipt{RequestID: command.RequestID, JobID: command.JobID, Accepted: true, Revision: stored.Revision}
			}
		}
	}
	for _, record := range state.jobs {
		if record.candidate == nil && record.presentation.Phase != fix.PhaseDiscarded {
			request := candidate.PrepareRequest{Job: record.presentation.ID, Workspace: record.draft.Workspace, Targets: record.draft.Targets,
				AllowedScope: record.draft.ChangeScope, AllowedPaths: record.draft.AllowedPaths}
			identity, found, err := manager.deps.Candidates.DiscoverPrepared(context.Background(), request)
			if err != nil {
				record.presentation.Phase = fix.PhaseAwaitingAction
				record.presentation.Attention = fix.AttentionBlocking
				record.presentation.CurrentAction = "Prepared candidate discovery failed"
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_discovery", Summary: sanitizeSummary(err.Error())}
			} else if found {
				record.candidate = &identity
				record.presentation.Revision++
				record.presentation.UpdatedAt = manager.options.Clock()
				if err := manager.appendRecord(context.Background(), record, "candidate_prepared_recovered", nil); err != nil {
					manager.journalFailed = true
					record.presentation.Phase = fix.PhaseAwaitingAction
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Recovered candidate could not be journaled"
					record.presentation.Issue = &fix.JobIssue{Code: "journal_failed", Summary: "Candidate recovery was not durably recorded", Detail: sanitizeSummary(err.Error())}
				}
			}
		}
		if record.candidate != nil {
			if record.presentation.Phase == fix.PhaseDiscarding {
				err := manager.deps.Candidates.ReconcileDiscard(context.Background(), *record.candidate)
				if err == nil {
					record.candidate = nil
					record.diffHash, record.diffPaths = "", nil
					record.presentation.Phase = fix.PhaseDiscarded
					record.presentation.Attention = fix.AttentionNone
					record.presentation.CurrentAction = "Candidate discard recovered"
					record.presentation.FinishedAt = manager.options.Clock()
					record.presentation.Revision++
				} else {
					record.presentation.Phase = fix.PhaseAwaitingAction
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Candidate discard recovery failed"
					record.presentation.Issue = &fix.JobIssue{Code: "discard_recovery", Summary: sanitizeSummary(err.Error())}
				}
			} else if err := manager.deps.Candidates.Recover(context.Background(), *record.candidate, record.draft.Targets, record.draft.ChangeScope, record.draft.AllowedPaths); err != nil {
				record.presentation.Phase = fix.PhaseAwaitingAction
				record.presentation.Attention = fix.AttentionRequired
				record.presentation.CurrentAction = "Candidate recovery failed"
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_recovery", Summary: err.Error()}
			} else if diff, err := manager.deps.Candidates.Diff(context.Background(), *record.candidate); err != nil {
				record.presentation.Phase = fix.PhaseAwaitingAction
				record.presentation.Attention = fix.AttentionBlocking
				record.presentation.CurrentAction = "Candidate inventory recovery failed"
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_inventory", Summary: sanitizeSummary(err.Error())}
			} else {
				manager.applyDiffInventory(record, diff)
			}
		}
		if !isQuiescent(record.presentation.Phase) {
			record.presentation.Phase = fix.PhaseAwaitingAction
			record.presentation.Attention = fix.AttentionRequired
			record.presentation.CurrentAction = "Interrupted by previous shutdown"
			record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job was interrupted and may be retried"}
			record.presentation.Revision++
		}
		manager.refreshActions(record)
		if record.presentation.Issue != nil && (record.presentation.Issue.Code == "candidate_recovery" || record.presentation.Issue.Code == "candidate_inventory" || record.presentation.Issue.Code == "candidate_discovery" || record.presentation.Issue.Code == "discard_recovery" || record.presentation.Issue.Code == "journal_failed") {
			record.presentation.AllowedActions = []fix.JobAction{fix.ActionArchive}
		}
		if record.presentation.Phase != fix.PhaseCompleted && record.presentation.Phase != fix.PhaseArchived && record.presentation.Phase != fix.PhaseDiscarded {
			for _, target := range record.draft.Targets {
				state.reservations[reservationKey(record.draft.Workspace, target)] = record.presentation.ID
			}
		}
		if state.globalRevision < GlobalRevision(record.presentation.Revision) {
			state.globalRevision = GlobalRevision(record.presentation.Revision)
		}
	}
	manager.reconcileConflicts(state)
	manager.initial = nil
}

func (manager *Manager) reconcileConflicts(state *controllerState) {
	var retained []*jobRecord
	for _, record := range state.jobs {
		if record.candidate == nil || len(record.diffPaths) == 0 || record.presentation.Phase == fix.PhaseDiscarded {
			continue
		}
		beforeScope, beforeCount := record.presentation.Scope, record.presentation.ConflictCount
		record.presentation.Scope = record.baseScope
		record.presentation.ConflictCount = 0
		record.conflicts = map[fix.JobID]string{}
		if record.presentation.Issue != nil && record.presentation.Issue.Code == "candidate_overlap" {
			record.presentation.Issue = nil
			if record.presentation.Compliance == fix.ComplianceNoncompliant || record.presentation.Validation == fix.ValidationFailed {
				record.presentation.Attention = fix.AttentionRequired
			} else {
				record.presentation.Attention = fix.AttentionNone
			}
		}
		if beforeScope != record.presentation.Scope || beforeCount != 0 {
			record.presentation.Revision++
		}
		manager.refreshActions(record)
		retained = append(retained, record)
	}
	for left := 0; left < len(retained); left++ {
		for right := left + 1; right < len(retained); right++ {
			if !pathsIntersect(retained[left].diffPaths, retained[right].diffPaths) {
				continue
			}
			leftRecord, rightRecord := retained[left], retained[right]
			leftRecord.conflicts[rightRecord.presentation.ID] = leftRecord.diffHash + "\x00" + rightRecord.diffHash
			rightRecord.conflicts[leftRecord.presentation.ID] = rightRecord.diffHash + "\x00" + leftRecord.diffHash
			for _, record := range []*jobRecord{leftRecord, rightRecord} {
				record.presentation.Scope = fix.ScopeConflicted
				record.presentation.ConflictCount++
				record.presentation.Attention = fix.AttentionBlocking
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_overlap", Summary: "Candidate overlaps another retained fix job"}
				record.presentation.Revision++
				manager.refreshActions(record)
			}
		}
	}
}

func pathsIntersect(left, right map[fix.RepoPath]bool) bool {
	if len(left) > len(right) {
		left, right = right, left
	}
	for path := range left {
		if right[path] {
			return true
		}
	}
	return false
}

func (manager *Manager) releaseReservations(state *controllerState, record *jobRecord) {
	for _, target := range record.draft.Targets {
		key := reservationKey(record.draft.Workspace, target)
		if state.reservations[key] == record.presentation.ID {
			delete(state.reservations, key)
		}
	}
}

func reservationKey(workspace fix.WorkspaceIdentity, target fix.RepoPath) string {
	repository := string(workspace.Repository)
	if repository == "" {
		repository = workspace.RepositoryRoot
	}
	return repository + "\x00" + target.String()
}

func baselineTargets(contract fix.ScoringContract) []fix.FilePresentation {
	result := make([]fix.FilePresentation, 0, len(contract.Targets))
	for _, target := range contract.Targets {
		result = append(result, fix.FilePresentation{Path: target.Path, BaselineScore: target.Score, Metrics: metricValues(target.Metrics)})
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

func goalLabel(draft FixDraft) string {
	return fmt.Sprintf("score ≤ %.1f", draft.TargetScore)
}

func allowedActions(value fix.JobPresentation) []fix.JobAction {
	switch value.Phase {
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing, fix.PhaseRunning, fix.PhaseWaitingVerifier, fix.PhaseVerifying, fix.PhasePublishing, fix.PhaseReconciling:
		return []fix.JobAction{fix.ActionCancel}
	case fix.PhaseAwaitingAction:
		return []fix.JobAction{fix.ActionRetry, fix.ActionArchive, fix.ActionDiscard}
	case fix.PhaseAwaitingReview:
		return []fix.JobAction{fix.ActionRetry, fix.ActionKeep, fix.ActionArchive, fix.ActionDiscard}
	case fix.PhaseCompleted:
		return []fix.JobAction{fix.ActionArchive, fix.ActionDiscard}
	case fix.PhaseArchived:
		return []fix.JobAction{fix.ActionCleanup}
	}
	return nil
}

func (manager *Manager) refreshActions(record *jobRecord) {
	actions := allowedActions(record.presentation)
	if record.presentation.Phase == fix.PhaseAwaitingReview && record.presentation.Scope == fix.ScopeConflicted && !allConflictsAcknowledged(record) {
		actions = append([]fix.JobAction{fix.ActionAcknowledgeConflict}, actions...)
	}
	publisherReady := record.draft.DeliveryMode != fix.DeliveryModePullRequest || manager.deps.Publisher != nil
	validationReady := record.draft.DeliveryMode != fix.DeliveryModePullRequest || record.presentation.Validation == fix.ValidationPassed
	if record.presentation.Phase == fix.PhaseAwaitingReview && manager.deps.Delivery != nil && publisherReady && validationReady && record.candidate != nil &&
		record.draft.DeliveryMode != "" && record.draft.DeliveryMode != fix.DeliveryModeCandidate &&
		record.presentation.Compliance == fix.ComplianceCompliant && (record.presentation.Scope == fix.ScopeClean || record.presentation.Scope == fix.ScopeConflicted && allConflictsAcknowledged(record)) &&
		(record.presentation.Validation == fix.ValidationPassed || record.presentation.Validation == fix.ValidationNotConfigured) {
		actions = append([]fix.JobAction{fix.ActionPublish}, actions...)
	}
	record.presentation.AllowedActions = actions
}

func cloneAcknowledgements(source map[fix.JobID]string) map[fix.JobID]string {
	if source == nil {
		return nil
	}
	result := make(map[fix.JobID]string, len(source))
	for job, fingerprint := range source {
		result[job] = fingerprint
	}
	return result
}

func allConflictsAcknowledged(record *jobRecord) bool {
	if len(record.conflicts) == 0 {
		return true
	}
	for other, pair := range record.conflicts {
		if record.acknowledged[other] != pair {
			return false
		}
	}
	return true
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
		if target.VerifiedScore != nil {
			score := *target.VerifiedScore
			result.Targets[index].VerifiedScore = &score
		}
	}
	result.AllowedActions = append([]fix.JobAction(nil), value.AllowedActions...)
	if value.Issue != nil {
		issue := *value.Issue
		result.Issue = &issue
	}
	return result
}

func cloneJobList(value JobListSnapshot) JobListSnapshot {
	result := JobListSnapshot{Revision: value.Revision, Jobs: make([]fix.JobPresentation, len(value.Jobs))}
	for index, job := range value.Jobs {
		result.Jobs[index] = clonePresentation(job)
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

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

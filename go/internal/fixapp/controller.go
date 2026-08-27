package fixapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/appconfig"
	"github.com/blater/slopmochi/internal/candidate"
	"github.com/blater/slopmochi/internal/delivery"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixanalysis"
	"github.com/blater/slopmochi/internal/fixprompt"
	"github.com/blater/slopmochi/internal/jobstore"
	"github.com/blater/slopmochi/internal/publisher"
	"github.com/blater/slopmochi/internal/sourcepath"
)

func (manager *Manager) run() {
	state := &controllerState{
		jobs: map[fix.JobID]*jobRecord{}, reservations: map[string]fix.JobID{},
	}
	manager.restore(state)
	close(manager.ready)
	sharedRefresh := time.NewTicker(time.Second)
	defer sharedRefresh.Stop()
	defer func() {
		manager.closed.Store(true)
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
			case runCall:
				manager.handleRun(state, value)
			case commandCall:
				manager.handleCommand(state, value)
			case candidateCall:
				record, found := state.jobs[value.id]
				if found && record.candidate != nil {
					value.response <- candidateResponse{identity: *record.candidate,
						previewBytes: record.input.Preferences.Concurrency.MaxCandidatePreviewBytes,
						previewLines: record.input.Preferences.Concurrency.MaxCandidatePreviewLines, ok: true}
				} else {
					value.response <- candidateResponse{}
				}
			case jobsCall:
				value.response <- jobList(state, value.filter)
			case transcriptCall:
				value.response <- manager.transcriptPage(state, value.id, value.cursor, value.limit)
			case shutdownCall:
				manager.handleShutdown(state, value)
			case reconfigureCall:
				manager.handleReconfigure(state, value)
			}
		case update := <-manager.events:
			if update.prompt != nil {
				manager.handlePrompt(state, update.job, update.attempt, *update.prompt)
			} else if update.barrier != nil {
				close(update.barrier)
			} else {
				manager.handleEvent(state, update.event)
			}
		case result := <-manager.results:
			manager.handleResult(state, result)
		case <-sharedRefresh.C:
			manager.refreshSharedJobs(state)
		}
	}
}

func (manager *Manager) handleReconfigure(state *controllerState, call reconfigureCall) {
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

func (manager *Manager) handleRun(state *controllerState, call runCall) {
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
	manager.refreshSharedJobs(state)
	input := call.input
	for _, key := range reservationKeys(input) {
		if owner, exists := state.reservations[key]; exists {
			call.response <- runResponse{err: fmt.Errorf("%w: selected files overlap running job %s", ErrTargetReserved, owner)}
			return
		}
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
	now := manager.options.Clock()
	presentation := fix.JobPresentation{
		ID: id, Phase: fix.PhaseQueued, Attention: fix.AttentionNone,
		ProfileLabel: input.Profile.Label, ProfileID: string(input.Profile.ID), ModelLabel: string(input.Model), EffortLabel: string(input.Effort),
		Goal: goalLabel(input), Targets: baselineTargets(input.Baseline.Contract), CurrentAction: "Waiting for an agent slot",
		AttemptOrdinal: 1,
		CreatedAt:      now, UpdatedAt: now, TargetStatus: fix.ScorePending,
		Scope: fix.ScopeUnknown, Delivery: fix.DeliveryNone,
		DeliveryPlan: input.DeliveryPlan, BranchName: input.BranchName,
	}
	record := &jobRecord{input: cloneFixInput(input), presentation: presentation, commands: map[fix.CommandID]CommandReceipt{}, runLock: runLock, runsHere: true}
	manager.refreshActions(record)
	if err := manager.saveRecord(call.ctx, record); err != nil {
		_ = runLock.Close()
		call.response <- runResponse{err: fmt.Errorf("persist fix start: %w", err)}
		return
	}
	state.jobs[id] = record
	state.order = append(state.order, id)
	manager.logJobStart(record)
	for _, key := range reservationKeys(input) {
		state.reservations[key] = id
	}
	manager.changed(state)
	call.response <- runResponse{id: id}
}

func (manager *Manager) schedule(state *controllerState) {
	if state.shuttingDown {
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
		if record.input.DeliveryPlan.Workspace == fix.WorkspaceCurrent {
			record.presentation.CurrentAction = "Preparing current files"
		} else {
			record.presentation.CurrentAction = "Creating worktree"
		}
		record.presentation.Attention = fix.AttentionNone
		record.presentation.Issue = nil
		ctx, cancel := context.WithCancel(context.Background())
		record.cancel = cancel
		state.agentsRunning++
		if record.candidate == nil {
			if !manager.bump(state, record) {
				state.agentsRunning--
				record.cancel = nil
				cancel()
				continue
			}
			go manager.runCandidatePrepare(ctx, record.input, record.presentation.ID, attempt)
			continue
		}
		record.presentation.CurrentAction = "Starting agent"
		if !manager.bump(state, record) {
			state.agentsRunning--
			record.cancel = nil
			cancel()
			continue
		}
		go manager.runAgent(ctx, record.input, record.nextAttemptNotes, record.presentation.ID, attempt, *record.candidate)
	}
	for state.verifiersRunning < manager.options.MaxVerifiers {
		record := firstInPhase(state, fix.PhaseWaitingVerifier)
		if record == nil {
			break
		}
		record.presentation.Phase = fix.PhaseVerifying
		record.presentation.CurrentAction = "Re-analyzing candidate"
		ctx, cancel := context.WithCancel(context.Background())
		record.cancel = cancel
		state.verifiersRunning++
		if !manager.bump(state, record) {
			state.verifiersRunning--
			record.cancel = nil
			cancel()
			continue
		}
		go manager.runVerifier(ctx, record.input, record.presentation.ID, record.attempt, *record.candidate)
	}
}

func (manager *Manager) runCandidatePrepare(ctx context.Context, input FixInput, job fix.JobID, attempt fix.AttemptID) {
	identity, err := manager.deps.Candidates.Prepare(ctx, candidate.PrepareRequest{Job: job, Workspace: input.Workspace,
		Mode: input.DeliveryPlan.Workspace, Targets: input.Targets, AllowedScope: input.ChangeScope, AllowedPaths: input.AllowedPaths, CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes})
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
		if record := state.jobs[id]; record != nil && !record.runsElsewhere && record.presentation.Phase == phase {
			return record
		}
	}
	return nil
}

func (manager *Manager) runAgent(ctx context.Context, input FixInput, nextAttemptNotes string, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	select {
	case manager.events <- agentUpdate{event: agent.Event{JobID: job, AttemptID: attempt, At: manager.options.Clock(), Kind: agent.EventActivity, Summary: "Candidate ready; starting agent"}}:
	case <-manager.done:
		return
	}
	strategy, err := manager.deps.Agents.Strategy(input.Profile.Runtime)
	if err != nil {
		manager.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, err: err})
		return
	}
	instructions := input.Instructions
	instructions.NextAttemptNotes = nextAttemptNotes
	manifest, err := prepareTargetManifest(identity, input.Targets)
	if err != nil {
		manager.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, err: fmt.Errorf("prepare target manifest: %w", err)})
		return
	}
	request := agent.Request{
		JobID: job, AttemptID: attempt, Workspace: identity, Model: input.Model, Effort: input.Effort,
		Task: agent.RemediationTask{Targets: cloneContract(input.Baseline.Contract).Targets, Goal: input.Baseline.Contract.Goal,
			Instructions: instructions, Manifest: manifest},
		Write:  agent.WritePolicy{Allowed: append([]fix.RepoPath(nil), input.AllowedPaths...), Scope: input.ChangeScope},
		Limits: agent.Limits{MaxActors: input.Preferences.Concurrency.MaxActorsPerJob},
	}
	prompt := request.Task.EffectivePrompt()
	select {
	case manager.events <- agentUpdate{job: job, attempt: attempt, prompt: &prompt}:
	case <-manager.done:
		return
	}
	result := strategy.Execute(ctx, input.Profile, request, agent.EventSinkFunc(func(event agent.Event) error {
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
	diff, inventoryErr := manager.deps.Candidates.Diff(ctx, identity)
	manager.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, agent: result,
		diff: diff, inventoryErr: inventoryErr})
}

func prepareTargetManifest(identity fix.CandidateIdentity, targets []fix.RepoPath) (*agent.TargetManifest, error) {
	if !fixprompt.RequiresTargetManifest(targets) {
		return nil, nil
	}
	if identity.StagingRoot == "" || !filepath.IsAbs(identity.StagingRoot) {
		return nil, errors.New("candidate staging is unavailable")
	}
	directory := filepath.Join(identity.StagingRoot, "agent-input")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, err
	}
	paths := append([]fix.RepoPath(nil), targets...)
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	lines := make([]string, len(paths))
	for index, path := range paths {
		lines[index] = path.String()
	}
	manifestPath := filepath.Join(directory, "targets.txt")
	if err := os.WriteFile(manifestPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return nil, err
	}
	if err := os.Chmod(manifestPath, 0o600); err != nil {
		return nil, err
	}
	return &agent.TargetManifest{Path: manifestPath, Count: len(paths)}, nil
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

func (manager *Manager) runVerifier(ctx context.Context, input FixInput, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	diff, err := manager.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, err: fmt.Errorf("inventory candidate diff: %w", err)}
		return
	}
	verified, err := manager.deps.Analysis.Verify(ctx, fixanalysis.VerificationRequest{Candidate: identity, Contract: input.Baseline.Contract})
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, err: fmt.Errorf("verify candidate scores: %w", err)}
		return
	}
	finalDiff, err := manager.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, verify: verified, err: fmt.Errorf("re-inventory candidate after verification: %w", err)}
		return
	}
	if finalDiff.Fingerprint != diff.Fingerprint {
		manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: finalDiff, verify: verified, err: errors.New("candidate changed during analysis")}
		return
	}
	manager.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: finalDiff, verify: verified}
}

func publicationRequests(input FixInput, job fix.JobID, identity fix.CandidateIdentity, diffHash string, paths []fix.RepoPath, delivered delivery.Result) (delivery.Request, publisher.Request) {
	commitTitle := renderPublicationTemplate(input.Preferences.Delivery.CommitTitleTemplate, "Refactor {targets} with Slopmochi", input, job)
	commitBody := renderPublicationTemplate(input.Preferences.Delivery.CommitBodyTemplate, "Automated remediation for {goal}.", input, job)
	prTitle := renderPublicationTemplate(input.Preferences.Delivery.PullRequestTitleTemplate, commitTitle, input, job)
	prBody := renderPublicationTemplate(input.Preferences.Delivery.PullRequestBodyTemplate, commitBody, input, job)
	request := delivery.Request{Job: job, Candidate: identity, DiffHash: diffHash, Plan: input.DeliveryPlan, Paths: append([]fix.RepoPath(nil), paths...), Branch: input.BranchName,
		Remote: input.Preferences.Delivery.Remote, CommitTitle: commitTitle, CommitBody: commitBody,
		ExpectedRemoteHost: input.DeliveryTarget.RemoteHost, HostRepository: input.DeliveryTarget.HostRepository,
		ExpectedRemoteIdentity: input.DeliveryTarget.RemoteIdentity, CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes}
	pullRequest := publisher.Request{Job: job, Repository: identity.Repository, Candidate: identity, HostRepository: delivered.Repository,
		Remote: input.Preferences.Delivery.Remote, BaseBranch: input.Preferences.Delivery.BaseBranch, HeadBranch: input.BranchName,
		Commit: delivered.Commit, Title: prTitle, Body: prBody, Draft: input.Preferences.Delivery.DraftPullRequests,
		CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes}
	return request, pullRequest
}

func renderPublicationTemplate(template, fallback string, input FixInput, job fix.JobID) string {
	if strings.TrimSpace(template) == "" {
		template = fallback
	}
	return strings.NewReplacer("{targets}", targetLabel(input.Targets), "{goal}", goalLabel(input), "{branch}", input.BranchName, "{job}", string(job)).Replace(template)
}

func (manager *Manager) runPublicationStep(ctx context.Context, step publicationStep, input FixInput, job fix.JobID, attempt fix.AttemptID,
	identity fix.CandidateIdentity, diffHash string, paths []fix.RepoPath, delivered delivery.Result, published publisher.Result) {
	var err error
	target := input.DeliveryTarget
	if step == publicationCommit {
		latest, inventoryErr := manager.deps.Candidates.Diff(ctx, identity)
		if inventoryErr != nil {
			err = fmt.Errorf("check files before commit: %w", inventoryErr)
		} else if latest.Fingerprint != diffHash {
			err = errors.New("files changed after verification")
		}
		current, preflightErr := manager.preflightDelivery(ctx, input.Workspace, input.DeliveryPlan, input.Preferences.Delivery, input.BranchName, true)
		if err == nil {
			err = preflightErr
		}
		if err == nil && target != (delivery.PreflightResult{}) && current != target {
			err = errors.New("delivery target changed since publication began")
		}
		if err == nil {
			target = current
			input.DeliveryTarget = current
		}
	}
	request, pullRequest := publicationRequests(input, job, identity, diffHash, paths, delivered)
	switch step {
	case publicationCommit:
		if err == nil {
			delivered, err = manager.deps.Delivery.CreateCommit(ctx, request)
		}
	case publicationLocalRef:
		delivered, err = manager.deps.Delivery.CreateLocalRef(ctx, request, delivered)
	case publicationRemoteRef:
		delivered, err = manager.deps.Delivery.CreateRemoteRef(ctx, request, delivered)
	case publicationReconcile:
		delivered, err = manager.deps.Delivery.Reconcile(ctx, request, delivered)
	case publicationPRReconcile:
		if manager.deps.Publisher == nil {
			err = errors.New("pull request publisher is unavailable")
		} else {
			pullRequest.Commit = delivered.Commit
			published, err = manager.deps.Publisher.Reconcile(ctx, pullRequest, published)
		}
	case publicationPullRequest:
		if manager.deps.Publisher == nil {
			err = errors.New("pull request publisher is unavailable")
		} else {
			pullRequest.Commit = delivered.Commit
			published, err = manager.deps.Publisher.Create(ctx, pullRequest)
		}
	}
	manager.results <- workerResult{kind: workerPublish, job: job, attempt: attempt, delivery: delivered, deliveryTarget: target, published: published, err: err}
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
	case workerCleanup:
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
	if result.kind == workerAgent {
		if reference := strings.TrimSpace(result.agent.SessionReference); reference != "" && !containsText(record.agentReferences, reference) {
			record.agentReferences = append(record.agentReferences, reference)
		}
		detail := nonempty(result.agent.Summary, result.agent.Diagnostic, string(result.agent.Status))
		manager.appendJobText(record.presentation.ID, fmt.Sprintf("%s  %-16s %s\n", manager.options.Clock().Format(time.RFC3339Nano), "agent_result", cleanLogText(detail)))
	}
	if result.kind == workerPublish {
		manager.handlePublicationResult(state, record, result)
		return
	}
	if result.kind == workerCleanup {
		if record.presentation.Phase == fix.PhaseCanceling {
			manager.finishCancellation(state, record)
			return
		}
		if errors.Is(result.err, context.Canceled) && record.presentation.Issue != nil && record.presentation.Issue.Code == "interrupted" {
			record.presentation.Phase = fix.PhaseFailed
			record.presentation.Attention = fix.AttentionInfo
			record.presentation.CurrentAction = "Stopped while finishing"
			record.presentation.Issue = &fix.JobIssue{Code: "cleanup_interrupted", Summary: "Finishing stopped during shutdown"}
			manager.releaseReservations(state, record)
			manager.bump(state, record)
			return
		}
		record.presentation.Phase = fix.PhaseCompleted
		record.presentation.FinishedAt = manager.options.Clock()
		manager.releaseReservations(state, record)
		if result.err == nil {
			record.candidate = nil
			record.diffHash, record.diffPaths = "", nil
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Done"
			record.presentation.Issue = nil
			manager.bump(state, record)
			return
		}
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Done; worktree cleanup failed"
		record.presentation.Issue = &fix.JobIssue{Code: "cleanup_failed", Summary: sanitizeSummary(result.err.Error())}
		manager.bump(state, record)
		return
	}
	if record.presentation.Phase == fix.PhaseCanceling && result.kind != workerDiscard {
		manager.finishCancellation(state, record)
		return
	}
	if result.kind == workerDiscard {
		if result.err == nil {
			wasCanceled := record.presentation.Issue != nil && record.presentation.Issue.Code == "canceled"
			record.candidate = nil
			record.diffHash, record.diffPaths = "", nil
			record.presentation.Phase = fix.PhaseDiscarded
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Candidate discarded"
			if wasCanceled {
				record.presentation.Phase = fix.PhaseCanceled
				record.presentation.CurrentAction = "Canceled"
			}
			record.presentation.FinishedAt = manager.options.Clock()
			manager.releaseReservations(state, record)
			manager.reconcileConflicts(state)
			manager.bump(state, record)
			return
		}
		if record.presentation.Issue != nil && record.presentation.Issue.Code == "canceled" {
			record.presentation.Phase = fix.PhaseCanceled
			record.presentation.Attention = fix.AttentionInfo
			record.presentation.CurrentAction = "Canceled; cleanup can be retried"
			record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(result.err.Error())}
			record.presentation.FinishedAt = manager.options.Clock()
			manager.releaseReservations(state, record)
			manager.reconcileConflicts(state)
			manager.bump(state, record)
			return
		}
		if errors.Is(result.err, context.Canceled) {
			record.presentation.Phase = fix.PhaseFailed
			record.presentation.Attention = fix.AttentionError
			record.presentation.CurrentAction = "Discard interrupted; candidate state will be reconciled"
			record.presentation.Issue = &fix.JobIssue{Code: "discard_interrupted", Summary: "Candidate discard was interrupted"}
			manager.bump(state, record)
			return
		}
		manager.failRecord(state, record, "discard_failed", result.err)
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
		manager.bump(state, record)
	case workerVerifier:
		manager.applyVerification(record, result)
		manager.reconcileConflicts(state)
		if record.presentation.TargetStatus != fix.TargetMet {
			record.presentation.AttemptOrdinal++
			record.presentation.Phase = fix.PhaseQueued
			record.presentation.Attention = fix.AttentionNone
			retryActivity := fmt.Sprintf("Retry attempt %d queued: target score not met", record.presentation.AttemptOrdinal)
			record.presentation.CurrentAction = retryActivity
			retryEntry := LogEntry{At: manager.options.Clock(), Kind: agent.EventActivity, Summary: retryActivity}
			record.logs = append(record.logs, retryEntry)
			manager.appendJobText(record.presentation.ID, formatJobActivity(retryEntry))
			record.presentation.Issue = nil
			record.presentation.TargetStatus = fix.ScorePending
			manager.bump(state, record)
			return
		}
		record.presentation.Phase = fix.PhasePublishing
		record.presentation.Attention = fix.AttentionNone
		if record.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
			record.presentation.CurrentAction = "Finishing"
		} else {
			record.presentation.CurrentAction = "Committing changes"
		}
		record.presentation.Issue = nil
		if manager.bump(state, record) {
			if record.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
				manager.startCandidateCleanup(state, record, *record.candidate)
			} else {
				manager.startNextPublication(state, record)
			}
		}
	}
}

func containsText(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (manager *Manager) finishCancellation(state *controllerState, record *jobRecord) {
	record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
	record.presentation.Attention = fix.AttentionNone
	manager.releaseReservations(state, record)
	if record.candidate == nil {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.CurrentAction = "Canceled"
		record.presentation.FinishedAt = manager.options.Clock()
		manager.reconcileConflicts(state)
		manager.bump(state, record)
		return
	}
	record.presentation.Phase = fix.PhaseDiscarding
	record.presentation.CurrentAction = "Canceling; cleaning up"
	if !manager.bump(state, record) {
		return
	}
	manager.startCandidateDiscard(state, record, *record.candidate)
}

func (manager *Manager) startCandidateDiscard(state *controllerState, record *jobRecord, identity fix.CandidateIdentity) {
	if manager.deps.Candidates == nil {
		record.presentation.Phase = fix.PhaseCanceled
		record.presentation.Attention = fix.AttentionInfo
		record.presentation.CurrentAction = "Canceled; cleanup unavailable"
		record.presentation.FinishedAt = manager.options.Clock()
		manager.releaseReservations(state, record)
		manager.reconcileConflicts(state)
		manager.bump(state, record)
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

func (manager *Manager) startCandidateCleanup(state *controllerState, record *jobRecord, identity fix.CandidateIdentity) {
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
// construction and untrusted runtime launch. The identity is saved before
// any adapter can execute.
func (manager *Manager) handleCandidatePrepared(state *controllerState, record *jobRecord, result workerResult) {
	record.cancel = nil
	if result.err != nil {
		state.agentsRunning--
		if record.presentation.Phase == fix.PhaseCanceling && errors.Is(result.err, context.Canceled) {
			manager.finishCancellation(state, record)
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
	record.presentation.WorkspacePath = identity.RepositoryRoot
	record.presentation.CurrentAction = "Candidate prepared; recording ownership"
	if !manager.bump(state, record) {
		state.agentsRunning--
		return
	}
	if record.presentation.Phase == fix.PhaseCanceling {
		state.agentsRunning--
		manager.finishCancellation(state, record)
		return
	}
	if state.shuttingDown {
		state.agentsRunning--
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionError
		record.presentation.CurrentAction = "Interrupted during shutdown; candidate retained for recovery"
		record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job was interrupted"}
		manager.bump(state, record)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel = cancel
	record.presentation.CurrentAction = "Starting agent"
	if !manager.bump(state, record) {
		state.agentsRunning--
		record.cancel = nil
		cancel()
		return
	}
	go manager.runAgent(ctx, record.input, record.nextAttemptNotes, record.presentation.ID, record.attempt, identity)
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

func (manager *Manager) applyDiffInventory(record *jobRecord, diff candidate.DiffSnapshot) {
	existing := make(map[fix.RepoPath]fix.FilePresentation, len(record.presentation.Targets))
	for _, file := range record.presentation.Targets {
		existing[file.Path] = file
	}
	record.baseScope = diff.Scope
	record.presentation.Scope = diff.Scope
	record.diffHash = diff.Fingerprint
	record.presentation.DiffFingerprint = diff.Fingerprint
	record.diffPaths = make(map[fix.RepoPath]bool, len(diff.Files)*2)
	files := make(map[fix.RepoPath]fix.FilePresentation, len(record.presentation.Targets)+len(diff.Files))
	for _, file := range baselineTargets(record.input.Baseline.Contract) {
		if prior, ok := existing[file.Path]; ok {
			file.VerifiedScore = prior.VerifiedScore
			file.VerifiedMetrics = append([]fix.MetricValue(nil), prior.VerifiedMetrics...)
			file.Verification = prior.Verification
		}
		files[file.Path] = file
	}
	for _, file := range diff.Files {
		if file.Path != "" {
			record.diffPaths[file.Path] = true
		}
		if file.Previous != "" {
			record.diffPaths[file.Previous] = true
		}
		if !sourcepath.IsSourceFile(file.Path.String()) {
			continue
		}
		projected, exists := files[file.Path]
		if !exists {
			projected = fix.FilePresentation{Path: file.Path, Classification: "supporting"}
			if prior, ok := existing[file.Path]; ok {
				projected = prior
			}
		}
		projected.Changed = true
		projected.ChangeStatus = file.Status
		projected.PreviousPath = file.Previous
		files[file.Path] = projected
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path.String())
	}
	sort.Strings(paths)
	record.presentation.Targets = make([]fix.FilePresentation, 0, len(paths))
	for _, path := range paths {
		record.presentation.Targets = append(record.presentation.Targets, files[fix.RepoPath(path)])
	}
}

func (manager *Manager) handlePublicationResult(state *controllerState, record *jobRecord, result workerResult) {
	record.cancel = nil
	record.publicationStep = ""
	record.delivery = result.delivery
	if result.deliveryTarget != (delivery.PreflightResult{}) {
		record.input.DeliveryTarget = result.deliveryTarget
	}
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
		if result.delivery.Ambiguous || result.published.Ambiguous {
			record.presentation.Phase = fix.PhaseReconciling
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Checking canceled delivery"
			record.presentation.Issue = &fix.JobIssue{Code: "publication_canceled", Summary: "Cancellation stopped publication after an external change may have occurred"}
			if manager.bump(state, record) {
				manager.startNextPublication(state, record)
			}
			return
		}
		// Persist every known local and remote side effect before the isolated
		// workspace is eligible for cleanup.
		if !manager.bump(state, record) {
			return
		}
		manager.finishCancellation(state, record)
		return
	}
	if result.err != nil {
		if result.delivery.Ambiguous || result.published.Ambiguous {
			record.presentation.Attention = fix.AttentionBlocking
			record.presentation.Issue = &fix.JobIssue{Code: "publication_ambiguous", Summary: "Publication state is ambiguous", Detail: result.err.Error()}
			record.presentation.Phase = fix.PhaseFailed
			record.presentation.CurrentAction = "Publication requires reconciliation"
			manager.bump(state, record)
			return
		}
		manager.failRecord(state, record, "publication_failed", result.err)
		return
	}
	if !manager.bump(state, record) {
		return
	}
	manager.startNextPublication(state, record)
}

func (manager *Manager) applyVerification(record *jobRecord, result workerResult) {
	manager.applyDiffInventory(record, result.diff)
	for index := range record.presentation.Targets {
		for _, verified := range result.verify.Files {
			if record.presentation.Targets[index].Path != verified.Path {
				continue
			}
			score := verified.Score
			record.presentation.Targets[index].VerifiedScore = &score
			record.presentation.Targets[index].Verification = verified.Diagnostic
			record.presentation.Targets[index].VerifiedMetrics = metricValues(verified.Metrics)
		}
	}
	if result.verify.Complete && result.verify.TargetMet && result.verify.Stable() {
		record.presentation.TargetStatus = fix.TargetMet
	} else {
		record.presentation.TargetStatus = fix.TargetNotMet
		record.presentation.Attention = fix.AttentionError
	}
	if record.presentation.TargetStatus == fix.TargetNotMet {
		record.nextAttemptNotes = nextAttemptNotes(record, result)
	} else {
		record.nextAttemptNotes = ""
	}
}

func nextAttemptNotes(record *jobRecord, result workerResult) string {
	parts := []string{fmt.Sprintf("attempt %d", record.presentation.AttemptOrdinal)}
	diagnostics := make([]string, 0, 2)
	if record.presentation.TargetStatus == fix.TargetNotMet {
		parts = append(parts, "target score not met")
		if diagnostic := boundedRetryDiagnostic(result.verify.Diagnostic); diagnostic != "" {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	for _, verified := range result.verify.Files {
		measurements := []string{fmt.Sprintf("SCORE %.1f/%.1f", verified.Score, record.input.TargetScore)}
		seen := map[fix.MetricID]bool{}
		for _, focus := range record.input.Focus {
			if metric, ok := verified.Metrics[focus.Metric]; ok {
				measurements = append(measurements, fmt.Sprintf("%s %.1f/%.1f", metricLabel(metric, focus.Metric), metric.Value, focus.Maximum))
				seen[focus.Metric] = true
			}
		}
		regressionIDs := make([]fix.MetricID, 0, len(record.input.Baseline.Contract.Goal.AllowedRegression))
		for metric := range record.input.Baseline.Contract.Goal.AllowedRegression {
			if !seen[metric] {
				regressionIDs = append(regressionIDs, metric)
			}
		}
		sort.Slice(regressionIDs, func(i, j int) bool { return regressionIDs[i] < regressionIDs[j] })
		for _, id := range regressionIDs {
			metric, ok := verified.Metrics[id]
			if !ok {
				continue
			}
			limit := regressionLimit(record.input.Baseline.Contract, verified.Path, id)
			measurements = append(measurements, fmt.Sprintf("%s %.1f/%.1f", metricLabel(metric, id), metric.Value, limit))
		}
		line := fmt.Sprintf("%s: %s", verified.Path, strings.Join(measurements, ", "))
		if diagnostic := boundedRetryDiagnostic(verified.Diagnostic); diagnostic != "" {
			line += " (" + diagnostic + ")"
		}
		parts = append(parts, line)
	}
	parts = append(parts, diagnostics...)
	return sanitizeSummary(strings.Join(parts, "; "))
}

func metricLabel(metric fix.MetricValue, id fix.MetricID) string {
	if strings.TrimSpace(metric.Label) != "" {
		return metric.Label
	}
	return string(id)
}

func regressionLimit(contract fix.ScoringContract, path fix.RepoPath, metric fix.MetricID) float64 {
	for _, target := range contract.Targets {
		if target.Path == path {
			return target.Metrics[metric].Value + contract.Goal.AllowedRegression[metric]
		}
	}
	return contract.Goal.AllowedRegression[metric]
}

func boundedRetryDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		value = value[:160] + "…"
	}
	return sanitizeSummary(value)
}

func (manager *Manager) handlePrompt(state *controllerState, job fix.JobID, attempt fix.AttemptID, prompt string) {
	record := state.jobs[job]
	if record == nil || record.attempt != attempt || (record.presentation.Phase != fix.PhasePreparing && record.presentation.Phase != fix.PhaseRunning) {
		return
	}
	manager.logJobPrompt(job, attempt, prompt)
	record.logs = append(record.logs, LogEntry{At: manager.options.Clock(), Kind: "prompt", Summary: cleanLogText(prompt)})
	manager.changedRecord(state, record)
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
	if event.Kind == agent.EventFileChanged && event.Path != "" && sourcepath.IsSourceFile(event.Path.String()) {
		found := false
		for index := range record.presentation.Targets {
			if record.presentation.Targets[index].Path == event.Path {
				record.presentation.Targets[index].Changed = true
				found = true
				break
			}
		}
		if !found {
			record.presentation.Targets = append(record.presentation.Targets, fix.FilePresentation{Path: event.Path, Classification: "provisional", Changed: true})
		}
	}
	if event.ActorID != "" {
		actorLimit := record.input.Preferences.Concurrency.MaxActorsPerJob
		if record.actors == nil {
			record.actors = map[string]bool{}
		}
		if len(record.actors) < actorLimit {
			record.actors[event.ActorID] = true
		}
		record.presentation.ActorCount = len(record.actors)
		updated := false
		for index := range record.presentation.Actors {
			if record.presentation.Actors[index].ID == event.ActorID {
				record.presentation.Actors[index].ParentID = sanitizeSummary(event.ParentActorID)
				if event.Summary != "" {
					record.presentation.Actors[index].CurrentAction = sanitizeSummary(event.Summary)
				}
				updated = true
				break
			}
		}
		if !updated && len(record.presentation.Actors) < actorLimit {
			record.presentation.Actors = append(record.presentation.Actors, fix.ActorPresentation{ID: sanitizeSummary(event.ActorID), ParentID: sanitizeSummary(event.ParentActorID), CurrentAction: sanitizeSummary(event.Summary)})
		}
	}
	if event.Usage != nil {
		record.presentation.UsageReported = true
		if event.Usage.Cumulative {
			record.presentation.Usage = fix.UsagePresentation{InputTokens: event.Usage.InputTokens, CachedTokens: event.Usage.CachedTokens, OutputTokens: event.Usage.OutputTokens, ReasoningTokens: event.Usage.ReasoningTokens}
		} else {
			record.presentation.Usage.InputTokens += event.Usage.InputTokens
			record.presentation.Usage.CachedTokens += event.Usage.CachedTokens
			record.presentation.Usage.OutputTokens += event.Usage.OutputTokens
			record.presentation.Usage.ReasoningTokens += event.Usage.ReasoningTokens
		}
	}
	entry := LogEntry{At: event.At, Kind: event.Kind, Summary: cleanLogText(event.Summary), ActorID: sanitizeSummary(event.ActorID), ParentActorID: sanitizeSummary(event.ParentActorID)}
	if event.Usage != nil {
		usage := *event.Usage
		entry.Usage = &usage
	}
	record.logs = append(record.logs, entry)
	manager.appendJobText(record.presentation.ID, formatJobActivity(entry))
	manager.changedRecord(state, record)
}

func cleanLogText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func formatJobActivity(entry LogEntry) string {
	actor := ""
	if entry.ActorID != "" {
		actor = " [" + entry.ActorID + "]"
	}
	text := strings.ReplaceAll(entry.Summary, "\n", "\n    ")
	return fmt.Sprintf("%s  %-16s%s %s\n", entry.At.Format(time.RFC3339Nano), entry.Kind, actor, text)
}

func (manager *Manager) handleCommand(state *controllerState, call commandCall) {
	record := state.jobs[call.command.JobID]
	if record == nil {
		call.response <- commandResponse{err: ErrJobNotFound}
		return
	}
	if record.runsElsewhere {
		call.response <- commandResponse{err: errors.New("job is running in another Slopmochi window")}
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
	if !hasAction(record.presentation.AllowedActions, call.command.Action) {
		call.response <- commandResponse{err: ErrActionNotAllowed}
		return
	}
	previousPresentation := clonePresentation(record.presentation)
	previousCanceled := record.canceled
	var cancelWorker context.CancelFunc
	var discardIdentity *fix.CandidateIdentity
	reconcileCanceled := false
	switch call.command.Action {
	case fix.ActionCancel:
		record.canceled = true
		record.presentation.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
		if record.cancel != nil {
			record.presentation.Phase = fix.PhaseCanceling
			record.presentation.CurrentAction = "Canceling"
			cancelWorker = record.cancel
		} else if record.delivery.Ambiguous || record.published.Ambiguous {
			record.presentation.Phase = fix.PhaseReconciling
			record.presentation.CurrentAction = "Checking canceled delivery"
			reconcileCanceled = true
		} else if record.candidate != nil {
			record.presentation.Phase = fix.PhaseDiscarding
			record.presentation.CurrentAction = "Canceling; cleaning up"
			identity := *record.candidate
			discardIdentity = &identity
		} else {
			record.presentation.Phase = fix.PhaseCanceled
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Canceled"
			record.presentation.FinishedAt = manager.options.Clock()
		}
	}
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	receipt := CommandReceipt{RequestID: call.command.RequestID, JobID: call.command.JobID, Accepted: true}
	record.commands[call.command.RequestID] = receipt
	if err := manager.saveRecord(call.ctx, record); err != nil {
		record.presentation = previousPresentation
		record.canceled = previousCanceled
		delete(record.commands, call.command.RequestID)
		manager.changed(state)
		call.response <- commandResponse{err: fmt.Errorf("persist job command: %w", err)}
		return
	}
	manager.releaseReservations(state, record)
	manager.reconcileConflicts(state)
	manager.changed(state)
	if cancelWorker != nil {
		cancelWorker()
	}
	if discardIdentity != nil {
		manager.startCandidateDiscard(state, record, *discardIdentity)
	}
	if reconcileCanceled {
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
			// Shutdown interrupts work; it is not a user cancellation. Keep the
			// current phase so the returned worker result is saved normally and
			// any candidate or publication state remains recoverable on restart.
			record.presentation.CurrentAction = "Stopping for shutdown"
			record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job was interrupted by shutdown"}
			record.cancel()
		} else if record.presentation.Phase == fix.PhaseQueued || record.presentation.Phase == fix.PhaseWaitingVerifier {
			record.presentation.Phase = fix.PhaseFailed
			record.presentation.Attention = fix.AttentionError
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
	if record.canceled {
		switch {
		case record.delivery.Ambiguous:
			step = publicationReconcile
			record.presentation.Phase = fix.PhaseReconciling
			record.presentation.CurrentAction = "Checking canceled branch delivery"
		case record.published.Ambiguous:
			step = publicationPRReconcile
			record.presentation.Phase = fix.PhaseReconciling
			record.presentation.CurrentAction = "Checking canceled pull request delivery"
		default:
			manager.finishCancellation(state, record)
			return
		}
	} else {
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
		case record.input.DeliveryPlan.Publish == fix.PublishLocal:
			if manager.deps.Candidates == nil {
				record.presentation.Phase = fix.PhaseCompleted
				record.presentation.CurrentAction = "Done"
				record.presentation.FinishedAt = manager.options.Clock()
				manager.releaseReservations(state, record)
				manager.bump(state, record)
				return
			}
			record.presentation.Phase = fix.PhasePublishing
			record.presentation.CurrentAction = "Finishing"
			if manager.bump(state, record) {
				manager.startCandidateCleanup(state, record, *record.candidate)
			}
			return
		case !record.delivery.Pushed:
			step = publicationRemoteRef
			record.presentation.Phase = fix.PhasePublishing
			record.presentation.CurrentAction = "Creating and verifying absent remote branch"
		case record.input.DeliveryPlan.Publish == fix.PublishPullRequest && record.published.Ambiguous:
			step = publicationPRReconcile
			record.presentation.Phase = fix.PhaseReconciling
			record.presentation.CurrentAction = "Reconciling exact pull request identity"
		case record.input.DeliveryPlan.Publish == fix.PublishPullRequest && record.published.URL == "":
			step = publicationPullRequest
			record.presentation.Phase = fix.PhasePublishing
			record.presentation.CurrentAction = "Creating or reconciling input pull request"
		default:
			if manager.deps.Candidates == nil {
				record.presentation.Phase = fix.PhaseCompleted
				record.presentation.Attention = fix.AttentionNone
				record.presentation.CurrentAction = "Done"
				record.presentation.FinishedAt = manager.options.Clock()
				manager.releaseReservations(state, record)
				manager.bump(state, record)
				return
			}
			record.presentation.Phase = fix.PhasePublishing
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Finishing"
			if manager.bump(state, record) {
				manager.startCandidateCleanup(state, record, *record.candidate)
			}
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel = cancel
	record.publicationStep = step
	state.otherRunning++
	if !manager.bump(state, record) {
		state.otherRunning--
		record.cancel = nil
		cancel()
		return
	}
	input := cloneFixInput(record.input)
	identity := *record.candidate
	delivered, published := record.delivery, record.published
	go manager.runPublicationStep(ctx, step, input, record.presentation.ID, record.attempt, identity, record.diffHash, sortedDiffPaths(record.diffPaths), delivered, published)
}

func sortedDiffPaths(values map[fix.RepoPath]bool) []fix.RepoPath {
	result := make([]fix.RepoPath, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (manager *Manager) failRecord(state *controllerState, record *jobRecord, code string, err error) {
	record.presentation.Phase = fix.PhaseFailed
	record.presentation.Attention = fix.AttentionError
	record.presentation.CurrentAction = "Failed"
	record.presentation.Issue = &fix.JobIssue{Code: code, Summary: sanitizeSummary(err.Error())}
	manager.releaseReservations(state, record)
	manager.bump(state, record)
}

func (manager *Manager) bump(state *controllerState, record *jobRecord) bool {
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	if err := manager.saveRecord(context.Background(), record); err != nil {
		if record.cancel != nil {
			record.cancel()
		}
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.Attention = fix.AttentionBlocking
		record.presentation.Issue = &fix.JobIssue{Code: "state_save_failed", Summary: "Job state could not be saved", Detail: err.Error()}
		manager.changed(state)
		return false
	}
	manager.changed(state)
	return true
}

func (manager *Manager) changedRecord(state *controllerState, record *jobRecord) {
	record.presentation.UpdatedAt = manager.options.Clock()
	manager.refreshActions(record)
	if !record.runsElsewhere && manager.deps.Store != nil {
		_ = manager.saveRecord(context.Background(), record)
	}
	manager.changed(state)
}

func (manager *Manager) changed(state *controllerState) {
	for _, record := range state.jobs {
		manager.logJobResult(record)
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

func jobList(state *controllerState, filter JobFilter) JobListSnapshot {
	presentations := make([]fix.JobPresentation, 0, len(state.jobs))
	for _, id := range state.order {
		if record := state.jobs[id]; record != nil {
			if !filter.IncludeFinished && record.presentation.Phase == fix.PhaseCompleted {
				continue
			}
			if filter.ActiveOnly && isQuiescent(record.presentation.Phase) {
				continue
			}
			presentations = append(presentations, clonePresentation(record.presentation))
		}
	}
	sortPresentations(presentations)
	return JobListSnapshot{Jobs: presentations}
}

func (manager *Manager) transcriptPage(state *controllerState, id fix.JobID, cursor LogCursor, limit int) transcriptResponse {
	record := state.jobs[id]
	if record == nil {
		return transcriptResponse{err: ErrJobNotFound}
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if manager.options.JobIndexPath != "" {
		contents, err := os.ReadFile(manager.jobTextLogPath(id))
		if err == nil {
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
			return transcriptResponse{page: LogPage{Entries: entries, Next: LogCursor(end), Complete: end == len(lines)}}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return transcriptResponse{err: err}
		}
	}
	start := max(0, min(int(cursor), len(record.logs)))
	end := min(len(record.logs), start+limit)
	return transcriptResponse{page: LogPage{
		Entries: append([]LogEntry(nil), record.logs[start:end]...), Next: LogCursor(end), Complete: end == len(record.logs),
	}}
}

func (manager *Manager) saveRecord(ctx context.Context, record *jobRecord) error {
	state := manager.storedJobState(record)
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return manager.deps.Store.Save(ctx, jobstore.Record{JobID: record.presentation.ID, UpdatedAt: record.presentation.UpdatedAt, State: encoded})
}

func (manager *Manager) storedJobState(record *jobRecord) storedJobState {
	diffPaths := make([]fix.RepoPath, 0, len(record.diffPaths))
	for path := range record.diffPaths {
		diffPaths = append(diffPaths, path)
	}
	sort.Slice(diffPaths, func(i, j int) bool { return diffPaths[i] < diffPaths[j] })
	return storedJobState{Presentation: clonePresentation(record.presentation), Input: storedJobInputFrom(record.input),
		Candidate: record.candidate, DiffHash: record.diffHash, DiffPaths: diffPaths, BaseScope: record.baseScope,
		Delivery: record.delivery, Published: record.published, Canceled: record.canceled,
		PublicationStep: record.publicationStep, Attempt: record.attempt, Commands: cloneReceipts(record.commands)}
}

type storedJobState struct {
	Presentation    fix.JobPresentation              `json:"presentation"`
	Input           storedJobInput                   `json:"input"`
	Candidate       *fix.CandidateIdentity           `json:"candidate,omitempty"`
	DiffHash        string                           `json:"diff_hash,omitempty"`
	DiffPaths       []fix.RepoPath                   `json:"diff_paths,omitempty"`
	BaseScope       fix.ScopeState                   `json:"base_scope,omitempty"`
	Delivery        delivery.Result                  `json:"delivery,omitempty"`
	Published       publisher.Result                 `json:"published,omitempty"`
	Canceled        bool                             `json:"canceled,omitempty"`
	PublicationStep publicationStep                  `json:"publication_step,omitempty"`
	Attempt         fix.AttemptID                    `json:"attempt,omitempty"`
	Commands        map[fix.CommandID]CommandReceipt `json:"commands,omitempty"`
}

type storedJobInput struct {
	Workspace           fix.WorkspaceIdentity    `json:"workspace"`
	Targets             []fix.RepoPath           `json:"targets"`
	TargetScore         float64                  `json:"target_score"`
	ChangeScope         string                   `json:"change_scope"`
	AllowedPaths        []fix.RepoPath           `json:"allowed_paths"`
	DeliveryPlan        fix.DeliveryPlan         `json:"delivery_plan"`
	DeliveryTarget      delivery.PreflightResult `json:"delivery_target"`
	BranchName          string                   `json:"branch_name"`
	DeliveryPreferences appconfig.Delivery       `json:"delivery_preferences"`
}

func storedJobInputFrom(input FixInput) storedJobInput {
	return storedJobInput{
		Workspace: input.Workspace, Targets: append([]fix.RepoPath(nil), input.Targets...), TargetScore: input.TargetScore,
		ChangeScope: input.ChangeScope, AllowedPaths: append([]fix.RepoPath(nil), input.AllowedPaths...), DeliveryPlan: input.DeliveryPlan,
		DeliveryTarget: input.DeliveryTarget, BranchName: input.BranchName, DeliveryPreferences: input.Preferences.Delivery,
	}
}

func (input storedJobInput) fixInput() FixInput {
	return FixInput{
		Workspace: input.Workspace, Targets: append([]fix.RepoPath(nil), input.Targets...), TargetScore: input.TargetScore,
		ChangeScope: input.ChangeScope, AllowedPaths: append([]fix.RepoPath(nil), input.AllowedPaths...), DeliveryPlan: input.DeliveryPlan,
		DeliveryTarget: input.DeliveryTarget, BranchName: input.BranchName, Preferences: appconfig.Resolved{Delivery: input.DeliveryPreferences},
	}
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

func jobRecordFromStored(stored jobstore.Record) (*jobRecord, bool) {
	var envelope storedJobState
	if json.Unmarshal(stored.State, &envelope) != nil || envelope.Presentation.ID == "" {
		return nil, false
	}
	record := &jobRecord{
		presentation: clonePresentation(envelope.Presentation), input: envelope.Input.fixInput(), attempt: envelope.Attempt,
		commands: cloneReceipts(envelope.Commands), diffHash: envelope.DiffHash, baseScope: envelope.BaseScope,
		delivery: envelope.Delivery, published: envelope.Published, canceled: envelope.Canceled,
		publicationStep: envelope.PublicationStep, storedAt: stored.UpdatedAt,
		resultLogged: isQuiescent(envelope.Presentation.Phase),
	}
	if record.commands == nil {
		record.commands = map[fix.CommandID]CommandReceipt{}
	}
	if record.presentation.AttemptOrdinal <= 0 {
		record.presentation.AttemptOrdinal = 1
	}
	if envelope.Candidate != nil {
		identity := *envelope.Candidate
		record.candidate = &identity
	}
	record.diffPaths = make(map[fix.RepoPath]bool, len(envelope.DiffPaths))
	for _, path := range envelope.DiffPaths {
		record.diffPaths[path] = true
	}
	return record, true
}

func (manager *Manager) restore(state *controllerState) {
	var resumeDelivery []*jobRecord
	for _, stored := range manager.initial {
		record, ok := jobRecordFromStored(stored)
		if !ok {
			continue
		}
		id := record.presentation.ID
		state.jobs[id] = record
		state.order = append(state.order, id)
	}
	for _, record := range state.jobs {
		if !isQuiescent(record.presentation.Phase) {
			runLock, err := manager.deps.Store.Lock(record.presentation.ID)
			if errors.Is(err, jobstore.ErrJobRunning) {
				record.runsElsewhere = true
				for _, key := range reservationKeys(record.input) {
					state.reservations[key] = record.presentation.ID
				}
				continue
			}
			if err != nil {
				record.presentation.Phase = fix.PhaseFailed
				record.presentation.Attention = fix.AttentionError
				record.presentation.CurrentAction = "Could not check running job"
				record.presentation.Issue = &fix.JobIssue{Code: "job_lock", Summary: sanitizeSummary(err.Error())}
				continue
			}
			record.runLock = runLock
			record.runsHere = true
		}
		resumeAfterRestart := record.presentation.Phase == fix.PhasePublishing ||
			record.presentation.Phase == fix.PhaseReconciling ||
			(record.presentation.Phase == fix.PhaseFailed && (record.delivery.Ambiguous || record.published.Ambiguous))
		cancelRecovery := record.presentation.Issue != nil &&
			(record.presentation.Issue.Code == "canceled" &&
				(record.presentation.Phase == fix.PhaseCanceling || record.presentation.Phase == fix.PhaseDiscarding) ||
				record.presentation.Issue.Code == "cancel_cleanup" && record.presentation.Phase == fix.PhaseCanceled)
		if record.canceled {
			switch record.publicationStep {
			case publicationLocalRef, publicationRemoteRef, publicationReconcile:
				record.delivery.Ambiguous = true
				resumeAfterRestart = true
				cancelRecovery = false
			case publicationPullRequest, publicationPRReconcile:
				record.published.Ambiguous = true
				resumeAfterRestart = true
				cancelRecovery = false
			}
		}
		if record.candidate == nil && record.presentation.Phase != fix.PhaseDiscarded {
			request := candidate.PrepareRequest{Job: record.presentation.ID, Workspace: record.input.Workspace, Targets: record.input.Targets,
				Mode: record.input.DeliveryPlan.Workspace, AllowedScope: record.input.ChangeScope, AllowedPaths: record.input.AllowedPaths, CommandOutputBytes: record.input.Preferences.Delivery.CommandOutputBytes}
			identity, found, err := manager.deps.Candidates.DiscoverPrepared(context.Background(), request)
			if err != nil {
				if cancelRecovery {
					record.presentation.Attention = fix.AttentionInfo
					record.presentation.CurrentAction = "Canceled; cleanup could not be checked"
					record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(err.Error())}
				} else {
					record.presentation.Phase = fix.PhaseFailed
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Prepared candidate discovery failed"
					record.presentation.Issue = &fix.JobIssue{Code: "candidate_discovery", Summary: sanitizeSummary(err.Error())}
				}
			} else if found {
				record.candidate = &identity
				record.presentation.UpdatedAt = manager.options.Clock()
				if err := manager.saveRecord(context.Background(), record); err != nil {
					record.presentation.Phase = fix.PhaseFailed
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Recovered candidate could not be saved"
					record.presentation.Issue = &fix.JobIssue{Code: "state_save_failed", Summary: "Candidate recovery was not saved", Detail: sanitizeSummary(err.Error())}
				}
			}
		}
		if cancelRecovery {
			record.presentation.Phase = fix.PhaseCanceled
			record.presentation.Attention = fix.AttentionNone
			record.presentation.CurrentAction = "Canceled"
			if record.candidate != nil {
				if err := manager.deps.Candidates.ReconcileDiscard(context.Background(), *record.candidate); err == nil {
					record.candidate = nil
					record.diffHash, record.diffPaths = "", nil
				} else {
					record.presentation.Attention = fix.AttentionInfo
					record.presentation.CurrentAction = "Canceled; cleanup will retry on startup"
					record.presentation.Issue = &fix.JobIssue{Code: "cancel_cleanup", Summary: sanitizeSummary(err.Error())}
				}
			}
			if record.presentation.FinishedAt.IsZero() {
				record.presentation.FinishedAt = manager.options.Clock()
			}
			record.presentation.UpdatedAt = manager.options.Clock()
			manager.refreshActions(record)
			_ = manager.saveRecord(context.Background(), record)
			continue
		}
		cleanupRecovery := record.presentation.Issue != nil &&
			(record.presentation.Issue.Code == "cleanup_failed" || record.presentation.Issue.Code == "cleanup_interrupted")
		if cleanupRecovery {
			var err error
			if record.candidate != nil {
				err = manager.deps.Candidates.ReconcileDiscard(context.Background(), *record.candidate)
			}
			record.presentation.Phase = fix.PhaseCompleted
			if record.presentation.FinishedAt.IsZero() {
				record.presentation.FinishedAt = manager.options.Clock()
			}
			if err == nil {
				record.candidate = nil
				record.diffHash, record.diffPaths = "", nil
				record.presentation.Attention = fix.AttentionNone
				record.presentation.CurrentAction = "Done"
				record.presentation.Issue = nil
			} else {
				record.presentation.Attention = fix.AttentionInfo
				record.presentation.CurrentAction = "Done; worktree cleanup failed"
				record.presentation.Issue = &fix.JobIssue{Code: "cleanup_failed", Summary: sanitizeSummary(err.Error())}
			}
			record.presentation.UpdatedAt = manager.options.Clock()
			manager.refreshActions(record)
			_ = manager.saveRecord(context.Background(), record)
			continue
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
				} else {
					record.presentation.Phase = fix.PhaseFailed
					record.presentation.Attention = fix.AttentionBlocking
					record.presentation.CurrentAction = "Candidate discard recovery failed"
					record.presentation.Issue = &fix.JobIssue{Code: "discard_recovery", Summary: sanitizeSummary(err.Error())}
				}
			} else if err := manager.deps.Candidates.Recover(context.Background(), *record.candidate, record.input.Targets, record.input.ChangeScope, record.input.AllowedPaths); err != nil {
				record.presentation.Phase = fix.PhaseFailed
				record.presentation.Attention = fix.AttentionError
				record.presentation.CurrentAction = "Candidate recovery failed"
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_recovery", Summary: err.Error()}
			} else if diff, err := manager.deps.Candidates.Diff(context.Background(), *record.candidate); err != nil {
				record.presentation.Phase = fix.PhaseFailed
				record.presentation.Attention = fix.AttentionBlocking
				record.presentation.CurrentAction = "Candidate inventory recovery failed"
				record.presentation.Issue = &fix.JobIssue{Code: "candidate_inventory", Summary: sanitizeSummary(err.Error())}
			} else {
				manager.applyDiffInventory(record, diff)
			}
		}
		if !isQuiescent(record.presentation.Phase) {
			record.presentation.Phase = fix.PhaseFailed
			record.presentation.Attention = fix.AttentionError
			record.presentation.CurrentAction = "Interrupted by previous shutdown"
			record.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "Job stopped during the previous shutdown"}
		}
		manager.refreshActions(record)
		if record.presentation.Issue != nil && (record.presentation.Issue.Code == "candidate_recovery" || record.presentation.Issue.Code == "candidate_inventory" || record.presentation.Issue.Code == "candidate_discovery" || record.presentation.Issue.Code == "discard_recovery" || record.presentation.Issue.Code == "state_save_failed") {
			record.presentation.AllowedActions = nil
		}
		if record.presentation.Phase != fix.PhaseFailed && record.presentation.Phase != fix.PhaseCompleted && record.presentation.Phase != fix.PhaseCanceled && record.presentation.Phase != fix.PhaseDiscarded {
			for _, key := range reservationKeys(record.input) {
				state.reservations[key] = record.presentation.ID
			}
		}
		if resumeAfterRestart && record.candidate != nil &&
			(record.presentation.Issue == nil || record.presentation.Issue.Code == "interrupted" ||
				record.presentation.Issue.Code == "publication_ambiguous" || record.presentation.Issue.Code == "publication_canceled") {
			record.presentation.Attention = fix.AttentionNone
			record.presentation.Issue = nil
			resumeDelivery = append(resumeDelivery, record)
		}
	}
	manager.reconcileConflicts(state)
	for _, record := range state.jobs {
		manager.logJobResult(record)
	}
	for _, record := range resumeDelivery {
		manager.startNextPublication(state, record)
	}
	manager.initial = nil
}

func (manager *Manager) refreshSharedJobs(state *controllerState) {
	storedJobs, err := manager.deps.Store.Load(context.Background())
	if err != nil {
		return
	}
	changed := false
	for _, stored := range storedJobs {
		fresh, ok := jobRecordFromStored(stored)
		if !ok {
			continue
		}
		id := fresh.presentation.ID
		current := state.jobs[id]
		if current != nil && current.runsHere {
			continue
		}
		if current != nil && !stored.UpdatedAt.After(current.storedAt) && !(current.runsElsewhere && !isQuiescent(current.presentation.Phase)) {
			continue
		}
		if !isQuiescent(fresh.presentation.Phase) {
			runLock, lockErr := manager.deps.Store.Lock(id)
			if errors.Is(lockErr, jobstore.ErrJobRunning) {
				fresh.runsElsewhere = true
			} else if lockErr != nil {
				continue
			} else {
				latest, found := manager.latestSharedJob(id)
				if found {
					fresh = latest
				}
				if !isQuiescent(fresh.presentation.Phase) {
					fresh.runLock = runLock
					fresh.runsHere = true
					fresh.presentation.Phase = fix.PhaseFailed
					fresh.presentation.Attention = fix.AttentionError
					fresh.presentation.CurrentAction = "Interrupted"
					fresh.presentation.Issue = &fix.JobIssue{Code: "interrupted", Summary: "The Slopmochi process running this job stopped"}
					fresh.presentation.FinishedAt = manager.options.Clock()
					fresh.presentation.UpdatedAt = fresh.presentation.FinishedAt
					manager.refreshActions(fresh)
					_ = manager.saveRecord(context.Background(), fresh)
				} else {
					_ = runLock.Close()
				}
			}
		}
		state.jobs[id] = fresh
		if current == nil {
			state.order = append(state.order, id)
		}
		changed = true
	}
	if !changed {
		return
	}
	state.reservations = map[string]fix.JobID{}
	for _, record := range state.jobs {
		if !isQuiescent(record.presentation.Phase) {
			for _, key := range reservationKeys(record.input) {
				state.reservations[key] = record.presentation.ID
			}
		}
	}
	manager.changed(state)
}

func (manager *Manager) latestSharedJob(id fix.JobID) (*jobRecord, bool) {
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

func (manager *Manager) reconcileConflicts(state *controllerState) {
	// A target can belong to only one running job, regardless of workspace or Git result.
	// Overlapping files are therefore not a user-action state.
}

func (manager *Manager) releaseReservations(state *controllerState, record *jobRecord) {
	for _, key := range reservationKeys(record.input) {
		if state.reservations[key] == record.presentation.ID {
			delete(state.reservations, key)
		}
	}
}

func reservationKeys(input FixInput) []string {
	paths := input.Targets
	if input.DeliveryPlan.Workspace == fix.WorkspaceCurrent {
		// Current-files agents may perform repository-wide supporting refactors,
		// so two of them must never mutate the same workspace concurrently.
		return []string{reservationRoot(input.Workspace) + "\x00*"}
	}
	keys := make([]string, 0, len(paths))
	for _, path := range paths {
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
		result = append(result, fix.FilePresentation{Path: target.Path, Classification: "target", BaselineScore: target.Score, BaselineMetrics: metrics, Metrics: metrics})
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

func goalLabel(input FixInput) string {
	return fmt.Sprintf("score ≤ %.1f", input.TargetScore)
}

func allowedActions(value fix.JobPresentation) []fix.JobAction {
	switch value.Phase {
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing, fix.PhaseRunning, fix.PhaseWaitingVerifier, fix.PhaseVerifying, fix.PhasePublishing, fix.PhaseReconciling:
		return []fix.JobAction{fix.ActionCancel}
	case fix.PhaseFailed:
		return []fix.JobAction{fix.ActionCancel}
	}
	return nil
}

func (manager *Manager) refreshActions(record *jobRecord) {
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

type jobIndexEntry struct {
	Job       fix.JobID         `json:"job"`
	LogFile   string            `json:"log_file"`
	StartedAt time.Time         `json:"started_at"`
	Targets   []fix.RepoPath    `json:"targets"`
	Profile   string            `json:"profile"`
	Runtime   agent.RuntimeKind `json:"runtime"`
	Model     agent.ModelID     `json:"model"`
	Effort    agent.EffortID    `json:"effort"`
	Score     float64           `json:"target_score"`
	Metrics   []fix.MetricGoal  `json:"metrics,omitempty"`
	Scope     string            `json:"may_edit"`
	Delivery  fix.DeliveryPlan  `json:"delivery"`
	Branch    string            `json:"branch,omitempty"`
	End       *jobIndexEnd      `json:"end,omitempty"`
}

type jobIndexEnd struct {
	At              time.Time             `json:"at"`
	Status          fix.Phase             `json:"status"`
	Result          string                `json:"result"`
	Error           string                `json:"error,omitempty"`
	FilesTouched    []jobIndexTouchedFile `json:"files_touched,omitempty"`
	AgentReferences []string              `json:"agent_references,omitempty"`
	Tokens          fix.UsagePresentation `json:"tokens,omitempty"`
	Git             fix.DeliveryState     `json:"git,omitempty"`
}

type jobIndexTouchedFile struct {
	Path   fix.RepoPath `json:"path"`
	Change string       `json:"change,omitempty"`
}

func (manager *Manager) logJobStart(record *jobRecord) {
	if record == nil || manager.options.JobIndexPath == "" {
		return
	}
	startedAt := record.presentation.CreatedAt
	if startedAt.IsZero() {
		startedAt = manager.options.Clock()
	}
	manager.startJobTextLog(record, startedAt)
	manager.writeJobIndex(manager.indexEntry(record, startedAt))
}

func (manager *Manager) logJobResult(record *jobRecord) {
	if record == nil || record.resultLogged || !isQuiescent(record.presentation.Phase) {
		return
	}
	record.resultLogged = true
	finishedAt := record.presentation.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = manager.options.Clock()
	}
	entry := manager.indexEntry(record, record.presentation.CreatedAt)
	entry.End = &jobIndexEnd{At: finishedAt, Status: record.presentation.Phase, Result: record.presentation.CurrentAction,
		FilesTouched: touchedFiles(record.presentation.Targets), AgentReferences: append([]string(nil), record.agentReferences...),
		Tokens: record.presentation.Usage, Git: record.presentation.Delivery}
	if record.presentation.Issue != nil {
		entry.End.Error = nonempty(record.presentation.Issue.Detail, record.presentation.Issue.Summary)
	}
	manager.appendJobText(record.presentation.ID, formatJobEnd(entry.End))
	manager.writeJobIndex(entry)
}

func (manager *Manager) indexEntry(record *jobRecord, startedAt time.Time) jobIndexEntry {
	if startedAt.IsZero() {
		startedAt = manager.options.Clock()
	}
	return jobIndexEntry{
		Job: record.presentation.ID, LogFile: manager.jobTextLogPath(record.presentation.ID), StartedAt: startedAt,
		Targets: append([]fix.RepoPath(nil), record.input.Targets...), Profile: string(record.input.Profile.ID), Runtime: record.input.Profile.Runtime,
		Model: record.input.Model, Effort: record.input.Effort, Score: record.input.TargetScore,
		Metrics: append([]fix.MetricGoal(nil), record.input.Focus...), Scope: record.input.ChangeScope,
		Delivery: record.input.DeliveryPlan, Branch: record.input.BranchName,
	}
}

func touchedFiles(files []fix.FilePresentation) []jobIndexTouchedFile {
	result := make([]jobIndexTouchedFile, 0)
	for _, file := range files {
		if file.Changed && sourcepath.IsSourceFile(file.Path.String()) {
			result = append(result, jobIndexTouchedFile{Path: file.Path, Change: file.ChangeStatus})
		}
	}
	return result
}

func (manager *Manager) jobTextLogPath(job fix.JobID) string {
	if manager.options.JobIndexPath == "" || job == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.options.JobIndexPath), "fix-jobs", string(job)+".log")
}

func (manager *Manager) startJobTextLog(record *jobRecord, startedAt time.Time) {
	path := manager.jobTextLogPath(record.presentation.ID)
	if path == "" {
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "SLOPMOCHI FIX JOB\nJob: %s\nStarted: %s\nAgent: %s\nRuntime: %s\nModel: %s\nEffort: %s\nTarget score: %g\nMay edit: %s\nDelivery: workspace=%s git=%s publish=%s\n",
		record.presentation.ID, startedAt.Format(time.RFC3339Nano), record.input.Profile.ID, record.input.Profile.Runtime,
		record.input.Model, record.input.Effort, record.input.TargetScore, record.input.ChangeScope,
		record.input.DeliveryPlan.Workspace, record.input.DeliveryPlan.Git, record.input.DeliveryPlan.Publish)
	if record.input.BranchName != "" {
		fmt.Fprintf(&text, "Branch: %s\n", record.input.BranchName)
	}
	text.WriteString("Files:\n")
	for _, target := range record.input.Targets {
		fmt.Fprintf(&text, "  %s\n", target)
	}
	text.WriteByte('\n')
	manager.jobLogMu.Lock()
	defer manager.jobLogMu.Unlock()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, []byte(text.String()), 0o600)
}

func (manager *Manager) logJobPrompt(job fix.JobID, attempt fix.AttemptID, prompt string) {
	manager.appendJobText(job, fmt.Sprintf("PROMPT %s\n%s\nEND PROMPT\n\n", attempt, cleanLogText(prompt)))
}

func (manager *Manager) appendJobText(job fix.JobID, text string) {
	path := manager.jobTextLogPath(job)
	if path == "" || text == "" {
		return
	}
	manager.jobLogMu.Lock()
	defer manager.jobLogMu.Unlock()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(text)
	_ = file.Close()
}

func formatJobEnd(end *jobIndexEnd) string {
	var text strings.Builder
	fmt.Fprintf(&text, "\nRESULT\nFinished: %s\nStatus: %s\nResult: %s\n", end.At.Format(time.RFC3339Nano), end.Status, cleanLogText(end.Result))
	if end.Error != "" {
		fmt.Fprintf(&text, "Error: %s\n", cleanLogText(end.Error))
	}
	if len(end.AgentReferences) > 0 {
		fmt.Fprintf(&text, "Agent references: %s\n", strings.Join(end.AgentReferences, ", "))
	}
	text.WriteString("Files touched:\n")
	for _, file := range end.FilesTouched {
		fmt.Fprintf(&text, "  %s %s\n", file.Change, file.Path)
	}
	return text.String()
}

func (manager *Manager) writeJobIndex(entry jobIndexEntry) {
	path := manager.options.JobIndexPath
	if path == "" {
		return
	}
	manager.jobLogMu.Lock()
	defer manager.jobLogMu.Unlock()
	entries := []jobIndexEntry{}
	if contents, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			var existing jobIndexEntry
			if json.Unmarshal([]byte(line), &existing) == nil && existing.Job != "" {
				entries = append(entries, existing)
			}
		}
	}
	replaced := false
	for index := range entries {
		if entries[index].Job == entry.Job {
			if entry.End != nil {
				entries[index].End = entry.End
			} else {
				entries[index] = entry
			}
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	var output strings.Builder
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for _, value := range entries {
		if encoder.Encode(value) != nil {
			return
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, []byte(output.String()), 0o600)
}

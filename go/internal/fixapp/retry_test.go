package fixapp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestRetryAttemptAndVerifierEvidenceSurviveRestart(t *testing.T) {
	seed, runtime := newTestManager(t, 1)
	draft := prepare(t, seed, "retry.go")
	draft.Instructions.DetachedBody = "Preserve this advanced draft exactly"
	dependencies := seed.deps
	shutdownManager(t, seed)

	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: draft.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job),
		AnalysisRoot: "/candidate/" + string(job), BaseCommit: draft.Workspace.BaseCommit}
	presentation := fix.JobPresentation{
		ID: job, Revision: 7, Phase: fix.PhaseAwaitingReview, Goal: goalLabel(draft), Targets: baselineTargets(draft.Baseline.Contract),
		AttemptOrdinal: 2, CreatedAt: time.Now(), UpdatedAt: time.Now(), Compliance: fix.ComplianceNoncompliant,
		Validation: fix.ValidationNotConfigured, Scope: fix.ScopeClean,
	}
	evidence := "attempt 2; scoring noncompliant; retry.go score 72.0 exceeds target 50.0"
	store := jobstore.NewMemory()
	encoded, err := json.Marshal(journalEnvelope{Presentation: presentation, Draft: draft, Candidate: &identity, RetryEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), jobstore.Record{JobID: job, Revision: presentation.Revision, Kind: "checkpoint", Data: encoded}); err != nil {
		t.Fatal(err)
	}
	dependencies.Store = store
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1, MaxRetainedJobs: 100, MaxTranscriptBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)

	restored, ok := restarted.Job(job)
	if !ok || restored.AttemptOrdinal != 2 || !hasAction(restored.AllowedActions, fix.ActionRetry) {
		t.Fatalf("restored retry state = %+v", restored)
	}
	commandID, _ := fix.NewCommandID()
	if _, err := restarted.Execute(context.Background(), fix.JobCommand{RequestID: commandID, JobID: job, ExpectedRevision: restored.Revision, Action: fix.ActionRetry}); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runtime, job)
	runtime.mu.Lock()
	requests := append([]agent.Request(nil), runtime.requests[job]...)
	runtime.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("retry requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.Task.Instructions.Envelope != draft.Instructions.Envelope || request.Task.Instructions.DetachedBody != draft.Instructions.DetachedBody {
		t.Fatalf("retry weakened or rewrote prepared instructions: %+v", request.Task.Instructions)
	}
	if request.Task.Instructions.RetryEvidence != evidence || !strings.Contains(request.Task.Instructions.EffectiveBody(), evidence) {
		t.Fatalf("retry evidence missing from next task: %+v", request.Task.Instructions)
	}
	running, _ := restarted.Job(job)
	if running.AttemptOrdinal != 3 {
		t.Fatalf("retry attempt = %d, want 3", running.AttemptOrdinal)
	}
	cancelID, _ := fix.NewCommandID()
	if _, err := restarted.Execute(context.Background(), fix.JobCommand{RequestID: cancelID, JobID: job, Action: fix.ActionCancel}); err != nil {
		t.Fatal(err)
	}
	readyAgain := waitForPhase(t, restarted, job, fix.PhaseAwaitingAction)
	if !hasAction(readyAgain.AllowedActions, fix.ActionRetry) {
		t.Fatalf("explicit retry was capped after attempt 3: %+v", readyAgain.AllowedActions)
	}
	fourthID, _ := fix.NewCommandID()
	if _, err := restarted.Execute(context.Background(), fix.JobCommand{RequestID: fourthID, JobID: job, ExpectedRevision: readyAgain.Revision, Action: fix.ActionRetry}); err != nil {
		t.Fatalf("fourth explicit attempt was rejected: %v", err)
	}
	waitStarted(t, runtime, job)
	fourth, _ := restarted.Job(job)
	if fourth.AttemptOrdinal != 4 {
		t.Fatalf("retry attempt = %d, want 4", fourth.AttemptOrdinal)
	}
	runtime.mu.Lock()
	history := append([]agent.Request(nil), runtime.requests[job]...)
	runtime.mu.Unlock()
	if len(history) != 2 || history[0].AttemptID == history[1].AttemptID {
		t.Fatalf("retry attempt history = %+v, want two distinct post-restart attempts", history)
	}
}

func TestRetryAndCancelOneJobDoNotDisturbConcurrentJob(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)
	first := prepareAndSubmit(t, manager, "first.go")
	second := prepareAndSubmit(t, manager, "second.go")
	waitStarted(t, runtime, first, second)

	runtime.complete(first)
	firstReview := waitForPhase(t, manager, first, fix.PhaseAwaitingReview)
	retryID, _ := fix.NewCommandID()
	if _, err := manager.Execute(context.Background(), fix.JobCommand{RequestID: retryID, JobID: first, ExpectedRevision: firstReview.Revision, Action: fix.ActionRetry}); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runtime, first)
	cancelID, _ := fix.NewCommandID()
	if _, err := manager.Execute(context.Background(), fix.JobCommand{RequestID: cancelID, JobID: first, Action: fix.ActionCancel}); err != nil {
		t.Fatal(err)
	}
	waitForPhase(t, manager, first, fix.PhaseAwaitingAction)
	secondView, _ := manager.Job(second)
	if secondView.Phase != fix.PhaseRunning || secondView.AttemptOrdinal != 1 {
		t.Fatalf("concurrent job disturbed by retry/cancel: %+v", secondView)
	}
	runtime.complete(second)
	waitForPhase(t, manager, second, fix.PhaseAwaitingReview)
}

func TestVerificationBuildsBoundedRetryEvidence(t *testing.T) {
	path, _ := fix.ParseRepoPath("bad.go")
	record := &jobRecord{
		draft: FixDraft{TargetScore: 50, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
			Targets: []fix.TargetSnapshot{{Path: path, Score: 88, Complete: true}},
		}}},
		presentation: fix.JobPresentation{AttemptOrdinal: 1, Targets: []fix.FilePresentation{{Path: path}}},
	}
	manager := &Manager{}
	manager.applyVerification(record, workerResult{
		diff:   candidateDiffForRetry(),
		verify: fixanalysis.VerificationResult{Files: []fixanalysis.FileResult{{Path: path, Score: 72, Complete: true}}, Complete: true, Compliant: false, Diagnostic: strings.Repeat("x", 600)},
	})
	if len(record.retryEvidence) > 504 || !strings.Contains(record.retryEvidence, "score 72.0 exceeds target 50.0") {
		t.Fatalf("retry evidence is not concise/actionable: len=%d value=%q", len(record.retryEvidence), record.retryEvidence)
	}
}

func candidateDiffForRetry() candidate.DiffSnapshot {
	return candidate.DiffSnapshot{Scope: fix.ScopeClean, Fingerprint: "retry-diff"}
}

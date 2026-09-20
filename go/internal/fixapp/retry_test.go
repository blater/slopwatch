package fixapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
)

type rejectingVerifier struct {
	fakeAnalysis
	calls atomic.Int32
}

func (analysis *rejectingVerifier) Verify(context.Context, fixanalysis.VerificationRequest) (fixanalysis.VerificationResult, error) {
	analysis.calls.Add(1)
	return fixanalysis.VerificationResult{}, errors.New("analysis validation must not run")
}

func TestSuccessfulAgentCompletesWithoutValidationOrAutomaticRetry(t *testing.T) {
	for _, plan := range []fix.DeliveryPlan{
		testPushPlan,
		{Workspace: fix.WorkspaceCurrent, Git: fix.GitLeaveUncommitted, Publish: fix.PublishLocal},
	} {
		t.Run(string(plan.Git), func(t *testing.T) {
			manager, runtime := newTestManager(t, 1)
			defer shutdownManager(t, manager)
			analysis := &rejectingVerifier{}
			manager.controller.workers.analysis = analysis
			input := prepare(t, manager, "marked.go")
			input.DeliveryPlan = plan
			input.Baseline.Contract.Targets[0].Complete = false
			input.Baseline.Contract.Targets[0].Score = 99
			input.TargetScore = 1
			id, err := manager.Run(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			waitStarted(t, runtime, id)
			runtime.complete(id)
			result := waitForPhase(t, manager, id, fix.PhaseCompleted)
			if analysis.calls.Load() != 0 || result.AttemptOrdinal != 1 || result.Issue != nil {
				t.Fatalf("completed work was validated or retried: calls=%d result=%+v", analysis.calls.Load(), result)
			}
			if result.TargetStatus != fix.ScoreUnmeasured || result.Targets[0].VerifiedScore != nil {
				t.Fatalf("unmeasured result claims score verification: %+v", result)
			}
			runtime.mu.Lock()
			requests := runtime.requests[id]
			if len(requests) != 1 || len(requests[0].Task.Targets) != 1 || requests[0].Task.Targets[0].Path != "marked.go" {
				t.Errorf("agent targets or attempt count changed: %+v", requests)
			}
			runtime.mu.Unlock()
		})
	}
}

func TestLegacyVerifierResultDoesNotRetryOrRejectWork(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	record := &jobRecord{
		input:        FixInput{DeliveryPlan: testPushPlan, TargetScore: 1},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseVerifying, AttemptOrdinal: 1},
		attempt:      attempt, candidate: &fix.CandidateIdentity{Job: job}, commands: map[fix.CommandID]CommandReceipt{},
		delivery: delivery.Result{Commit: "done", LocalRef: "refs/heads/fix", Pushed: true},
	}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory(), Delivery: &fakeDeliverySaga{}}, Options{Clock: time.Now})
	manager.state = &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, verifiersRunning: 1}
	manager.handleResult(workerResult{kind: workerVerifier, job: job, attempt: attempt,
		diff:   candidate.DiffSnapshot{Scope: fix.ScopeViolated, Fingerprint: "diff"},
		verify: fixanalysis.VerificationResult{Complete: false, TargetMet: false, Diagnostic: "incomplete estimated evidence"},
	})
	if record.presentation.Phase != fix.PhaseCompleted || record.presentation.AttemptOrdinal != 1 || record.presentation.Issue != nil {
		t.Fatalf("legacy validation rejected completed work: %+v", record.presentation)
	}
}

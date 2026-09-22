package fixapp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
)

func TestPublicationRunsAsSavedSagaSteps(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	saga := &fakeDeliverySaga{}
	manager.deps.Delivery = saga
	manager.publication.delivery = saga
	defer shutdownManager(t, manager)
	input := prepare(t, manager, "one.go")
	input, err := ApplyFormValues(input, FormValues{TargetScore: input.TargetScore, Focus: input.Focus, ChangeScope: input.ChangeScope,
		DeliveryPlan: testPushPlan, BranchName: "slopwatch/fix/one-test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := manager.Run(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runtime, id)
	runtime.complete(id)
	completed := waitForPhase(t, manager, id, fix.PhaseCompleted)
	if completed.Delivery != fix.DeliveryPushed {
		t.Fatalf("delivery = %s, want pushed", completed.Delivery)
	}
	saga.mu.Lock()
	steps := append([]string(nil), saga.steps...)
	saga.mu.Unlock()
	if got := fmt.Sprint(steps); got != "[commit local remote]" {
		t.Fatalf("publication steps = %s", got)
	}
}

func TestAmbiguousPullRequestUsesDistinctReconciliationStep(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "17", URL: "https://github.com/owner/repo/pull/17", Draft: true}}
	store := jobstore.NewMemory()
	manager := bareTestManager(Dependencies{Store: store, Delivery: &fakeDeliverySaga{}, Publisher: pullRequests}, Options{Clock: time.Now})
	record := &jobRecord{
		input:        FixInput{DeliveryPlan: testPRPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhasePublishing}, attempt: attempt,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
		delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		published: publisher.Result{ProviderID: "unverified", URL: "https://github.com/owner/repo/pull/99", Ambiguous: true},
	}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.state = state
	manager.controller.startNextPublication(record)
	if record.presentation.Phase != fix.PhaseReconciling || state.otherRunning != 1 {
		t.Fatalf("ambiguous PR did not enter reconciliation: phase=%s running=%d", record.presentation.Phase, state.otherRunning)
	}
	result := <-manager.results
	manager.handleResult(result)
	if record.presentation.Phase != fix.PhaseCompleted || pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
		t.Fatalf("PR reconciliation result: phase=%s creates=%d reconciles=%d", record.presentation.Phase, pullRequests.createCount(), pullRequests.reconcileCount())
	}
}

func TestCanceledPullRequestReconciliationReleasesJobWithoutHidingAmbiguity(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	pullRequests := &recordingPublisher{blockFirstReconcile: true, started: make(chan struct{}, 1),
		reconcileResult: publisher.Result{ProviderID: "18", URL: "https://github.com/owner/repo/pull/18", Draft: true}}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: &fakeDeliverySaga{}, Publisher: pullRequests}, Options{Clock: time.Now})
	record := &jobRecord{input: FixInput{DeliveryPlan: testPRPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhasePublishing}, attempt: attempt,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
		delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		published: publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.state = state
	manager.controller.startNextPublication(record)
	<-pullRequests.started
	record.canceled = true
	record.presentation.Phase = fix.PhaseCanceling
	record.cancel()
	manager.handleResult(<-manager.results)
	manager.handleResult(<-manager.results)
	manager.handleResult(<-manager.results)
	if record.presentation.Phase != fix.PhaseCanceled || record.published.Ambiguous || record.candidate != nil {
		t.Fatalf("canceled reconciliation did not resolve and clean up: %+v", record.presentation)
	}
	if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 2 {
		t.Fatalf("cancel performed the wrong delivery calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
	}
}

func TestCancelDuringAmbiguousLocalRefReconcilesBeforeCleanup(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	saga := &resolvingDeliverySaga{}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: saga}, Options{Clock: time.Now})
	record := &jobRecord{input: FixInput{DeliveryPlan: testPushPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseCanceling}, attempt: attempt, canceled: true,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
	manager.state = state
	manager.controller.handlePublicationResult(record, workerResult{kind: workerPublish, job: job, attempt: attempt,
		delivery: delivery.Result{Commit: "abc", Ambiguous: true}, err: context.Canceled})
	manager.handleResult(<-manager.results)
	manager.handleResult(<-manager.results)
	if record.presentation.Phase != fix.PhaseCanceled || record.candidate != nil || saga.recorded() != "[reconcile]" {
		t.Fatalf("local ref cancellation did not reconcile before cleanup: phase=%s steps=%s", record.presentation.Phase, saga.recorded())
	}
}

func TestRestartedAmbiguousPullRequestReconcilesBeforeCompleting(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	input := prepare(t, seed, "one.go")
	dependencies := seed.deps
	shutdownManager(t, seed)
	input.DeliveryPlan = testPRPlan
	input.BranchName = "slopwatch/fix/test"
	input.Preferences.Delivery.Remote = "origin"
	input.Preferences.Delivery.BaseBranch = "main"
	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: input.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job), AnalysisRoot: "/candidate/" + string(job), BaseCommit: input.Workspace.BaseCommit}
	presentation := fix.JobPresentation{ID: job, Phase: fix.PhaseFailed, Issue: &fix.JobIssue{Code: "publication_ambiguous", Summary: "Delivery state is ambiguous"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store := jobstore.NewMemory()
	checkpoint, _ := json.Marshal(storedJobState{Presentation: presentation, Input: storedJobInputFrom(input), Candidate: &identity,
		Delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		Published: publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}})
	if err := store.Save(t.Context(), jobstore.Record{JobID: job, UpdatedAt: presentation.UpdatedAt, State: checkpoint}); err != nil {
		t.Fatal(err)
	}
	pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "19", URL: "https://github.com/owner/repo/pull/19", Draft: true}}
	dependencies.Store, dependencies.Candidates, dependencies.Delivery, dependencies.Publisher = store, fakeCandidates{}, &fakeDeliverySaga{}, pullRequests
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)
	waitForPhase(t, restarted, job, fix.PhaseCompleted)
	if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
		t.Fatalf("restart publication calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
	}
}

func TestRestartPreservesCanceledAmbiguousDeliveryAndOnlyReconciles(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	input := prepare(t, seed, "one.go")
	dependencies := seed.deps
	shutdownManager(t, seed)
	input.DeliveryPlan = testPRPlan
	input.BranchName = "slopwatch/fix/test"
	input.Preferences.Delivery.Remote = "origin"
	input.Preferences.Delivery.BaseBranch = "main"
	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: input.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job), AnalysisRoot: "/candidate/" + string(job), BaseCommit: input.Workspace.BaseCommit}
	presentation := fix.JobPresentation{ID: job, Phase: fix.PhaseReconciling, Issue: &fix.JobIssue{Code: "publication_canceled", Summary: "Checking canceled delivery"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store := jobstore.NewMemory()
	checkpoint, _ := json.Marshal(storedJobState{Presentation: presentation, Input: storedJobInputFrom(input), Candidate: &identity, Canceled: true,
		Delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		Published: publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}})
	if err := store.Save(t.Context(), jobstore.Record{JobID: job, UpdatedAt: presentation.UpdatedAt, State: checkpoint}); err != nil {
		t.Fatal(err)
	}
	pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "19", URL: "https://github.com/owner/repo/pull/19", Draft: true}}
	dependencies.Store, dependencies.Candidates, dependencies.Delivery, dependencies.Publisher = store, fakeCandidates{}, &fakeDeliverySaga{}, pullRequests
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)
	waitForPhase(t, restarted, job, fix.PhaseCanceled)
	if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
		t.Fatalf("restart cancellation calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
	}
}

func TestRestartReconcilesCanceledInFlightDeliveryStepBeforeCleanup(t *testing.T) {
	for _, testCase := range canceledPublicationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			runCanceledPublicationRecovery(t, testCase)
		})
	}
}

type canceledPublicationCase struct {
	name       string
	step       publicationStep
	plan       fix.DeliveryPlan
	delivered  delivery.Result
	expectPull bool
}

func canceledPublicationCases() []canceledPublicationCase {
	localRef := "refs/heads/slopwatch/fix/test"
	return []canceledPublicationCase{
		{name: string(publicationLocalRef), step: publicationLocalRef, plan: testPushPlan, delivered: delivery.Result{Commit: "abc"}},
		{name: string(publicationRemoteRef), step: publicationRemoteRef, plan: testPushPlan, delivered: delivery.Result{Commit: "abc", LocalRef: localRef}},
		{name: string(publicationPullRequest), step: publicationPullRequest, plan: testPRPlan, expectPull: true,
			delivered: delivery.Result{Commit: "abc", LocalRef: localRef, RemoteRef: localRef, Repository: "owner/repo", Pushed: true}},
	}
}

func runCanceledPublicationRecovery(t *testing.T, testCase canceledPublicationCase) {
	seed, _ := newTestManager(t, 1)
	input := prepare(t, seed, "one.go")
	dependencies := seed.deps
	shutdownManager(t, seed)
	input.BranchName = "slopwatch/fix/test"
	input.Preferences.Delivery.Remote = "origin"
	input.DeliveryPlan = testCase.plan
	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: input.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job), AnalysisRoot: "/candidate/" + string(job), BaseCommit: input.Workspace.BaseCommit}
	presentation := fix.JobPresentation{ID: job, Phase: fix.PhaseCanceling, Issue: &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store := jobstore.NewMemory()
	checkpoint, _ := json.Marshal(storedJobState{Presentation: presentation, Input: storedJobInputFrom(input), Candidate: &identity, Canceled: true, PublicationStep: testCase.step, Delivery: testCase.delivered})
	if err := store.Save(t.Context(), jobstore.Record{JobID: job, UpdatedAt: presentation.UpdatedAt, State: checkpoint}); err != nil {
		t.Fatal(err)
	}
	saga := &resolvingDeliverySaga{}
	pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "21", URL: "https://github.com/owner/repo/pull/21"}}
	dependencies.Store, dependencies.Candidates, dependencies.Delivery, dependencies.Publisher = store, fakeCandidates{}, saga, pullRequests
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)
	waitForPhase(t, restarted, job, fix.PhaseCanceled)
	assertCanceledPublicationCalls(t, testCase.expectPull, saga, pullRequests)
}

func assertCanceledPublicationCalls(t *testing.T, expectPull bool, saga *resolvingDeliverySaga, pullRequests *recordingPublisher) {
	t.Helper()
	if expectPull {
		if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
			t.Fatalf("restart PR calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
		}
		return
	}
	if saga.recorded() != "[reconcile]" {
		t.Fatalf("restart delivery steps = %s", saga.recorded())
	}
}

func TestCancelFailedAmbiguousDeliveryReconcilesBeforeCleanup(t *testing.T) {
	for _, testCase := range failedAmbiguousCases() {
		t.Run(testCase.name, func(t *testing.T) {
			runFailedAmbiguousCancellation(t, testCase)
		})
	}
}

type failedAmbiguousCase struct {
	name       string
	plan       fix.DeliveryPlan
	published  publisher.Result
	expectPull bool
}

func failedAmbiguousCases() []failedAmbiguousCase {
	return []failedAmbiguousCase{
		{name: string(fix.PublishPush), plan: testPushPlan},
		{name: string(fix.PublishPullRequest), plan: testPRPlan,
			published: publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}, expectPull: true},
	}
}

func runFailedAmbiguousCancellation(t *testing.T, testCase failedAmbiguousCase) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	saga := &resolvingDeliverySaga{}
	pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "20", URL: "https://github.com/owner/repo/pull/20"}}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: saga, Publisher: pullRequests}, Options{Clock: time.Now})
	record := failedAmbiguousRecord(job, attempt, testCase)
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
	manager.state = state
	requestID, _ := fix.NewCommandID()
	response := make(chan commandResponse, 1)
	manager.handleCommand(commandCall{ctx: t.Context(), command: fix.JobCommand{RequestID: requestID, JobID: job, Action: fix.ActionCancel}, response: response})
	if reply := <-response; reply.err != nil {
		t.Fatal(reply.err)
	}
	manager.handleResult(<-manager.results)
	manager.handleResult(<-manager.results)
	if record.presentation.Phase != fix.PhaseCanceled || record.candidate != nil {
		t.Fatalf("ambiguous cancellation did not reconcile then clean up: %+v", record.presentation)
	}
	assertFailedAmbiguousCalls(t, testCase.expectPull, saga, pullRequests)
}

func failedAmbiguousRecord(job fix.JobID, attempt fix.AttemptID, testCase failedAmbiguousCase) *jobRecord {
	return &jobRecord{input: FixInput{DeliveryPlan: testCase.plan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseFailed, AllowedActions: []fix.JobAction{fix.ActionCancel}, Issue: &fix.JobIssue{Code: "publication_ambiguous"}},
		attempt:      attempt, candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
		delivery: delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true, Ambiguous: !testCase.expectPull,
			RemoteRef: chooseRemoteRef(testCase.expectPull)}, published: testCase.published}
}

func chooseRemoteRef(expectPull bool) string {
	if expectPull {
		return "refs/heads/slopwatch/fix/test"
	}
	return ""
}

func assertFailedAmbiguousCalls(t *testing.T, expectPull bool, saga *resolvingDeliverySaga, pullRequests *recordingPublisher) {
	t.Helper()
	if expectPull {
		if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
			t.Fatalf("PR cancellation calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
		}
		return
	}
	if got := saga.recorded(); got != "[reconcile]" {
		t.Fatalf("branch cancellation delivery steps = %s", got)
	}
}

func TestFailedCancelSaveRollsBackCancellationIntent(t *testing.T) {
	job, _ := fix.NewJobID()
	record := &jobRecord{presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseFailed, AllowedActions: []fix.JobAction{fix.ActionCancel}}, commands: map[fix.CommandID]CommandReceipt{}}
	manager := bareTestManager(Dependencies{Store: &failSaveStore{}}, Options{Clock: time.Now})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
	manager.state = state
	requestID, _ := fix.NewCommandID()
	response := make(chan commandResponse, 1)
	manager.handleCommand(commandCall{ctx: t.Context(), command: fix.JobCommand{RequestID: requestID, JobID: job, Action: fix.ActionCancel}, response: response})
	if reply := <-response; reply.err == nil {
		t.Fatal("cancel unexpectedly succeeded when its durable applied record failed")
	}
	if record.canceled || record.presentation.Phase != fix.PhaseFailed {
		t.Fatalf("failed cancel leaked transient intent: canceled=%t phase=%s", record.canceled, record.presentation.Phase)
	}
}

func TestRestartReconcilesFailedCleanupBeforeCandidateRecovery(t *testing.T) {
	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate/" + string(job), AnalysisRoot: "/candidate/" + string(job)}
	presentation := fix.JobPresentation{ID: job, Phase: fix.PhaseCompleted, Issue: &fix.JobIssue{Code: "cleanup_failed", Summary: "partial cleanup"}}
	envelope, _ := json.Marshal(storedJobState{Presentation: presentation, Candidate: &identity})
	candidates := &cleanupRecoveryCandidates{}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory(), Candidates: candidates}, Options{Clock: time.Now})
	manager.initial = []jobstore.Record{{JobID: job, State: envelope}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{}, reservations: map[string]fix.JobID{}}
	manager.state = state
	manager.restore()
	record := state.jobs[job]
	if candidates.reconcileCalls != 1 || candidates.recoverCalls != 0 {
		t.Fatalf("cleanup restart calls: reconcile=%d recover=%d", candidates.reconcileCalls, candidates.recoverCalls)
	}
	if record == nil || record.candidate != nil || record.presentation.Issue != nil || record.presentation.Phase != fix.PhaseCompleted {
		t.Fatalf("cleanup restart did not finish safely: %+v", record)
	}
}

func TestPullRequestRunDefersMissingBaseBranchToPublication(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	input := prepare(t, manager, "one.go")
	input, err := ApplyFormValues(input, FormValues{TargetScore: input.TargetScore, Focus: input.Focus, ChangeScope: input.ChangeScope, DeliveryPlan: testPRPlan, BranchName: "slopwatch/fix/test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Run(context.Background(), input); err != nil {
		t.Fatalf("run rejected delivery policy before publication: %v", err)
	}
}

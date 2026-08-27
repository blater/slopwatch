package fixapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
)

var (
	testPushPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}
	testPRPlan   = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPullRequest}
)

func TestJobLogContainsStartAndResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fix-jobs.jsonl")
	manager := &Manager{options: Options{Clock: time.Now, JobIndexPath: path}}
	startedAt := time.Now()
	record := &jobRecord{
		input: FixInput{Targets: []fix.RepoPath{"a.go"}, Profile: agent.Profile{ID: "codex", Runtime: "codex-cli"}, Model: "gpt", Effort: "high", TargetScore: 10,
			Instructions: agent.InstructionDocument{Envelope: "trusted instructions", Objective: "refactor every selected file"}},
		presentation: fix.JobPresentation{ID: "job-one", Phase: fix.PhaseQueued, CreatedAt: startedAt, Targets: []fix.FilePresentation{
			{Path: "a.go", Changed: true, ChangeStatus: "M"},
			{Path: "target/classes/A.class", Changed: true, ChangeStatus: "A", Classification: "supporting"},
		}},
		agentReferences: []string{"thread-one"},
	}
	manager.logJobStart(record)
	manager.logJobPrompt(record.presentation.ID, "attempt-one", record.input.Instructions.EffectiveBody())
	record.presentation.Phase = fix.PhaseCompleted
	record.presentation.CurrentAction = "Done"
	manager.logJobResult(record)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(contents), "\n") != 1 {
		t.Fatalf("job index should contain one record per job: %q", contents)
	}
	var indexed jobIndexEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(contents))), &indexed); err != nil {
		t.Fatal(err)
	}
	if indexed.End == nil || indexed.End.Status != fix.PhaseCompleted || indexed.LogFile == "" || len(indexed.End.FilesTouched) != 1 || len(indexed.End.AgentReferences) != 1 {
		t.Fatalf("job index = %+v", indexed)
	}
	if bytes.Contains(contents, []byte("trusted instructions")) {
		t.Fatalf("job index contains the prompt: %s", contents)
	}
	logContents, err := os.ReadFile(indexed.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"SLOPWATCH FIX JOB", "PROMPT attempt-one", record.input.Instructions.EffectiveBody(), "Status: completed", "thread-one", "M a.go"} {
		if !strings.Contains(string(logContents), wanted) {
			t.Fatalf("job text log omitted %q: %s", wanted, logContents)
		}
	}
	if strings.Contains(string(logContents), "target/classes/A.class") {
		t.Fatalf("job text log recorded a build artifact: %s", logContents)
	}
}

func TestRunningJobPersistsPromptActivityAndResultInItsTextLog(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "fix-jobs.jsonl")
	manager, runtime := newTestManagerWithStore(t, 1, jobstore.NewMemory(), Options{JobIndexPath: indexPath})
	defer shutdownManager(t, manager)
	job := loadAndRun(t, manager, "one.go")
	waitStarted(t, runtime, job)
	runtime.complete(job)
	waitForPhase(t, manager, job, fix.PhaseCompleted)

	indexContents, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var indexed jobIndexEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(indexContents))), &indexed); err != nil {
		t.Fatal(err)
	}
	logContents, err := os.ReadFile(indexed.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"PROMPT ", "Required target checklist:", "Refactoring", "Final streamed activity", "RESULT", "Status: completed", "session-" + string(job)} {
		if !strings.Contains(string(logContents), wanted) {
			t.Fatalf("persisted job log omitted %q: %s", wanted, logContents)
		}
	}
	page, err := manager.Transcript(context.Background(), job, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	visible := ""
	for _, entry := range page.Entries {
		visible += entry.Text + "\n"
	}
	if !strings.Contains(visible, "Required target checklist:") || !strings.Contains(visible, "Final streamed activity") {
		t.Fatalf("UI transcript did not read the persisted job log: %q", visible)
	}
}

func TestLoadFixIncludesModelAndEffortChoices(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	input := prepare(t, manager, "one.go")
	if len(input.Probe.Capabilities.Models) == 0 || len(input.Probe.Capabilities.Efforts) == 0 || input.Model != "gpt-test" || input.Effort != "high" {
		t.Fatalf("Fix choices were not loaded from the agent: model=%q effort=%q capabilities=%+v", input.Model, input.Effort, input.Probe.Capabilities)
	}
}

func TestManagerRunsJobsConcurrentlyAndStartsMoreWhileRunning(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)

	first := loadAndRun(t, manager, "one.go")
	second := loadAndRun(t, manager, "two.go")
	started := map[fix.JobID]bool{}
	for len(started) < 2 {
		select {
		case id := <-runtime.started:
			started[id] = true
		case <-time.After(time.Second):
			t.Fatal("two jobs did not start concurrently")
		}
	}
	if !started[first] || !started[second] {
		t.Fatalf("started %v, want %s and %s", started, first, second)
	}

	third := loadAndRun(t, manager, "three.go")
	if got := manager.Jobs(JobFilter{}); len(got.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(got.Jobs))
	}
	if job, _ := manager.Job(third); job.Phase != fix.PhaseQueued {
		t.Fatalf("third phase = %s, want queued", job.Phase)
	}

	runtime.complete(first)
	waitForPhase(t, manager, first, fix.PhaseCompleted)
	select {
	case id := <-runtime.started:
		if id != third {
			t.Fatalf("newly started = %s, want %s", id, third)
		}
	case <-time.After(time.Second):
		t.Fatal("queued job did not start when capacity became available")
	}
	runtime.complete(second)
	runtime.complete(third)
	waitForPhase(t, manager, second, fix.PhaseCompleted)
	waitForPhase(t, manager, third, fix.PhaseCompleted)
}

func TestMultipleSlopwatchProcessesShareJobProgressAndStartJobs(t *testing.T) {
	directory := t.TempDir()
	firstStore, err := jobstore.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	secondStore, err := jobstore.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	first, runtime := newTestManagerWithStore(t, 1, firstStore, Options{})
	second, secondRuntime := newTestManagerWithStore(t, 1, secondStore, Options{})
	defer shutdownManager(t, first)
	defer shutdownManager(t, second)

	firstJob := loadAndRun(t, first, "first.go")
	waitStarted(t, runtime, firstJob)
	waitForPhase(t, second, firstJob, fix.PhaseRunning)

	secondJob := loadAndRun(t, second, "second.go")
	waitStarted(t, secondRuntime, secondJob)
	waitForPhase(t, first, secondJob, fix.PhaseRunning)

	runtime.complete(firstJob)
	secondRuntime.complete(secondJob)
	for _, manager := range []*Manager{first, second} {
		waitForPhase(t, manager, firstJob, fix.PhaseCompleted)
		waitForPhase(t, manager, secondJob, fix.PhaseCompleted)
	}
}

func TestDisplayTranscriptDoesNotSetAgentExecutionOutputBudget(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	job := loadAndRun(t, manager, "one.go")
	waitStarted(t, runtime, job)
	runtime.mu.Lock()
	request := runtime.requests[job][0]
	runtime.mu.Unlock()
	if request.Limits.MaxOutputBytes != 0 {
		t.Fatalf("display transcript leaked into execution budget: %d", request.Limits.MaxOutputBytes)
	}
	runtime.complete(job)
	waitForPhase(t, manager, job, fix.PhaseCompleted)
}

func TestAllSelectedTargetsReachTheAgentPromptAndTask(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	workspace := fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", GitCommonDir: "/repo/.git", BaseCommit: "abc", CurrentBranch: "main"}
	input, err := manager.LoadFix(context.Background(), LoadRequest{Workspace: workspace, Targets: []fix.RepoPath{"one.go", "two.go", "three.go"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Run(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runtime, job)
	runtime.mu.Lock()
	request := runtime.requests[job][0]
	runtime.mu.Unlock()
	if len(request.Task.Targets) != 3 {
		t.Fatalf("agent task targets = %v", request.Task.Targets)
	}
	prompt := request.Task.Instructions.EffectiveBody()
	for _, path := range []string{"one.go", "two.go", "three.go"} {
		if !strings.Contains(prompt, "- "+path) {
			t.Fatalf("agent prompt omitted %s: %q", path, prompt)
		}
	}
	if !strings.Contains(prompt, "do not stop after the first") {
		t.Fatalf("agent prompt omitted the multi-target completion instruction: %q", prompt)
	}
	runtime.complete(job)
	waitForPhase(t, manager, job, fix.PhaseCompleted)
}

func TestLargeTargetManifestContainsEverySelectedFileInCandidateStaging(t *testing.T) {
	staging, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]fix.RepoPath, 200)
	for index := range targets {
		targets[index] = fix.RepoPath(fmt.Sprintf("src/feature-%03d/a-deliberately-long-selected-filename-for-manifest.go", index))
	}
	manifest, err := prepareTargetManifest(fix.CandidateIdentity{StagingRoot: staging}, targets)
	if err != nil {
		t.Fatal(err)
	}
	if manifest == nil || manifest.Count != len(targets) || filepath.Dir(filepath.Dir(manifest.Path)) != staging {
		t.Fatalf("manifest = %#v, staging = %q", manifest, staging)
	}
	contents, err := os.ReadFile(manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range targets {
		if strings.Count(string(contents), path.String()+"\n") != 1 {
			t.Fatalf("manifest omitted or duplicated %s", path)
		}
	}
	info, err := os.Stat(manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest permissions = %v", info.Mode().Perm())
	}
}

func TestCancelAffectsOnlySelectedJobAndCommandIsIdempotent(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)
	first := loadAndRun(t, manager, "one.go")
	second := loadAndRun(t, manager, "two.go")
	waitStarted(t, runtime, first, second)

	opened := waitForPhase(t, manager, first, fix.PhaseRunning)
	runtime.mu.Lock()
	attempt := runtime.requests[first][0].AttemptID
	runtime.mu.Unlock()
	manager.events <- agentUpdate{event: agent.Event{
		JobID: first, AttemptID: attempt, At: time.Now(), Kind: agent.EventActivity, Summary: "Activity after cancel confirmation opened",
	}}
	deadline := time.Now().Add(time.Second)
	advanced := false
	for time.Now().Before(deadline) {
		latest, _ := manager.Job(first)
		if latest.UpdatedAt.After(opened.UpdatedAt) {
			advanced = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !advanced {
		t.Fatal("agent activity did not advance the job revision")
	}
	commandID, _ := fix.NewCommandID()
	command := fix.JobCommand{RequestID: commandID, JobID: first, Action: fix.ActionCancel}
	receipt, err := manager.Execute(context.Background(), command)
	if err != nil || !receipt.Accepted {
		t.Fatalf("cancel: receipt=%+v err=%v", receipt, err)
	}
	duplicate, err := manager.Execute(context.Background(), command)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate cancel: receipt=%+v err=%v", duplicate, err)
	}
	waitForPhase(t, manager, first, fix.PhaseCanceled)
	if secondJob, _ := manager.Job(second); secondJob.Phase != fix.PhaseRunning {
		t.Fatalf("second phase = %s, want running", secondJob.Phase)
	}
	runtime.complete(second)
	waitForPhase(t, manager, second, fix.PhaseCompleted)
}

func TestTargetReservationIsReleasedImmediatelyOnCancel(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	input := prepare(t, manager, "one.go")
	first, err := manager.Run(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := prepare(t, manager, "one.go")
	if _, err := manager.Run(context.Background(), secondInput); !errors.Is(err, ErrTargetReserved) {
		t.Fatalf("second run error = %v, want target reserved", err)
	}
	waitStarted(t, runtime, first)
	waitForPhase(t, manager, first, fix.PhaseRunning)
	commandID, _ := fix.NewCommandID()
	if _, err := manager.Execute(context.Background(), fix.JobCommand{RequestID: commandID, JobID: first, Action: fix.ActionCancel}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Run(context.Background(), secondInput); err != nil {
		t.Fatalf("run immediately after cancel: %v", err)
	}
	waitForPhase(t, manager, first, fix.PhaseCanceled)
}

func TestSubscriptionIsLevelTriggered(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	subscription := manager.Subscribe()
	defer subscription.Close()
	id := loadAndRun(t, manager, "one.go")
	if err := subscription.Wait(context.Background()); err != nil {
		t.Fatalf("wait after an already-published change: %v", err)
	}
	waitStarted(t, runtime, id)
	runtime.complete(id)
}

func TestAgentCompletionCannotOvertakeFinalStreamEvent(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	id := loadAndRun(t, manager, "one.go")
	waitStarted(t, runtime, id)
	runtime.complete(id)
	waitForPhase(t, manager, id, fix.PhaseCompleted)
	page, err := manager.Transcript(context.Background(), id, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range page.Entries {
		found = found || event.Summary == "Final streamed activity"
	}
	if !found {
		t.Fatalf("final streamed event was lost before completion transition: %+v", page.Entries)
	}
}

func TestPreparedCandidateIsSavedBeforeAgentLaunch(t *testing.T) {
	store := jobstore.NewMemory()
	manager, runtime := newTestManagerWithStore(t, 1, store, Options{})
	defer shutdownManager(t, manager)
	id := loadAndRun(t, manager, "one.go")
	waitStarted(t, runtime, id)
	records, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("saved job documents = %+v", records)
	}
	var saved storedJobState
	if err := json.Unmarshal(records[0].State, &saved); err != nil || saved.Candidate == nil {
		t.Fatalf("candidate was not saved before launch: state=%+v err=%v", saved, err)
	}
	runtime.complete(id)
}

func TestRestoreDiscoversAndSavesCandidateLostBeforePreparedHandshake(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	input := prepare(t, seed, "one.go")
	dependencies := seed.deps
	shutdownManager(t, seed)

	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: input.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job),
		AnalysisRoot: "/candidate/" + string(job), GitCommonDir: input.Workspace.GitCommonDir, BaseCommit: input.Workspace.BaseCommit}
	store := jobstore.NewMemory()
	presentation := fix.JobPresentation{ID: job, Phase: fix.PhasePreparing, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	state, _ := json.Marshal(storedJobState{Presentation: presentation, Input: storedJobInputFrom(input)})
	if err := store.Save(context.Background(), jobstore.Record{JobID: job, UpdatedAt: presentation.UpdatedAt, State: state}); err != nil {
		t.Fatal(err)
	}
	dependencies.Store = store
	dependencies.Candidates = discoveringCandidates{fakeCandidates: fakeCandidates{}, identity: identity}
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)
	records, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("recovery state = %+v", records)
	}
	var recovered storedJobState
	if err := json.Unmarshal(records[0].State, &recovered); err != nil || recovered.Candidate == nil || *recovered.Candidate != identity {
		t.Fatalf("recovered candidate was not durably handshaked: %+v, %v", recovered.Candidate, err)
	}
	jobView, _ := restarted.Job(job)
	if jobView.Phase != fix.PhaseFailed || jobView.Issue == nil || jobView.Issue.Code != "interrupted" {
		t.Fatalf("restored job = %+v", jobView)
	}
}

func TestRunDefersUnprovenIsolationToRuntime(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	input := prepare(t, manager, "one.go")
	runtime.mu.Lock()
	runtime.eligible = false
	runtime.mu.Unlock()
	if _, err := manager.Run(context.Background(), input); err != nil {
		t.Fatalf("run rejected readiness policy before runtime: %v", err)
	}
}

func TestPublicationRunsAsSavedSagaSteps(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	saga := &fakeDeliverySaga{}
	manager.deps.Delivery = saga
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
	manager := &Manager{deps: Dependencies{Store: store, Delivery: &fakeDeliverySaga{}, Publisher: pullRequests}, options: Options{Clock: time.Now},
		results: make(chan workerResult, 4), notify: make(chan struct{})}
	record := &jobRecord{
		input:        FixInput{DeliveryPlan: testPRPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhasePublishing}, attempt: attempt,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
		delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		published: publisher.Result{ProviderID: "unverified", URL: "https://github.com/owner/repo/pull/99", Ambiguous: true},
	}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.startNextPublication(state, record)
	if record.presentation.Phase != fix.PhaseReconciling || state.otherRunning != 1 {
		t.Fatalf("ambiguous PR did not enter reconciliation: phase=%s running=%d", record.presentation.Phase, state.otherRunning)
	}
	result := <-manager.results
	manager.handleResult(state, result)
	if record.presentation.Phase != fix.PhaseCompleted || pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
		t.Fatalf("PR reconciliation result: phase=%s creates=%d reconciles=%d", record.presentation.Phase, pullRequests.createCount(), pullRequests.reconcileCount())
	}
}

func TestCanceledPullRequestReconciliationReleasesJobWithoutHidingAmbiguity(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	pullRequests := &recordingPublisher{blockFirstReconcile: true, started: make(chan struct{}, 1),
		reconcileResult: publisher.Result{ProviderID: "18", URL: "https://github.com/owner/repo/pull/18", Draft: true}}
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: &fakeDeliverySaga{}, Publisher: pullRequests}, options: Options{Clock: time.Now},
		results: make(chan workerResult, 4), notify: make(chan struct{})}
	record := &jobRecord{input: FixInput{DeliveryPlan: testPRPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhasePublishing}, attempt: attempt,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
		delivery:  delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", RemoteRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true},
		published: publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.startNextPublication(state, record)
	<-pullRequests.started
	record.canceled = true
	record.presentation.Phase = fix.PhaseCanceling
	record.cancel()
	manager.handleResult(state, <-manager.results)
	manager.handleResult(state, <-manager.results)
	manager.handleResult(state, <-manager.results)
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
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: saga}, options: Options{Clock: time.Now},
		results: make(chan workerResult, 4), notify: make(chan struct{})}
	record := &jobRecord{input: FixInput{DeliveryPlan: testPushPlan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin"}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseCanceling}, attempt: attempt, canceled: true,
		candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
	manager.handlePublicationResult(state, record, workerResult{kind: workerPublish, job: job, attempt: attempt,
		delivery: delivery.Result{Commit: "abc", Ambiguous: true}, err: context.Canceled})
	manager.handleResult(state, <-manager.results)
	manager.handleResult(state, <-manager.results)
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
	for _, step := range []publicationStep{publicationLocalRef, publicationRemoteRef, publicationPullRequest} {
		t.Run(string(step), func(t *testing.T) {
			seed, _ := newTestManager(t, 1)
			input := prepare(t, seed, "one.go")
			dependencies := seed.deps
			shutdownManager(t, seed)
			input.BranchName = "slopwatch/fix/test"
			input.Preferences.Delivery.Remote = "origin"
			job, _ := fix.NewJobID()
			identity := fix.CandidateIdentity{Job: job, Repository: input.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job), AnalysisRoot: "/candidate/" + string(job), BaseCommit: input.Workspace.BaseCommit}
			presentation := fix.JobPresentation{ID: job, Phase: fix.PhaseCanceling, Issue: &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			delivered := delivery.Result{Commit: "abc"}
			if step == publicationRemoteRef || step == publicationPullRequest {
				delivered.LocalRef = "refs/heads/slopwatch/fix/test"
			}
			if step == publicationPullRequest {
				input.DeliveryPlan = testPRPlan
				delivered.RemoteRef, delivered.Pushed, delivered.Repository = delivered.LocalRef, true, "owner/repo"
			}
			store := jobstore.NewMemory()
			checkpoint, _ := json.Marshal(storedJobState{Presentation: presentation, Input: storedJobInputFrom(input), Candidate: &identity, Canceled: true, PublicationStep: step, Delivery: delivered})
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
			if step == publicationPullRequest {
				if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
					t.Fatalf("restart PR calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
				}
			} else if saga.recorded() != "[reconcile]" {
				t.Fatalf("restart delivery steps = %s", saga.recorded())
			}
		})
	}
}

func TestCancelFailedAmbiguousDeliveryReconcilesBeforeCleanup(t *testing.T) {
	for _, plan := range []fix.DeliveryPlan{testPushPlan, testPRPlan} {
		t.Run(string(plan.Publish), func(t *testing.T) {
			job, _ := fix.NewJobID()
			attempt, _ := fix.NewAttemptID()
			saga := &resolvingDeliverySaga{}
			pullRequests := &recordingPublisher{reconcileResult: publisher.Result{ProviderID: "20", URL: "https://github.com/owner/repo/pull/20"}}
			manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory(), Candidates: fakeCandidates{}, Delivery: saga, Publisher: pullRequests},
				options: Options{Clock: time.Now}, results: make(chan workerResult, 4), notify: make(chan struct{})}
			record := &jobRecord{input: FixInput{DeliveryPlan: plan, BranchName: "slopwatch/fix/test", Preferences: appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin", BaseBranch: "main"}}},
				presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseFailed, AllowedActions: []fix.JobAction{fix.ActionCancel}, Issue: &fix.JobIssue{Code: "publication_ambiguous"}},
				attempt:      attempt, candidate: &fix.CandidateIdentity{Job: job, RepositoryRoot: "/candidate"}, commands: map[fix.CommandID]CommandReceipt{},
				delivery: delivery.Result{Commit: "abc", LocalRef: "refs/heads/slopwatch/fix/test", Repository: "owner/repo", Pushed: true}}
			if plan.Publish == fix.PublishPush {
				record.delivery.Ambiguous = true
			} else {
				record.delivery.RemoteRef = "refs/heads/slopwatch/fix/test"
				record.published = publisher.Result{URL: "https://github.com/owner/repo/pull/99", Ambiguous: true}
			}
			state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
			requestID, _ := fix.NewCommandID()
			response := make(chan commandResponse, 1)
			manager.handleCommand(state, commandCall{ctx: t.Context(), command: fix.JobCommand{RequestID: requestID, JobID: job, Action: fix.ActionCancel}, response: response})
			if reply := <-response; reply.err != nil {
				t.Fatal(reply.err)
			}
			manager.handleResult(state, <-manager.results)
			manager.handleResult(state, <-manager.results)
			if record.presentation.Phase != fix.PhaseCanceled || record.candidate != nil {
				t.Fatalf("ambiguous cancellation did not reconcile then clean up: %+v", record.presentation)
			}
			if plan.Publish == fix.PublishPush {
				if got := saga.recorded(); got != "[reconcile]" {
					t.Fatalf("branch cancellation delivery steps = %s", got)
				}
			} else if pullRequests.createCount() != 0 || pullRequests.reconcileCount() != 1 {
				t.Fatalf("PR cancellation calls: creates=%d reconciles=%d", pullRequests.createCount(), pullRequests.reconcileCount())
			}
		})
	}
}

func TestFailedCancelSaveRollsBackCancellationIntent(t *testing.T) {
	job, _ := fix.NewJobID()
	record := &jobRecord{presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseFailed, AllowedActions: []fix.JobAction{fix.ActionCancel}}, commands: map[fix.CommandID]CommandReceipt{}}
	manager := &Manager{deps: Dependencies{Store: &failSaveStore{}}, options: Options{Clock: time.Now}, notify: make(chan struct{})}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, reservations: map[string]fix.JobID{}}
	requestID, _ := fix.NewCommandID()
	response := make(chan commandResponse, 1)
	manager.handleCommand(state, commandCall{ctx: t.Context(), command: fix.JobCommand{RequestID: requestID, JobID: job, Action: fix.ActionCancel}, response: response})
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
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory(), Candidates: candidates}, options: Options{Clock: time.Now},
		initial: []jobstore.Record{{JobID: job, State: envelope}}, notify: make(chan struct{})}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{}, reservations: map[string]fix.JobID{}}
	manager.restore(state)
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

func TestDiffInventoryProjectsAllRepositoryChangesAsSupporting(t *testing.T) {
	manager := &Manager{}
	record := &jobRecord{input: FixInput{AllowedPaths: []fix.RepoPath{"target.go", "helper.go"}, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{Targets: []fix.TargetSnapshot{{Path: "target.go", Score: 100, Complete: true}}}}}}
	manager.applyDiffInventory(record, candidate.DiffSnapshot{Fingerprint: "fingerprint", Scope: fix.ScopeViolated, Files: []candidate.DiffFile{
		{Path: "target.go", Status: "modified"}, {Path: "helper.go", Status: "added"}, {Path: "renamed.go", Previous: "outside.go", Status: "renamed"},
		{Path: "target/generated-sources/Generated.java", Status: "added"}, {Path: "target/classes/App.class", Status: "added"},
		{Path: "target/surefire-reports/AppTest.txt", Status: "added"}, {Path: "README.md", Status: "modified"},
	}})
	if len(record.presentation.Targets) != 3 || record.presentation.DiffFingerprint != "fingerprint" {
		t.Fatalf("projection=%+v", record.presentation)
	}
	byPath := map[fix.RepoPath]fix.FilePresentation{}
	for _, file := range record.presentation.Targets {
		byPath[file.Path] = file
	}
	if byPath["target.go"].Classification != "target" || byPath["helper.go"].Classification != "supporting" || byPath["renamed.go"].Classification != "supporting" || byPath["renamed.go"].ScopeViolation || byPath["renamed.go"].PreviousPath != "outside.go" {
		t.Fatalf("files=%+v", byPath)
	}
}

func TestAgentFileEventsDoNotAddArtifactsToTheSupportingFileList(t *testing.T) {
	job, attempt := fix.JobID("job-source-events"), fix.AttemptID("attempt-source-events")
	record := &jobRecord{attempt: attempt, input: FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: 1}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}}
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory()}, options: Options{Clock: time.Now}, notify: make(chan struct{})}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	for _, path := range []fix.RepoPath{"target/classes/App.class", "target/generated-sources/Generated.java", "target/surefire-reports/AppTest.txt", "helper.go"} {
		manager.handleEvent(state, agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventFileChanged, Path: path, Summary: "changed " + path.String()})
	}
	if len(record.presentation.Targets) != 1 || record.presentation.Targets[0].Path != "helper.go" {
		t.Fatalf("agent supporting files = %+v", record.presentation.Targets)
	}
}

func TestUnchangedDiffRefreshPreservesVerifiedBeforeAfterProjection(t *testing.T) {
	score := 42.0
	record := &jobRecord{input: FixInput{AllowedPaths: []fix.RepoPath{"target.go"}, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{Targets: []fix.TargetSnapshot{{Path: "target.go", Score: 88, Complete: true}}}}},
		presentation: fix.JobPresentation{Targets: []fix.FilePresentation{{Path: "target.go", BaselineScore: 88, VerifiedScore: &score, VerifiedMetrics: []fix.MetricValue{{ID: "cog", Value: 4, Complete: true}}, Verification: "verified"}}}}
	manager := &Manager{}
	manager.applyDiffInventory(record, candidate.DiffSnapshot{Fingerprint: "same", Scope: fix.ScopeClean, Files: []candidate.DiffFile{{Path: "target.go", Status: "modified"}}})
	target := record.presentation.Targets[0]
	if target.VerifiedScore == nil || *target.VerifiedScore != 42 || len(target.VerifiedMetrics) != 1 || target.Verification != "verified" {
		t.Fatalf("verified projection lost: %+v", target)
	}
}

func TestRepositoryScopeDiffProjectionDoesNotInventViolations(t *testing.T) {
	record := &jobRecord{input: FixInput{ChangeScope: "repository"}, presentation: fix.JobPresentation{Targets: []fix.FilePresentation{{Path: "target.go", Classification: "target"}}}}
	manager := &Manager{}
	manager.applyDiffInventory(record, candidate.DiffSnapshot{Fingerprint: "repository", Scope: fix.ScopeClean, Files: []candidate.DiffFile{
		{Path: "target.go", Status: "modified"}, {Path: "new.go", Status: "added"}, {Path: "renamed.go", Previous: "old.go", Status: "renamed"},
	}})
	for _, file := range record.presentation.Targets {
		if file.ScopeViolation || file.Classification == "violation" {
			t.Fatalf("repository-scope file %+v was projected as a violation", file)
		}
	}
}

func TestAgentEventsProjectActorTreeActivityAndUsage(t *testing.T) {
	job, attempt := fix.JobID("job-events"), fix.AttemptID("attempt-events")
	record := &jobRecord{attempt: attempt, input: FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: 2}}}, presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}, actors: map[string]bool{}}
	manager := &Manager{options: Options{Clock: time.Now}, notify: make(chan struct{})}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.handleEvent(state, agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventActivity, ActorID: "primary", Summary: "editing", Usage: &agent.Usage{InputTokens: 10, OutputTokens: 2}})
	manager.handleEvent(state, agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventUsage, ActorID: "reviewer", ParentActorID: "primary", Summary: "reviewing", Usage: &agent.Usage{InputTokens: 20, CachedTokens: 5, OutputTokens: 4, Cumulative: true}})
	if len(record.presentation.Actors) != 2 || record.presentation.Actors[1].ParentID != "primary" || record.presentation.Usage.InputTokens != 20 || record.presentation.Usage.CachedTokens != 5 || len(record.logs) != 2 || record.logs[1].ActorID != "reviewer" {
		t.Fatalf("projection actors=%+v usage=%+v logs=%+v", record.presentation.Actors, record.presentation.Usage, record.logs)
	}
}

func TestStoredJobStateOmitsTranscriptText(t *testing.T) {
	job := fix.JobID("job-state")
	record := &jobRecord{
		presentation: fix.JobPresentation{ID: job},
		input: FixInput{
			Instructions: agent.InstructionDocument{Envelope: "full agent prompt that must remain outside job state"},
			Preferences:  appconfig.Resolved{Fix: appconfig.FixDefaults{PromptTemplate: "configured prompt that must remain outside job state"}},
		},
		commands: map[fix.CommandID]CommandReceipt{},
		logs:     []LogEntry{{Summary: "text that must remain outside job state"}},
	}
	encoded, err := json.Marshal((&Manager{}).storedJobState(record))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("must remain outside job state")) || bytes.Contains(encoded, []byte(`"transcript"`)) {
		t.Fatalf("stored job state contains transcript data: %s", encoded)
	}
}

func TestAgentActorProjectionUsesPinnedPerJobLimit(t *testing.T) {
	for _, test := range []struct {
		name, job string
		limit     int
		events    int
		want      int
	}{
		{name: "configured above former compiled ceiling", job: "job-many-actors", limit: 33, events: 33, want: 33},
		{name: "configured cap", job: "job-capped-actors", limit: 2, events: 3, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			job, attempt := fix.JobID(test.job), fix.AttemptID("attempt-actors")
			record := &jobRecord{attempt: attempt,
				input:        FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: test.limit}}},
				presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}, actors: map[string]bool{}}
			manager := &Manager{options: Options{Clock: time.Now}, notify: make(chan struct{})}
			state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
			for index := 0; index < test.events; index++ {
				manager.handleEvent(state, agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventActivity,
					ActorID: fmt.Sprintf("actor-%02d", index), Summary: "working"})
			}
			if len(record.actors) != test.want || record.presentation.ActorCount != test.want || len(record.presentation.Actors) != test.want {
				t.Fatalf("actor projection map=%d count=%d rows=%d, want %d pinned by input", len(record.actors), record.presentation.ActorCount, len(record.presentation.Actors), test.want)
			}
		})
	}
}

type fakeRuntime struct {
	started  chan fix.JobID
	mu       sync.Mutex
	release  map[fix.JobID]chan struct{}
	requests map[fix.JobID][]agent.Request
	eligible bool
	burst    int
}

func (runtime *fakeRuntime) ProfileDescriptor() agent.ProfileDescriptor {
	return agent.ProfileDescriptor{Runtime: "test", Label: "Test"}
}
func (runtime *fakeRuntime) ValidateProfile(agent.Profile) error { return nil }

func (runtime *fakeRuntime) Probe(context.Context, agent.Profile) agent.ProbeResult {
	runtime.mu.Lock()
	eligible := runtime.eligible
	runtime.mu.Unlock()
	return agent.ProbeResult{Runtime: "test", State: agent.ProbeReady, Capabilities: agent.Capabilities{
		Models: []agent.Option[agent.ModelID]{{ID: "gpt-test"}}, Efforts: []agent.Option[agent.EffortID]{{ID: "high"}},
		Progress:  agent.ProgressStructured,
		Isolation: agent.RuntimeIsolation{Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: eligible},
	}}
}

func (runtime *fakeRuntime) Execute(ctx context.Context, _ agent.Profile, request agent.Request, sink agent.EventSink) agent.Result {
	release := make(chan struct{})
	runtime.mu.Lock()
	runtime.release[request.JobID] = release
	runtime.requests[request.JobID] = append(runtime.requests[request.JobID], request)
	burst := runtime.burst
	runtime.mu.Unlock()
	_ = sink.Emit(agent.Event{JobID: request.JobID, AttemptID: request.AttemptID, At: time.Now(), Kind: agent.EventStarted, Summary: "Refactoring"})
	for index := 0; index < burst; index++ {
		_ = sink.Emit(agent.Event{JobID: request.JobID, AttemptID: request.AttemptID, At: time.Now(), Kind: agent.EventActivity, Summary: fmt.Sprintf("Burst event %d", index)})
	}
	runtime.started <- request.JobID
	select {
	case <-release:
		_ = sink.Emit(agent.Event{JobID: request.JobID, AttemptID: request.AttemptID, At: time.Now(), Kind: agent.EventActivity, Summary: "Final streamed activity"})
		return agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultCompleted, SessionReference: "session-" + string(request.JobID)}
	case <-ctx.Done():
		return agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultCanceled, Failure: agent.FailureCancellation}
	}
}

func (runtime *fakeRuntime) complete(id fix.JobID) {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		runtime.mu.Lock()
		channel := runtime.release[id]
		runtime.mu.Unlock()
		if channel != nil {
			close(channel)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

type fakeCandidates struct{}

type cleanupRecoveryCandidates struct {
	fakeCandidates
	reconcileCalls int
	recoverCalls   int
}

func (service *cleanupRecoveryCandidates) ReconcileDiscard(context.Context, fix.CandidateIdentity) error {
	service.reconcileCalls++
	return nil
}

func (service *cleanupRecoveryCandidates) Recover(context.Context, fix.CandidateIdentity, []fix.RepoPath, string, []fix.RepoPath) error {
	service.recoverCalls++
	return errors.New("partial worktree is gone")
}

type changingCandidates struct {
	fakeCandidates
	snapshot candidate.DiffSnapshot
}

func (service *changingCandidates) Diff(context.Context, fix.CandidateIdentity) (candidate.DiffSnapshot, error) {
	return service.snapshot, nil
}

type discoveringCandidates struct {
	fakeCandidates
	identity fix.CandidateIdentity
}

func (service discoveringCandidates) DiscoverPrepared(_ context.Context, request candidate.PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if request.Job == service.identity.Job {
		return service.identity, true, nil
	}
	return fix.CandidateIdentity{}, false, nil
}

func (fakeCandidates) Prepare(_ context.Context, request candidate.PrepareRequest) (fix.CandidateIdentity, error) {
	return fix.CandidateIdentity{Job: request.Job, WorkspaceMode: request.Mode, Repository: request.Workspace.Repository, RepositoryRoot: "/candidate/" + string(request.Job), AnalysisRoot: "/candidate/" + string(request.Job), BaseCommit: request.Workspace.BaseCommit}, nil
}
func (fakeCandidates) DiscoverPrepared(context.Context, candidate.PrepareRequest) (fix.CandidateIdentity, bool, error) {
	return fix.CandidateIdentity{}, false, nil
}
func (fakeCandidates) Diff(context.Context, fix.CandidateIdentity) (candidate.DiffSnapshot, error) {
	return candidate.DiffSnapshot{Scope: fix.ScopeClean, Fingerprint: "diff"}, nil
}
func (fakeCandidates) ReadFile(context.Context, fix.CandidateIdentity, fix.RepoPath, int64) (candidate.File, error) {
	return candidate.File{}, nil
}
func (fakeCandidates) Recover(context.Context, fix.CandidateIdentity, []fix.RepoPath, string, []fix.RepoPath) error {
	return nil
}
func (fakeCandidates) ReconcileDiscard(context.Context, fix.CandidateIdentity) error { return nil }
func (fakeCandidates) Discard(context.Context, fix.CandidateIdentity) error          { return nil }
func (fakeCandidates) Release(context.Context, fix.CandidateIdentity) error          { return nil }
func (fakeCandidates) Close() error                                                  { return nil }

type fakeAnalysis struct{}

func (fakeAnalysis) PrepareBaseline(_ context.Context, request fixanalysis.BaselineRequest) (fixanalysis.BaselineSnapshot, error) {
	targets := make([]fix.TargetSnapshot, 0, len(request.Targets))
	for _, path := range request.Targets {
		targets = append(targets, fix.TargetSnapshot{Path: path, Score: 88, Complete: true})
	}
	return fixanalysis.BaselineSnapshot{Workspace: request.Workspace, Contract: fix.ScoringContract{Targets: targets, Goal: request.Goal, RequireComplete: true}, Fingerprint: "base"}, nil
}
func (fakeAnalysis) Verify(_ context.Context, request fixanalysis.VerificationRequest) (fixanalysis.VerificationResult, error) {
	files := make([]fixanalysis.FileResult, 0, len(request.Contract.Targets))
	for _, target := range request.Contract.Targets {
		files = append(files, fixanalysis.FileResult{Path: target.Path, Score: 42, Complete: true, TargetMet: true})
	}
	return fixanalysis.VerificationResult{Files: files, FingerprintBefore: "same", FingerprintAfter: "same", Complete: true, TargetMet: true}, nil
}

func newTestManager(t *testing.T, maxAgents int) (*Manager, *fakeRuntime) {
	return newTestManagerWithStore(t, maxAgents, jobstore.NewMemory(), Options{})
}

func newTestManagerWithStore(t *testing.T, maxAgents int, store jobstore.Store, options Options) (*Manager, *fakeRuntime) {
	t.Helper()
	runtime := &fakeRuntime{started: make(chan fix.JobID, 20), release: map[fix.JobID]chan struct{}{}, requests: map[fix.JobID][]agent.Request{}, eligible: true}
	registry := agent.NewRegistry()
	if err := registry.Register("test", runtime); err != nil {
		t.Fatal(err)
	}
	config := appconfig.NewMemory(appconfig.Resolved{SchemaVersion: 1, Revision: 1, Origins: map[string]appconfig.Origin{},
		Fix:         appconfig.FixDefaults{TargetScore: 50, Profile: "test-profile", Model: "gpt-test", Effort: "high", ChangeScope: "targets"},
		Concurrency: appconfig.Concurrency{MaxActorsPerJob: 32}, Profiles: []agent.Profile{{ID: "test-profile", Label: "Test", Runtime: "test"}},
		Delivery: appconfig.Delivery{DefaultPlan: testPushPlan, Remote: "origin", BranchTemplate: "fix/{target-stem}-{job-short-id}", Publisher: "github-cli", DraftPullRequests: true},
	})
	options.MaxAgents = maxAgents
	options.MaxVerifiers = 1
	manager, err := New(Dependencies{Config: config, Analysis: fakeAnalysis{}, Candidates: fakeCandidates{}, Agents: registry, Store: store, Delivery: &fakeDeliverySaga{}}, options)
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime
}

func prepare(t *testing.T, manager *Manager, name string) FixInput {
	t.Helper()
	path, _ := fix.ParseRepoPath(name)
	input, err := manager.LoadFix(context.Background(), LoadRequest{Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", GitCommonDir: "/repo/.git", BaseCommit: "abc", CurrentBranch: "main"}, Targets: []fix.RepoPath{path}})
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func loadAndRun(t *testing.T, manager *Manager, name string) fix.JobID {
	t.Helper()
	id, err := manager.Run(context.Background(), prepare(t, manager, name))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func waitStarted(t *testing.T, runtime *fakeRuntime, expected ...fix.JobID) {
	t.Helper()
	want := map[fix.JobID]bool{}
	for _, id := range expected {
		want[id] = true
	}
	for len(want) > 0 {
		select {
		case id := <-runtime.started:
			delete(want, id)
		case <-time.After(time.Second):
			t.Fatalf("jobs did not start: %v", want)
		}
	}
}

func waitForPhase(t *testing.T, manager *Manager, id fix.JobID, phase fix.Phase) fix.JobPresentation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := manager.Job(id)
		if ok && job.Phase == phase {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	job, _ := manager.Job(id)
	t.Fatalf("job %s phase = %s, want %s (issue=%v)", id, job.Phase, phase, job.Issue)
	return fix.JobPresentation{}
}

func shutdownManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil && !errors.Is(err, ErrClosed) {
		t.Errorf("shutdown: %v", err)
	}
}

type fakeDeliverySaga struct {
	mu    sync.Mutex
	steps []string
}

type resolvingDeliverySaga struct{ fakeDeliverySaga }

func (saga *resolvingDeliverySaga) Reconcile(ctx context.Context, request delivery.Request, result delivery.Result) (delivery.Result, error) {
	saga.record("reconcile")
	result.Ambiguous = false
	if result.LocalRef == "" {
		result.LocalRef = "refs/heads/" + request.Branch
	} else {
		result.RemoteRef = result.LocalRef
		result.Pushed = true
	}
	return result, nil
}

func (saga *resolvingDeliverySaga) recorded() string {
	saga.mu.Lock()
	defer saga.mu.Unlock()
	return fmt.Sprint(saga.steps)
}

func (*fakeDeliverySaga) Preflight(context.Context, delivery.PreflightRequest) (delivery.PreflightResult, error) {
	return delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}, nil
}

type fakePublisher struct{}

func (fakePublisher) Preflight(context.Context, publisher.PreflightRequest) (publisher.Readiness, error) {
	return publisher.Readiness{Provider: "github-cli", HostRepository: "owner/repo"}, nil
}

func (fakePublisher) Create(context.Context, publisher.Request) (publisher.Result, error) {
	return publisher.Result{ProviderID: "1", URL: "https://example.test/pull/1", Draft: true}, nil
}

type recordingPublisher struct {
	mu                  sync.Mutex
	creates             int
	reconciles          int
	blockFirstReconcile bool
	started             chan struct{}
	reconcileResult     publisher.Result
}

func (service *recordingPublisher) Preflight(context.Context, publisher.PreflightRequest) (publisher.Readiness, error) {
	return publisher.Readiness{Provider: "github-cli", HostRepository: "owner/repo"}, nil
}

func (service *recordingPublisher) Create(context.Context, publisher.Request) (publisher.Result, error) {
	service.mu.Lock()
	service.creates++
	service.mu.Unlock()
	return publisher.Result{}, errors.New("unexpected pull request create")
}

func (service *recordingPublisher) Reconcile(ctx context.Context, _ publisher.Request, previous publisher.Result) (publisher.Result, error) {
	service.mu.Lock()
	service.reconciles++
	call := service.reconciles
	block := service.blockFirstReconcile && call == 1
	started := service.started
	result := service.reconcileResult
	service.mu.Unlock()
	if block {
		if started != nil {
			started <- struct{}{}
		}
		<-ctx.Done()
		return previous, ctx.Err()
	}
	return result, nil
}

func (service *recordingPublisher) createCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.creates
}

func (service *recordingPublisher) reconcileCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.reconciles
}

func (fakePublisher) Reconcile(context.Context, publisher.Request, publisher.Result) (publisher.Result, error) {
	return publisher.Result{}, nil
}

type failSaveStore struct{}

type failSaveLock struct{}

func (failSaveLock) Close() error { return nil }

func (*failSaveStore) Lock(fix.JobID) (jobstore.Lock, error) { return failSaveLock{}, nil }

func (*failSaveStore) Save(context.Context, jobstore.Record) error {
	return errors.New("save failed")
}
func (*failSaveStore) Load(context.Context) ([]jobstore.Record, error) { return nil, nil }
func (*failSaveStore) Close() error                                    { return nil }

func (saga *fakeDeliverySaga) record(step string) {
	saga.mu.Lock()
	saga.steps = append(saga.steps, step)
	saga.mu.Unlock()
}

func (saga *fakeDeliverySaga) CreateCommit(_ context.Context, _ delivery.Request) (delivery.Result, error) {
	saga.record("commit")
	return delivery.Result{Commit: "commit"}, nil
}
func (saga *fakeDeliverySaga) CreateLocalRef(_ context.Context, _ delivery.Request, result delivery.Result) (delivery.Result, error) {
	saga.record("local")
	result.LocalRef = "refs/heads/slopwatch/fix/one-test"
	return result, nil
}
func (saga *fakeDeliverySaga) CreateRemoteRef(_ context.Context, _ delivery.Request, result delivery.Result) (delivery.Result, error) {
	saga.record("remote")
	result.RemoteRef, result.Pushed = result.LocalRef, true
	return result, nil
}
func (saga *fakeDeliverySaga) Reconcile(_ context.Context, _ delivery.Request, result delivery.Result) (delivery.Result, error) {
	saga.record("reconcile")
	return result, nil
}

func containsFixActionForTest(actions []fix.JobAction, wanted fix.JobAction) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

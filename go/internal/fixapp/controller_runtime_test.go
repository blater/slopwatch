package fixapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

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

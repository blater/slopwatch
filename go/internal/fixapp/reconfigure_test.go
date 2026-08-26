package fixapp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestRuntimeLimitsPreserveExactTranscriptByteBudget(t *testing.T) {
	for _, budget := range []int64{1, 257, 1 << 40} {
		limits := RuntimeLimitsFromConcurrency(appconfig.Concurrency{
			MaxAgents: 1, MaxVerifiers: 1, MaxRetainedJobs: 1, MaxTranscriptBytes: budget,
		})
		if limits.MaxTranscriptBytes != budget {
			t.Fatalf("transcript budget = %d, want exact %d", limits.MaxTranscriptBytes, budget)
		}
	}
}

func TestPrepareRequiresRestartAfterValidationWorkspacePolicyChanges(t *testing.T) {
	startupPolicy := appconfig.ValidationWorkspace{}
	manager, _ := newTestManagerWithStore(t, 1, jobstore.NewMemory(), Options{
		StartupValidationWorkspace: startupPolicy,
	})
	defer shutdownManager(t, manager)

	// The policy installed in the fake validation service and the freshly
	// resolved policy initially agree, so ordinary preparation remains live.
	prepare(t, manager, "before-policy-change.go")
	preparedBefore := len(manager.prepared)

	config, ok := manager.deps.Config.(*appconfig.Memory)
	if !ok {
		t.Fatalf("config service type = %T, want *appconfig.Memory", manager.deps.Config)
	}
	workspace := fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"}
	resolved, err := config.Resolve(t.Context(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	changedPolicy := resolved.ValidationWorkspace
	changedPolicy.MaxFiles = 1
	if _, err := config.Save(t.Context(), workspace, appconfig.ScopeUser, appconfig.Patch{
		ValidationWorkspace: &changedPolicy,
	}, resolved.Revision); err != nil {
		t.Fatal(err)
	}

	path, _ := fix.ParseRepoPath("after-policy-change.go")
	_, err = manager.Prepare(t.Context(), PrepareRequest{Workspace: workspace, Targets: []fix.RepoPath{path}})
	if err == nil || !strings.Contains(err.Error(), "restart Slopwatch") || !strings.Contains(err.Error(), "saved policy is enforced") {
		t.Fatalf("Prepare error = %v, want actionable restart-required diagnostic", err)
	}
	if len(manager.prepared) != preparedBefore {
		t.Fatalf("prepared drafts = %d, want unchanged %d after policy mismatch", len(manager.prepared), preparedBefore)
	}
}

func TestReconfigureExpandsAgentCapacityWithoutRestart(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)

	first := prepareAndSubmit(t, manager, "one.go")
	second := prepareAndSubmit(t, manager, "two.go")
	waitStarted(t, runtime, first)
	if job, _ := manager.Job(second); job.Phase != fix.PhaseQueued {
		t.Fatalf("second phase = %s, want queued", job.Phase)
	}

	if err := manager.Reconfigure(context.Background(), RuntimeLimits{
		MaxAgents: 2, MaxVerifiers: 1, MaxRetainedJobs: 20, MaxTranscriptBytes: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case started := <-runtime.started:
		if started != second {
			t.Fatalf("started %s after reconfigure, want %s", started, second)
		}
	case <-time.After(time.Second):
		t.Fatal("queued job did not start after live capacity increase")
	}
	runtime.complete(first)
	runtime.complete(second)
}

func TestReconfigureReductionDoesNotCancelRunningJobsAndGatesNewScheduling(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)

	first := prepareAndSubmit(t, manager, "one.go")
	second := prepareAndSubmit(t, manager, "two.go")
	waitStarted(t, runtime, first, second)
	if err := manager.Reconfigure(context.Background(), RuntimeLimits{
		MaxAgents: 1, MaxVerifiers: 1, MaxRetainedJobs: 20, MaxTranscriptBytes: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	third := prepareAndSubmit(t, manager, "three.go")
	runtime.complete(first)
	waitForPhase(t, manager, first, fix.PhaseAwaitingReview)
	select {
	case started := <-runtime.started:
		t.Fatalf("job %s started while one pre-existing job still occupied reduced capacity", started)
	case <-time.After(50 * time.Millisecond):
	}
	if job, _ := manager.Job(second); job.Phase != fix.PhaseRunning {
		t.Fatalf("running job was disturbed by capacity reduction: %s", job.Phase)
	}
	runtime.complete(second)
	waitForPhase(t, manager, second, fix.PhaseAwaitingReview)
	select {
	case started := <-runtime.started:
		if started != third {
			t.Fatalf("started %s, want queued job %s", started, third)
		}
	case <-time.After(time.Second):
		t.Fatal("queued job did not start after reduced-capacity slot became free")
	}
	runtime.complete(third)
}

func TestReconfigureTrimsExistingTranscriptsImmediately(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	runtime.mu.Lock()
	runtime.burst = 20
	runtime.mu.Unlock()
	id := prepareAndSubmit(t, manager, "one.go")
	waitStarted(t, runtime, id)
	var before LogPage
	deadline := time.Now().Add(time.Second)
	burstComplete := false
	for !burstComplete && time.Now().Before(deadline) {
		var err error
		before, err = manager.Transcript(context.Background(), id, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range before.Entries {
			if entry.Summary == "Burst event 19" {
				burstComplete = true
				break
			}
		}
		if !burstComplete {
			time.Sleep(time.Millisecond)
		}
	}
	if !burstComplete || len(before.Entries) < 6 {
		t.Fatalf("transcript entries=%d, final burst event visible=%t", len(before.Entries), burstComplete)
	}
	want := append([]LogEntry(nil), before.Entries[len(before.Entries)-5:]...)
	budget := transcriptBytes(want)

	if err := manager.Reconfigure(context.Background(), RuntimeLimits{
		MaxAgents: 1, MaxVerifiers: 1, MaxRetainedJobs: 20, MaxTranscriptBytes: budget,
	}); err != nil {
		t.Fatal(err)
	}
	page, err := manager.Transcript(context.Background(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 5 || !page.Truncated {
		t.Fatalf("transcript entries=%d truncated=%t, want 5/true", len(page.Entries), page.Truncated)
	}
	if transcriptBytes(page.Entries) != budget {
		t.Fatalf("retained transcript bytes=%d, want exact budget %d", transcriptBytes(page.Entries), budget)
	}
	for index := range want {
		if page.Entries[index].Summary != want[index].Summary || page.Entries[index].At != want[index].At {
			t.Fatalf("trim did not retain newest activity: got=%+v want=%+v", page.Entries, want)
		}
	}
	runtime.complete(id)
}

func TestTranscriptByteBudgetHasNoHiddenMinimum(t *testing.T) {
	entry := LogEntry{At: time.Unix(1, 0).UTC(), Summary: strings.Repeat("x", 64)}
	size := transcriptBytes([]LogEntry{entry})
	entries, retained, truncated := appendTranscript(nil, 0, entry, size-1)
	if len(entries) != 0 || retained != 0 || !truncated {
		t.Fatalf("sub-entry budget retained data: entries=%+v bytes=%d truncated=%t", entries, retained, truncated)
	}
	entries, retained, truncated = appendTranscript(nil, 0, entry, size)
	if len(entries) != 1 || retained != size || truncated {
		t.Fatalf("exact entry budget was not honored: entries=%+v bytes=%d truncated=%t", entries, retained, truncated)
	}
}

func TestRestoreTrimsTranscriptToCurrentByteBudget(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	draft := prepare(t, seed, "restore.go")
	dependencies := seed.deps
	shutdownManager(t, seed)

	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: draft.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job),
		AnalysisRoot: "/candidate/" + string(job), BaseCommit: draft.Workspace.BaseCommit}
	entries := []LogEntry{
		{At: time.Unix(1, 0).UTC(), Summary: "old activity"},
		{At: time.Unix(2, 0).UTC(), Summary: "new activity"},
	}
	presentation := fix.JobPresentation{ID: job, Revision: 1, Phase: fix.PhaseAwaitingReview, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	checkpoint, err := json.Marshal(journalEnvelope{Presentation: presentation, Draft: draft, Candidate: &identity,
		Transcript: &transcriptCheckpoint{Entries: entries}})
	if err != nil {
		t.Fatal(err)
	}
	store := jobstore.NewMemory()
	if _, err := store.Append(t.Context(), jobstore.Record{JobID: job, Revision: 1, Kind: "checkpoint", Data: checkpoint}); err != nil {
		t.Fatal(err)
	}
	dependencies.Store = store
	budget := transcriptBytes(entries[1:])
	restarted, err := New(dependencies, Options{MaxAgents: 1, MaxVerifiers: 1, MaxRetainedJobs: 100, MaxTranscriptBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownManager(t, restarted)
	page, err := restarted.Transcript(t.Context(), job, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Summary != "new activity" || transcriptBytes(page.Entries) != budget || !page.Truncated {
		t.Fatalf("restored transcript = %+v bytes=%d truncated=%t", page.Entries, transcriptBytes(page.Entries), page.Truncated)
	}
}

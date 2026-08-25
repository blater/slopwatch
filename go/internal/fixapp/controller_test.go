package fixapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/blater/slopwatch/internal/validation"
)

func TestManagerRunsJobsConcurrentlyAndAdmitsMoreWhileRunning(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)

	first := prepareAndSubmit(t, manager, "one.go")
	second := prepareAndSubmit(t, manager, "two.go")
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

	third := prepareAndSubmit(t, manager, "three.go")
	if got := manager.Jobs(JobFilter{}); len(got.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(got.Jobs))
	}
	if job, _ := manager.Job(third); job.Phase != fix.PhaseQueued {
		t.Fatalf("third phase = %s, want queued", job.Phase)
	}

	runtime.complete(first)
	waitForPhase(t, manager, first, fix.PhaseAwaitingReview)
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
	waitForPhase(t, manager, second, fix.PhaseAwaitingReview)
	waitForPhase(t, manager, third, fix.PhaseAwaitingReview)
}

func TestCancelAffectsOnlySelectedJobAndCommandIsIdempotent(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)
	first := prepareAndSubmit(t, manager, "one.go")
	second := prepareAndSubmit(t, manager, "two.go")
	waitStarted(t, runtime, first, second)

	waitForPhase(t, manager, first, fix.PhaseRunning)
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
	waitForPhase(t, manager, first, fix.PhaseAwaitingAction)
	if secondJob, _ := manager.Job(second); secondJob.Phase != fix.PhaseRunning {
		t.Fatalf("second phase = %s, want running", secondJob.Phase)
	}
	runtime.complete(second)
	waitForPhase(t, manager, second, fix.PhaseAwaitingReview)
}

func TestTargetReservationIsAtomicAndReleasedOnKeep(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	draft := prepare(t, manager, "one.go")
	first, err := manager.Submit(context.Background(), SubmitRequest{Draft: draft})
	if err != nil {
		t.Fatal(err)
	}
	secondDraft := prepare(t, manager, "one.go")
	if _, err := manager.Submit(context.Background(), SubmitRequest{Draft: secondDraft}); !errors.Is(err, ErrTargetReserved) {
		t.Fatalf("second submit error = %v, want target reserved", err)
	}
	waitStarted(t, runtime, first)
	runtime.complete(first)
	job := waitForPhase(t, manager, first, fix.PhaseAwaitingReview)
	commandID, _ := fix.NewCommandID()
	if _, err := manager.Execute(context.Background(), fix.JobCommand{RequestID: commandID, JobID: first, ExpectedRevision: job.Revision, Action: fix.ActionKeep}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(context.Background(), SubmitRequest{Draft: secondDraft}); err != nil {
		t.Fatalf("submit after keep: %v", err)
	}
}

func TestSubscriptionIsLevelTriggered(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	subscription := manager.Subscribe()
	defer subscription.Close()
	initial := manager.Jobs(JobFilter{}).Revision
	id := prepareAndSubmit(t, manager, "one.go")
	revision, err := subscription.Wait(context.Background(), initial)
	if err != nil || revision <= initial {
		t.Fatalf("wait = %d, %v; want newer than %d", revision, err, initial)
	}
	waitStarted(t, runtime, id)
	runtime.complete(id)
}

func TestAgentCompletionCannotOvertakeFinalStreamEvent(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	id := prepareAndSubmit(t, manager, "one.go")
	waitStarted(t, runtime, id)
	runtime.complete(id)
	waitForPhase(t, manager, id, fix.PhaseAwaitingReview)
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

func TestPreparedCandidateIsDurableBeforeAgentLaunch(t *testing.T) {
	store := jobstore.NewMemory()
	manager, runtime := newTestManagerWithStore(t, 1, store, Options{})
	defer shutdownManager(t, manager)
	id := prepareAndSubmit(t, manager, "one.go")
	waitStarted(t, runtime, id)
	records, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	prepared, started := -1, -1
	for index, record := range records {
		if record.JobID != id {
			continue
		}
		switch record.Kind {
		case "candidate_prepared":
			prepared = index
		case "agent_started":
			started = index
		}
	}
	if prepared < 0 || started <= prepared {
		t.Fatalf("journal order does not prove prepared-before-launch: prepared=%d started=%d records=%+v", prepared, started, records)
	}
	runtime.complete(id)
}

func TestTranscriptBurstUsesBoundedCheckpointsAndPhysicalCompaction(t *testing.T) {
	store := &countingStore{Memory: jobstore.NewMemory()}
	manager, runtime := newTestManagerWithStore(t, 1, store, Options{JournalCompactRecords: 6, TranscriptCheckpointEvents: 16})
	defer shutdownManager(t, manager)
	runtime.mu.Lock()
	runtime.burst = 160
	runtime.mu.Unlock()
	id := prepareAndSubmit(t, manager, "one.go")
	waitStarted(t, runtime, id)
	runtime.complete(id)
	waitForPhase(t, manager, id, fix.PhaseAwaitingReview)
	store.mu.Lock()
	appends, compactions := store.appends, store.compactions
	store.mu.Unlock()
	if appends >= 40 {
		t.Fatalf("%d durable appends for a 160-event burst; checkpoints were not coalesced", appends)
	}
	if compactions == 0 {
		t.Fatal("journal threshold did not physically compact the burst")
	}
	records, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) > 7 {
		t.Fatalf("compacted journal retained %d records, want at most checkpoint plus threshold", len(records))
	}
	page, err := manager.Transcript(context.Background(), id, 0, 500)
	if err != nil || len(page.Entries) < 160 {
		t.Fatalf("live bounded transcript lost burst events: entries=%d err=%v", len(page.Entries), err)
	}
}

func TestRestoreDiscoversAndJournalsCandidateLostBeforePreparedHandshake(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	draft := prepare(t, seed, "one.go")
	dependencies := seed.deps
	shutdownManager(t, seed)

	job, _ := fix.NewJobID()
	identity := fix.CandidateIdentity{Job: job, Repository: draft.Workspace.Repository, RepositoryRoot: "/candidate/" + string(job),
		AnalysisRoot: "/candidate/" + string(job), GitCommonDir: draft.Workspace.GitCommonDir, BaseCommit: draft.Workspace.BaseCommit}
	store := jobstore.NewMemory()
	presentation := fix.JobPresentation{ID: job, Revision: 1, Phase: fix.PhaseQueued, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	admitted, _ := json.Marshal(journalEnvelope{Presentation: presentation, Draft: draft})
	if _, err := store.Append(context.Background(), jobstore.Record{JobID: job, Revision: 1, Kind: "admitted", Data: admitted}); err != nil {
		t.Fatal(err)
	}
	presentation.Revision = 2
	presentation.Phase = fix.PhasePreparing
	preparing, _ := json.Marshal(journalEnvelope{Presentation: presentation, Draft: draft})
	if _, err := store.Append(context.Background(), jobstore.Record{JobID: job, Revision: 2, Kind: "candidate_prepare_started", Data: preparing}); err != nil {
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
	if len(records) != 3 || records[2].Kind != "candidate_prepared_recovered" {
		t.Fatalf("recovery journal = %+v", records)
	}
	var recovered journalEnvelope
	if err := json.Unmarshal(records[2].Data, &recovered); err != nil || recovered.Candidate == nil || *recovered.Candidate != identity {
		t.Fatalf("recovered candidate was not durably handshaked: %+v, %v", recovered.Candidate, err)
	}
	jobView, _ := restarted.Job(job)
	if jobView.Phase != fix.PhaseAwaitingAction || jobView.Issue == nil || jobView.Issue.Code != "interrupted" {
		t.Fatalf("restored job = %+v", jobView)
	}
}

func TestNewRejectsMalformedJournalInsteadOfSkippingIt(t *testing.T) {
	seed, _ := newTestManager(t, 1)
	dependencies := seed.deps
	shutdownManager(t, seed)
	store := jobstore.NewMemory()
	job, _ := fix.NewJobID()
	data := json.RawMessage(fmt.Sprintf(`{"presentation":{"id":%q,"revision":1},"draft":{},"unknown_field":true}`, job))
	if _, err := store.Append(context.Background(), jobstore.Record{JobID: job, Revision: 1, Kind: "admitted", Data: data}); err != nil {
		t.Fatal(err)
	}
	dependencies.Store = store
	dependencies.Candidates = fakeCandidates{}
	if manager, err := New(dependencies, Options{}); err == nil {
		shutdownManager(t, manager)
		t.Fatal("malformed journal was silently accepted")
	}
}

func TestSubmitRejectsUnprovenIsolation(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	draft := prepare(t, manager, "one.go")
	runtime.mu.Lock()
	runtime.eligible = false
	runtime.mu.Unlock()
	if _, err := manager.Submit(context.Background(), SubmitRequest{Draft: draft}); err == nil {
		t.Fatal("submit succeeded without proven crash containment")
	}
}

func TestPublicationRunsAsJournalBoundedSagaSteps(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	saga := &fakeDeliverySaga{}
	manager.deps.Delivery = saga
	defer shutdownManager(t, manager)
	draft := prepare(t, manager, "one.go")
	draft, err := ReviseDraft(draft, DraftEdits{TargetScore: draft.TargetScore, Focus: draft.Focus, ChangeScope: draft.ChangeScope,
		DeliveryMode: "branch", BranchName: "slopwatch/fix/one-test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := manager.Submit(context.Background(), SubmitRequest{Draft: draft})
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runtime, id)
	runtime.complete(id)
	job := waitForPhase(t, manager, id, fix.PhaseAwaitingReview)
	if !containsFixActionForTest(job.AllowedActions, fix.ActionPublish) {
		t.Fatalf("publish action missing: %v", job.AllowedActions)
	}
	commandID, _ := fix.NewCommandID()
	if _, err := manager.Execute(context.Background(), fix.JobCommand{RequestID: commandID, JobID: id, ExpectedRevision: job.Revision, Action: fix.ActionPublish}); err != nil {
		t.Fatal(err)
	}
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

func TestPullRequestSubmitRequiresExplicitBaseBranch(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	draft := prepare(t, manager, "one.go")
	draft, err := ReviseDraft(draft, DraftEdits{TargetScore: draft.TargetScore, Focus: draft.Focus, ChangeScope: draft.ChangeScope, DeliveryMode: fix.DeliveryModePullRequest, BranchName: "slopwatch/fix/test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(context.Background(), SubmitRequest{Draft: draft}); err == nil || !strings.Contains(err.Error(), "base branch") {
		t.Fatalf("submit error=%v", err)
	}
}

func TestPullRequestPublishRequiresPublisherAndPassingValidation(t *testing.T) {
	record := &jobRecord{
		draft:     FixDraft{DeliveryMode: "pull-request"},
		candidate: &fix.CandidateIdentity{},
		presentation: fix.JobPresentation{Phase: fix.PhaseAwaitingReview, Compliance: fix.ComplianceCompliant,
			Scope: fix.ScopeClean, Validation: fix.ValidationNotConfigured},
	}
	manager := &Manager{deps: Dependencies{Delivery: &fakeDeliverySaga{}}}
	manager.refreshActions(record)
	if containsFixActionForTest(record.presentation.AllowedActions, fix.ActionPublish) {
		t.Fatal("pull-request publication was allowed without a configured publisher and passing validation")
	}
	manager.deps.Publisher = fakePublisher{}
	manager.refreshActions(record)
	if containsFixActionForTest(record.presentation.AllowedActions, fix.ActionPublish) {
		t.Fatal("pull-request publication was allowed without passing validation")
	}
	record.presentation.Validation = fix.ValidationPassed
	manager.refreshActions(record)
	if !containsFixActionForTest(record.presentation.AllowedActions, fix.ActionPublish) {
		t.Fatal("pull-request publication was not allowed after its dependencies and release gates passed")
	}
}

func TestFailedCommandAppliedRecordRollsBackConflictAcknowledgement(t *testing.T) {
	job := fix.JobID("01JTESTJOB0000000000000000")
	other := fix.JobID("01JTESTJOB0000000000000001")
	record := &jobRecord{
		presentation: fix.JobPresentation{ID: job, Revision: 7, Phase: fix.PhaseAwaitingReview,
			Scope: fix.ScopeConflicted, AllowedActions: []fix.JobAction{fix.ActionAcknowledgeConflict}},
		commands:     map[fix.CommandID]CommandReceipt{},
		conflicts:    map[fix.JobID]string{other: "left\x00right"},
		acknowledged: nil,
	}
	manager := &Manager{deps: Dependencies{Store: &failAppliedStore{}}, options: Options{Clock: time.Now}, notify: make(chan struct{})}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	commandID, _ := fix.NewCommandID()
	response := make(chan commandResponse, 1)
	manager.handleCommand(state, commandCall{ctx: context.Background(), command: fix.JobCommand{
		RequestID: commandID, JobID: job, ExpectedRevision: 7, Action: fix.ActionAcknowledgeConflict,
	}, response: response})
	result := <-response
	if result.err == nil {
		t.Fatal("command succeeded when its applied record failed")
	}
	if len(record.acknowledged) != 0 || record.conflicts[other] != "left\x00right" {
		t.Fatalf("failed command leaked conflict state: acknowledged=%v conflicts=%v", record.acknowledged, record.conflicts)
	}
}

type fakeRuntime struct {
	started  chan fix.JobID
	mu       sync.Mutex
	release  map[fix.JobID]chan struct{}
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
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle}}, Progress: agent.ProgressStructured,
		Isolation: agent.RuntimeIsolation{Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: eligible},
	}}
}

func (runtime *fakeRuntime) Execute(ctx context.Context, _ agent.Profile, request agent.Request, sink agent.EventSink) agent.Result {
	release := make(chan struct{})
	runtime.mu.Lock()
	runtime.release[request.JobID] = release
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
		return agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultCompleted}
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

func (fakeCandidates) Preflight(context.Context, candidate.PreflightRequest) (candidate.PreflightResult, error) {
	return candidate.PreflightResult{Clean: true, Supported: true}, nil
}
func (fakeCandidates) Prepare(_ context.Context, request candidate.PrepareRequest) (fix.CandidateIdentity, error) {
	return fix.CandidateIdentity{Job: request.Job, Repository: request.Workspace.Repository, RepositoryRoot: "/candidate/" + string(request.Job), AnalysisRoot: "/candidate/" + string(request.Job), BaseCommit: request.Workspace.BaseCommit}, nil
}
func (fakeCandidates) DiscoverPrepared(context.Context, candidate.PrepareRequest) (fix.CandidateIdentity, bool, error) {
	return fix.CandidateIdentity{}, false, nil
}
func (fakeCandidates) Diff(context.Context, fix.CandidateIdentity) (candidate.DiffSnapshot, error) {
	return candidate.DiffSnapshot{Scope: fix.ScopeClean, Fingerprint: "diff"}, nil
}
func (fakeCandidates) ReadFile(context.Context, fix.CandidateIdentity, fix.RepoPath) (candidate.File, error) {
	return candidate.File{}, nil
}
func (fakeCandidates) Recover(context.Context, fix.CandidateIdentity, []fix.RepoPath, string, []fix.RepoPath) error {
	return nil
}
func (fakeCandidates) ReconcileDiscard(context.Context, fix.CandidateIdentity) error { return nil }
func (fakeCandidates) Discard(context.Context, fix.CandidateIdentity) error          { return nil }
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
		files = append(files, fixanalysis.FileResult{Path: target.Path, Score: 42, Complete: true, Compliant: true})
	}
	return fixanalysis.VerificationResult{Files: files, FingerprintBefore: "same", FingerprintAfter: "same", Complete: true, Compliant: true}, nil
}

func newTestManager(t *testing.T, maxAgents int) (*Manager, *fakeRuntime) {
	return newTestManagerWithStore(t, maxAgents, jobstore.NewMemory(), Options{})
}

func newTestManagerWithStore(t *testing.T, maxAgents int, store jobstore.Store, options Options) (*Manager, *fakeRuntime) {
	t.Helper()
	runtime := &fakeRuntime{started: make(chan fix.JobID, 20), release: map[fix.JobID]chan struct{}{}, eligible: true}
	registry := agent.NewRegistry()
	if err := registry.Register("test", runtime); err != nil {
		t.Fatal(err)
	}
	config := appconfig.NewMemory(appconfig.Resolved{SchemaVersion: 1, Revision: 1, Origins: map[string]appconfig.Origin{},
		Fix:         appconfig.FixDefaults{TargetScore: 50, Profile: "test-profile", Model: "gpt-test", Effort: "high", Delegation: agent.DelegationSingle, AttemptTimeout: time.Minute, ChangeScope: "targets", BranchTemplate: "fix/{target-stem}-{job-short-id}"},
		Concurrency: appconfig.Concurrency{MaxTranscriptBytes: 1 << 20}, Profiles: []agent.Profile{{ID: "test-profile", Label: "Test", Runtime: "test"}},
		Delivery:   appconfig.Delivery{DefaultMode: fix.DeliveryModeCandidate, Remote: "origin"},
		Validation: []validation.Plan{},
	})
	options.MaxAgents = maxAgents
	options.MaxVerifiers = 1
	manager, err := New(Dependencies{Config: config, Analysis: fakeAnalysis{}, Candidates: fakeCandidates{}, Agents: registry, Store: store}, options)
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime
}

type countingStore struct {
	*jobstore.Memory
	mu          sync.Mutex
	appends     int
	compactions int
}

func (store *countingStore) Append(ctx context.Context, record jobstore.Record) (jobstore.Record, error) {
	store.mu.Lock()
	store.appends++
	store.mu.Unlock()
	return store.Memory.Append(ctx, record)
}

func (store *countingStore) Compact(ctx context.Context, records []jobstore.Record) error {
	store.mu.Lock()
	store.compactions++
	store.mu.Unlock()
	return store.Memory.Compact(ctx, records)
}

func prepare(t *testing.T, manager *Manager, name string) FixDraft {
	t.Helper()
	path, _ := fix.ParseRepoPath(name)
	draft, err := manager.Prepare(context.Background(), PrepareRequest{Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"}, Targets: []fix.RepoPath{path}})
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func prepareAndSubmit(t *testing.T, manager *Manager, name string) fix.JobID {
	t.Helper()
	id, err := manager.Submit(context.Background(), SubmitRequest{Draft: prepare(t, manager, name)})
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

func (*fakeDeliverySaga) Preflight(context.Context, delivery.PreflightRequest) error { return nil }

type fakePublisher struct{}

func (fakePublisher) Create(context.Context, publisher.Request) (publisher.Result, error) {
	return publisher.Result{ProviderID: "1", URL: "https://example.test/pull/1", Draft: true}, nil
}

func (fakePublisher) Reconcile(context.Context, publisher.Request, publisher.Result) (publisher.Result, error) {
	return publisher.Result{}, nil
}

type failAppliedStore struct{ appends int }

func (store *failAppliedStore) Append(_ context.Context, record jobstore.Record) (jobstore.Record, error) {
	store.appends++
	if store.appends == 2 {
		return jobstore.Record{}, errors.New("applied record failed")
	}
	return record, nil
}

func (*failAppliedStore) Load(context.Context) ([]jobstore.Record, error)  { return nil, nil }
func (*failAppliedStore) Compact(context.Context, []jobstore.Record) error { return nil }
func (*failAppliedStore) Close() error                                     { return nil }

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

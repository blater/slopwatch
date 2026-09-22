package fixapp

import (
	"context"
	"errors"
	"fmt"
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

func bareTestManager(deps Dependencies, options Options) *Manager {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	controller := &controller{
		controllerServices: controllerServices{deps: deps, options: options},
		controllerRuntime: controllerRuntime{
			results: make(chan workerResult, 4), notify: make(chan struct{}),
		},
		publication: publicationOwner{candidates: deps.Candidates, delivery: deps.Delivery, preflight: deps.DeliveryPreflight, publisher: deps.Publisher},
		persistence: persistenceOwner{store: deps.Store},
		logging:     loggingOwner{indexPath: options.JobIndexPath, clock: options.Clock},
		workers:     workerOwner{agents: deps.Agents, candidates: deps.Candidates, analysis: deps.Analysis, clock: options.Clock},
		state:       newControllerState(),
	}
	controller.recovery = recoveryOwner{store: deps.Store, candidates: deps.Candidates,
		persistence: &controller.persistence, logging: &controller.logging, clock: options.Clock}
	controller.publication.results = controller.results
	controller.workers.events, controller.workers.results, controller.workers.done = controller.events, controller.results, controller.done
	return &Manager{controller: controller}
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

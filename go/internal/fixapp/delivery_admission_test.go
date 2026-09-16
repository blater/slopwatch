package fixapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/publisher"
)

func TestPullRequestPrepareDefersProviderValidationToPublication(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "gitlab.example", HostRepository: "owner/repo"}}
	manager.deps.Publisher = &sequencedPublisher{}

	input, err := manager.LoadFix(t.Context(), pullRequestPrepareRequest())
	if err != nil {
		t.Fatalf("LoadFix() performed delivery validation: %v", err)
	}
	if input.DeliveryTarget != (delivery.PreflightResult{}) {
		t.Fatalf("LoadFix() started a speculative delivery target: %+v", input.DeliveryTarget)
	}
}

func TestPreparePreservesSessionDeliverySelectionWithoutPreflight(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}}
	request := pullRequestPrepareRequest()
	request.Delivery = &LoadDelivery{Plan: testPushPlan, Branch: "slopwatch/fix/edited"}
	input, err := manager.LoadFix(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if input.DeliveryPlan != testPushPlan || input.BranchName != request.Delivery.Branch || input.DeliveryTarget != (delivery.PreflightResult{}) {
		t.Fatalf("prepared delivery = plan %+v branch %q target %+v", input.DeliveryPlan, input.BranchName, input.DeliveryTarget)
	}
}

func TestPullRequestPrepareDoesNotProbePublisher(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}}
	publisherService := &sequencedPublisher{preflightErrors: []error{errors.New("GitHub authorization is unavailable")}}
	manager.deps.Publisher = publisherService

	if _, err := manager.LoadFix(t.Context(), pullRequestPrepareRequest()); err != nil {
		t.Fatalf("LoadFix() probed publisher: %v", err)
	}
	if publisherService.preflightCount() != 0 {
		t.Fatal("LoadFix() contacted the publisher")
	}
}

func TestPullRequestRunDoesNotProbePublisher(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}}
	publisherService := &sequencedPublisher{preflightErrors: []error{errors.New("GitHub authorization expired")}}
	manager.deps.Publisher = publisherService

	input, err := manager.LoadFix(t.Context(), pullRequestPrepareRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Run(t.Context(), input); err != nil {
		t.Fatalf("Run() probed publisher: %v", err)
	}
	if publisherService.preflightCount() != 0 {
		t.Fatal("Run() contacted the publisher")
	}
}

func TestPublicationTargetChangeIsRejectedBeforeCommit(t *testing.T) {
	saga := &targetChangingDelivery{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "other/repo"}}
	manager := &Manager{deps: Dependencies{Candidates: fakeCandidates{}, Delivery: saga, DeliveryPreflight: saga}, results: make(chan workerResult, 1)}
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	input := FixInput{
		Workspace:      fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
		DeliveryPlan:   testPushPlan,
		DeliveryTarget: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"},
		BranchName:     "slopwatch/fix/test",
		Preferences:    appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin"}},
	}
	manager.runPublicationStep(t.Context(), publicationCommit, input, job, attempt, fix.CandidateIdentity{}, "diff", []fix.RepoPath{"one.go"}, delivery.Result{}, publisher.Result{})
	result := <-manager.results
	if result.err == nil || !strings.Contains(result.err.Error(), "delivery target changed") {
		t.Fatalf("publication error = %v", result.err)
	}
	if saga.commitCount() != 0 {
		t.Fatal("commit was created after the started remote identity changed")
	}
}

func TestPublicationDiscoversDeliveryTargetAtRuntime(t *testing.T) {
	want := delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo", RemoteIdentity: "remote-id"}
	saga := &targetChangingDelivery{target: want}
	manager := &Manager{deps: Dependencies{Candidates: fakeCandidates{}, Delivery: saga, DeliveryPreflight: saga}, results: make(chan workerResult, 1)}
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	input := FixInput{
		Workspace:    fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
		DeliveryPlan: testPushPlan,
		BranchName:   "slopwatch/fix/test",
		Preferences:  appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin"}},
	}
	manager.runPublicationStep(t.Context(), publicationCommit, input, job, attempt, fix.CandidateIdentity{}, "diff", []fix.RepoPath{"one.go"}, delivery.Result{}, publisher.Result{})
	result := <-manager.results
	if result.err != nil || result.deliveryTarget != want || saga.commitCount() != 1 {
		t.Fatalf("runtime delivery discovery: target=%+v commits=%d err=%v", result.deliveryTarget, saga.commitCount(), result.err)
	}
}

func configurePullRequestTest(t *testing.T, manager *Manager) {
	t.Helper()
	store := manager.deps.Config.(*appconfig.Memory)
	resolved, err := store.Resolve(t.Context(), pullRequestPrepareRequest().Workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	resolved.Delivery.DefaultPlan = testPRPlan
	resolved.Delivery.BaseBranch = "main"
	resolved.Delivery.Publisher = "github-cli"
	resolved.Delivery.DraftPullRequests = true
	if _, err := store.Save(t.Context(), pullRequestPrepareRequest().Workspace, appconfig.ScopeUser,
		appconfig.Patch{Fix: &resolved.Fix, Delivery: &resolved.Delivery}, resolved.Revision); err != nil {
		t.Fatal(err)
	}
}

func pullRequestPrepareRequest() LoadRequest {
	return LoadRequest{Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", GitCommonDir: "/repo/.git", BaseCommit: "abc", CurrentBranch: "main"}, Targets: []fix.RepoPath{"one.go"}}
}

func assertNothingStarted(t *testing.T, manager *Manager, runtime *fakeRuntime) {
	t.Helper()
	if jobs := manager.Jobs(JobFilter{}).Jobs; len(jobs) != 0 {
		t.Fatalf("jobs started after failed preflight: %+v", jobs)
	}
	select {
	case job := <-runtime.started:
		t.Fatalf("agent %s started after failed preflight", job)
	default:
	}
}

type staticDeliveryPreflight struct{ target delivery.PreflightResult }

func (service staticDeliveryPreflight) Preflight(context.Context, delivery.PreflightRequest) (delivery.PreflightResult, error) {
	return service.target, nil
}

type sequencedPublisher struct {
	mu              sync.Mutex
	preflightErrors []error
	preflights      int
}

func (service *sequencedPublisher) Preflight(_ context.Context, request publisher.PreflightRequest) (publisher.Readiness, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	index := service.preflights
	service.preflights++
	if index < len(service.preflightErrors) && service.preflightErrors[index] != nil {
		return publisher.Readiness{}, service.preflightErrors[index]
	}
	return publisher.Readiness{Provider: request.Provider, HostRepository: request.HostRepository}, nil
}
func (service *sequencedPublisher) preflightCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.preflights
}
func (*sequencedPublisher) Create(context.Context, publisher.Request) (publisher.Result, error) {
	return publisher.Result{}, nil
}
func (*sequencedPublisher) Reconcile(context.Context, publisher.Request, publisher.Result) (publisher.Result, error) {
	return publisher.Result{}, nil
}

type targetChangingDelivery struct {
	mu      sync.Mutex
	target  delivery.PreflightResult
	commits int
}

func (service *targetChangingDelivery) Preflight(context.Context, delivery.PreflightRequest) (delivery.PreflightResult, error) {
	return service.target, nil
}
func (service *targetChangingDelivery) CreateCommit(context.Context, delivery.Request) (delivery.Result, error) {
	service.mu.Lock()
	service.commits++
	service.mu.Unlock()
	return delivery.Result{Commit: "commit"}, nil
}
func (*targetChangingDelivery) CreateLocalRef(context.Context, delivery.Request, delivery.Result) (delivery.Result, error) {
	return delivery.Result{}, nil
}
func (*targetChangingDelivery) CreateRemoteRef(context.Context, delivery.Request, delivery.Result) (delivery.Result, error) {
	return delivery.Result{}, nil
}
func (*targetChangingDelivery) Reconcile(context.Context, delivery.Request, delivery.Result) (delivery.Result, error) {
	return delivery.Result{}, nil
}
func (service *targetChangingDelivery) commitCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.commits
}

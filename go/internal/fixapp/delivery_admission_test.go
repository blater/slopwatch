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
	"github.com/blater/slopwatch/internal/validation"
)

func TestPullRequestPrepareRejectsNonGitHubRemoteBeforeAdmission(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "gitlab.example", HostRepository: "owner/repo"}}
	manager.deps.Publisher = &sequencedPublisher{}
	manager.deps.Validation = readyValidation{}

	_, err := manager.Prepare(t.Context(), pullRequestPrepareRequest())
	if err == nil || !strings.Contains(err.Error(), "canonical github.com") {
		t.Fatalf("Prepare() error = %v", err)
	}
	assertNothingAdmittedOrStarted(t, manager, runtime)
}

func TestPreparePreflightsExactSessionDeliverySelection(t *testing.T) {
	manager, _ := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	wantTarget := delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: wantTarget}
	request := pullRequestPrepareRequest()
	request.Delivery = &PrepareDelivery{Mode: fix.DeliveryModeBranch, Branch: "slopwatch/fix/edited"}
	draft, err := manager.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if draft.DeliveryMode != fix.DeliveryModeBranch || draft.BranchName != request.Delivery.Branch || draft.DeliveryTarget != wantTarget {
		t.Fatalf("prepared delivery = mode %q branch %q target %+v", draft.DeliveryMode, draft.BranchName, draft.DeliveryTarget)
	}
}

func TestPullRequestPrepareRejectsUnavailablePublisherBeforeAdmission(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}}
	manager.deps.Publisher = &sequencedPublisher{preflightErrors: []error{errors.New("GitHub authorization is unavailable")}}
	manager.deps.Validation = readyValidation{}

	_, err := manager.Prepare(t.Context(), pullRequestPrepareRequest())
	if err == nil || !strings.Contains(err.Error(), "publisher preflight") {
		t.Fatalf("Prepare() error = %v", err)
	}
	assertNothingAdmittedOrStarted(t, manager, runtime)
}

func TestPullRequestSubmitRechecksPublisherBeforeAdmission(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)
	configurePullRequestTest(t, manager)
	manager.deps.DeliveryPreflight = staticDeliveryPreflight{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"}}
	manager.deps.Publisher = &sequencedPublisher{preflightErrors: []error{nil, errors.New("GitHub authorization expired")}}
	manager.deps.Validation = readyValidation{}

	draft, err := manager.Prepare(t.Context(), pullRequestPrepareRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(t.Context(), SubmitRequest{Draft: draft}); err == nil || !strings.Contains(err.Error(), "publisher preflight") {
		t.Fatalf("Submit() error = %v", err)
	}
	assertNothingAdmittedOrStarted(t, manager, runtime)
}

func TestPublicationTargetChangeIsRejectedBeforeCommit(t *testing.T) {
	saga := &targetChangingDelivery{target: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "other/repo"}}
	manager := &Manager{deps: Dependencies{Delivery: saga, DeliveryPreflight: saga}, results: make(chan workerResult, 1)}
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	draft := FixDraft{
		Workspace:      fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
		DeliveryMode:   fix.DeliveryModeBranch,
		DeliveryTarget: delivery.PreflightResult{RemoteHost: "github.com", HostRepository: "owner/repo"},
		BranchName:     "slopwatch/fix/test",
		Preferences:    appconfig.Resolved{Delivery: appconfig.Delivery{Remote: "origin"}},
	}
	manager.runPublicationStep(t.Context(), publicationCommit, draft, job, attempt, fix.CandidateIdentity{}, "diff", delivery.Result{}, publisher.Result{})
	result := <-manager.results
	if result.err == nil || !strings.Contains(result.err.Error(), "delivery target changed") {
		t.Fatalf("publication error = %v", result.err)
	}
	if saga.commitCount() != 0 {
		t.Fatal("commit was created after the admitted remote identity changed")
	}
}

func configurePullRequestTest(t *testing.T, manager *Manager) {
	t.Helper()
	store := manager.deps.Config.(*appconfig.Memory)
	resolved, err := store.Resolve(t.Context(), pullRequestPrepareRequest().Workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	resolved.Fix.ValidationPlan = "release"
	resolved.Delivery.DefaultMode = fix.DeliveryModePullRequest
	resolved.Delivery.BaseBranch = "main"
	resolved.Delivery.Publisher = "github-cli"
	resolved.Delivery.DraftPullRequests = true
	resolved.Validation = []validation.Plan{{ID: "release", Checks: []validation.Check{{ID: "test", Executable: "/usr/bin/true", Required: true}}}}
	if _, err := store.Save(t.Context(), pullRequestPrepareRequest().Workspace, appconfig.ScopeUser,
		appconfig.Patch{Fix: &resolved.Fix, Validation: &resolved.Validation, Delivery: &resolved.Delivery}, resolved.Revision); err != nil {
		t.Fatal(err)
	}
}

func pullRequestPrepareRequest() PrepareRequest {
	return PrepareRequest{Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"}, Targets: []fix.RepoPath{"one.go"}}
}

func assertNothingAdmittedOrStarted(t *testing.T, manager *Manager, runtime *fakeRuntime) {
	t.Helper()
	if jobs := manager.Jobs(JobFilter{}).Jobs; len(jobs) != 0 {
		t.Fatalf("jobs admitted after failed preflight: %+v", jobs)
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

type readyValidation struct{}

func (readyValidation) Preflight(context.Context, fix.WorkspaceIdentity, validation.Plan) validation.Readiness {
	return validation.Readiness{Required: true, Ready: true}
}
func (readyValidation) Validate(context.Context, fix.CandidateIdentity, validation.Plan) (validation.Result, error) {
	return validation.Result{Passed: true, FingerprintBefore: "same", FingerprintAfter: "same"}, nil
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

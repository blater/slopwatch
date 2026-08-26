package fixapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/fixprompt"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
	"github.com/blater/slopwatch/internal/validation"
)

type Dependencies struct {
	Config            appconfig.Resolver
	Analysis          fixanalysis.Service
	Validation        validation.Service
	Candidates        candidate.Service
	ScopePlanner      candidate.ScopePlanner
	Agents            *agent.Registry
	Store             jobstore.Store
	Delivery          delivery.SagaService
	DeliveryPreflight delivery.PreflightService
	Publisher         publisher.Service
	SecretAdmission   SecretAdmission
}

// SecretAdmission checks untrusted user-authored text without exposing secret
// values to orchestration or persistence.
type SecretAdmission interface {
	RejectKnownSecret(context.Context, string, ...string) error
}

type Options struct {
	MaxAgents          int
	MaxVerifiers       int
	MaxRetainedJobs    int
	MaxTranscriptBytes int64
	// StartupValidationWorkspace is the exact workspace and container policy
	// installed in the validation service for this process. Prepare rejects a
	// newly resolved policy that differs from this snapshot, because pinning a
	// policy which the running validator cannot enforce would be misleading.
	StartupValidationWorkspace appconfig.ValidationWorkspace
	JournalCompactRecords      int
	TranscriptCheckpointEvents int
	Clock                      func() time.Time
}

type Manager struct {
	deps     Dependencies
	options  Options
	requests chan any
	events   chan agentUpdate
	results  chan workerResult
	done     chan struct{}
	ready    chan struct{}

	current atomic.Value // JobListSnapshot
	logs    atomic.Value // map[fix.JobID]logSnapshot

	notifyMu       sync.Mutex
	notify         chan struct{}
	closed         atomic.Bool
	initial        []jobstore.Record
	journalRecords int
	journalFailed  bool
	draftMu        sync.Mutex
	prepared       map[fix.DraftID]preparedDraft
}

type jobRecord struct {
	draft                 FixDraft
	presentation          fix.JobPresentation
	attempt               fix.AttemptID
	retryEvidence         string
	candidate             *fix.CandidateIdentity
	cancel                context.CancelFunc
	logs                  []LogEntry
	logBytes              int64
	truncated             bool
	commands              map[fix.CommandID]CommandReceipt
	actors                map[string]bool
	diffHash              string
	diffPaths             map[fix.RepoPath]bool
	baseScope             fix.ScopeState
	conflicts             map[fix.JobID]string
	acknowledged          map[fix.JobID]string
	delivery              delivery.Result
	published             publisher.Result
	eventsSinceCheckpoint int
}

type controllerState struct {
	jobs             map[fix.JobID]*jobRecord
	order            []fix.JobID
	reservations     map[string]fix.JobID
	agentsRunning    int
	verifiersRunning int
	otherRunning     int
	globalRevision   GlobalRevision
	shuttingDown     bool
	shutdownWaiters  []chan error
}

type submitCall struct {
	ctx      context.Context
	request  SubmitRequest
	response chan submitResponse
}

type submitResponse struct {
	id  fix.JobID
	err error
}

type commandCall struct {
	ctx      context.Context
	command  fix.JobCommand
	response chan commandResponse
}

type commandResponse struct {
	receipt CommandReceipt
	err     error
}

type candidateCall struct {
	id       fix.JobID
	response chan candidateResponse
}

type candidateResponse struct {
	identity     fix.CandidateIdentity
	previewBytes int64
	previewLines int
	ok           bool
}

type shutdownCall struct{ response chan error }

type reconfigureCall struct {
	ctx      context.Context
	limits   RuntimeLimits
	response chan error
}

type workerKind uint8

const (
	workerCandidate workerKind = iota
	workerAgent
	workerVerifier
	workerDiscard
	workerPublish
)

type workerResult struct {
	kind         workerKind
	step         publicationStep
	job          fix.JobID
	attempt      fix.AttemptID
	candidate    *fix.CandidateIdentity
	agent        agent.Result
	verify       fixanalysis.VerificationResult
	validation   validation.Result
	diff         candidate.DiffSnapshot
	delivery     delivery.Result
	published    publisher.Result
	err          error
	inventoryErr error
}

type agentUpdate struct {
	event   agent.Event
	barrier chan struct{}
	job     fix.JobID
	attempt fix.AttemptID
}

type publicationStep string

const (
	publicationCommit      publicationStep = "commit"
	publicationLocalRef    publicationStep = "local_ref"
	publicationRemoteRef   publicationStep = "remote_ref"
	publicationPullRequest publicationStep = "pull_request"
	publicationReconcile   publicationStep = "reconcile"
	publicationPRReconcile publicationStep = "pull_request_reconcile"
)

func New(dependencies Dependencies, options Options) (*Manager, error) {
	if dependencies.Config == nil || dependencies.Analysis == nil || dependencies.Candidates == nil ||
		dependencies.Agents == nil || dependencies.Store == nil {
		return nil, errors.New("fix service dependencies are incomplete")
	}
	if options.MaxAgents <= 0 || options.MaxVerifiers <= 0 || options.MaxRetainedJobs <= 0 || options.MaxTranscriptBytes <= 0 {
		return nil, errors.New("fix service requires explicit positive scheduler and retention settings")
	}
	if options.JournalCompactRecords <= 0 {
		options.JournalCompactRecords = 512
	}
	if options.TranscriptCheckpointEvents <= 0 {
		options.TranscriptCheckpointEvents = 32
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	records, err := dependencies.Store.Load(context.Background())
	if err != nil {
		_ = dependencies.Store.Close()
		return nil, fmt.Errorf("load fix job journal: %w", err)
	}
	if err := validateInitialJournal(records); err != nil {
		_ = dependencies.Store.Close()
		return nil, fmt.Errorf("restore fix job journal: %w", err)
	}
	manager := &Manager{
		deps: dependencies, options: options,
		requests: make(chan any), events: make(chan agentUpdate, 256), results: make(chan workerResult, 32),
		done: make(chan struct{}), ready: make(chan struct{}), notify: make(chan struct{}), initial: records, journalRecords: len(records), prepared: map[fix.DraftID]preparedDraft{},
	}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	go manager.run()
	<-manager.ready
	return manager, nil
}

func (manager *Manager) Prepare(ctx context.Context, request PrepareRequest) (FixDraft, error) {
	if manager.closed.Load() {
		return FixDraft{}, ErrClosed
	}
	if len(request.Targets) == 0 {
		return FixDraft{}, errors.New("prepare fix: at least one target is required")
	}
	resolved, err := manager.deps.Config.Resolve(ctx, request.Workspace, request.Overrides)
	if err != nil {
		return FixDraft{}, fmt.Errorf("prepare fix preferences: %w", err)
	}
	if resolved.ValidationWorkspace != manager.options.StartupValidationWorkspace {
		return FixDraft{}, errors.New("prepare fix: validation workspace or container settings changed after Slopwatch started; restart Slopwatch before preparing another Fix so the saved policy is enforced")
	}
	if resolved.Fix.PromptTemplate != "" && resolved.Fix.PromptTemplate != "default" {
		return FixDraft{}, fmt.Errorf("unsupported fix prompt template %q", resolved.Fix.PromptTemplate)
	}
	profile, err := selectedProfile(resolved)
	if err != nil {
		return FixDraft{}, err
	}
	validationReadinessByPlan := manager.preflightValidationPlans(ctx, request.Workspace, resolved.Validation)
	validationReadiness := selectedValidationReadiness(resolved.Fix.ValidationPlan, validationReadinessByPlan)
	strategy, err := manager.deps.Agents.Strategy(profile.Runtime)
	if err != nil {
		return FixDraft{}, err
	}
	goal := fix.ScoringGoal{MaximumScore: resolved.Fix.TargetScore}
	baseline, err := manager.deps.Analysis.PrepareBaseline(ctx, fixanalysis.BaselineRequest{
		Workspace: request.Workspace, Targets: append([]fix.RepoPath(nil), request.Targets...), Goal: goal,
		RequiredMetrics: append([]fix.MetricID(nil), resolved.Fix.Focus...),
	})
	if err != nil {
		return FixDraft{}, fmt.Errorf("prepare fix baseline: %w", err)
	}
	preflight, err := manager.deps.Candidates.Preflight(ctx, candidate.PreflightRequest{
		Workspace: request.Workspace, Targets: append([]fix.RepoPath(nil), request.Targets...),
		CommandOutputBytes: resolved.Delivery.CommandOutputBytes,
	})
	if err != nil {
		return FixDraft{}, fmt.Errorf("prepare fix workspace: %w", err)
	}
	// Freeze the superset needed by every non-repository dialog choice. The
	// user may change scope after Prepare; ReviseDraft must never rediscover the
	// filesystem or silently lose supporting test paths.
	plannedPaths := append([]fix.RepoPath(nil), request.Targets...)
	if manager.deps.ScopePlanner != nil {
		plannedPaths, err = manager.deps.ScopePlanner.Plan(ctx, request.Workspace, request.Targets, "targets-and-tests")
		if err != nil {
			return FixDraft{}, fmt.Errorf("prepare fix change scope: %w", err)
		}
	}
	preflight.AllowedPaths = append([]fix.RepoPath(nil), plannedPaths...)
	allowedPaths := append([]fix.RepoPath(nil), plannedPaths...)
	switch resolved.Fix.ChangeScope {
	case "targets", "targets-only":
		allowedPaths = append([]fix.RepoPath(nil), request.Targets...)
	case "targets-and-tests":
	case "repository":
		allowedPaths = nil
	default:
		return FixDraft{}, fmt.Errorf("unsupported change scope %q", resolved.Fix.ChangeScope)
	}
	probe := strategy.Probe(ctx, profile)
	model, effort, delegation := resolved.Fix.Model, resolved.Fix.Effort, resolved.Fix.Delegation
	if request.Overrides.Profile != nil || model == "" {
		model = compatibleOption(probe.Capabilities.Models, model)
	}
	if request.Overrides.Profile != nil || effort == "" {
		effort = compatibleOption(probe.Capabilities.Efforts, effort)
	}
	if request.Overrides.Profile != nil || delegation == "" {
		delegation = compatibleOption(probe.Capabilities.Delegation, delegation)
	}
	draftID, err := fix.NewDraftID()
	if err != nil {
		return FixDraft{}, err
	}
	branchName := renderBranch(effectiveBranchTemplate(resolved), request.Targets, draftID, resolved.Fix.Focus, manager.options.Clock())
	deliveryMode := resolved.Delivery.DefaultMode
	if request.Delivery != nil {
		deliveryMode = request.Delivery.Mode
		branchName = request.Delivery.Branch
	}
	deliveryTarget, err := manager.preflightDelivery(ctx, request.Workspace, deliveryMode, resolved.Delivery, branchName, false)
	if err != nil {
		return FixDraft{}, err
	}
	baseline.Contract.Goal.Focus, err = configuredFocusGoals(resolved.Fix.Focus, baseline.Contract.Targets)
	if err != nil {
		return FixDraft{}, err
	}
	instructions, err := fixprompt.Compile(fixprompt.Input{
		Contract: baseline.Contract, AllowedScope: resolved.Fix.ChangeScope, AllowedPaths: allowedPaths,
		ValidationPlan: resolved.Fix.ValidationPlan,
	})
	if err != nil {
		return FixDraft{}, err
	}
	draft := FixDraft{
		ID: draftID, Revision: 1, Workspace: request.Workspace,
		Targets: append([]fix.RepoPath(nil), request.Targets...), Baseline: baseline,
		Preferences: resolved, Profile: cloneProfile(profile), Probe: cloneProbe(probe),
		Model: model, Effort: effort, Delegation: delegation,
		TargetScore: resolved.Fix.TargetScore,
		Focus:       append([]fix.MetricGoal(nil), baseline.Contract.Goal.Focus...), ChangeScope: resolved.Fix.ChangeScope,
		AllowedPaths:              append([]fix.RepoPath(nil), allowedPaths...),
		ValidationPlanID:          resolved.Fix.ValidationPlan,
		ValidationReadiness:       validationReadiness,
		ValidationReadinessByPlan: cloneValidationReadiness(validationReadinessByPlan),
		DeliveryMode:              deliveryMode, DeliveryTarget: deliveryTarget, BranchName: branchName,
		Instructions: instructions, Preflight: preflight,
	}
	manager.draftMu.Lock()
	manager.prepared[draft.ID] = preparedDraft{draft: cloneSubmit(SubmitRequest{Draft: draft}).Draft, hash: immutableDraftFingerprint(draft)}
	manager.draftMu.Unlock()
	return draft, nil
}

func compatibleOption[T ~string](options []agent.Option[T], selected T) T {
	if containsOption(options, selected) || len(options) == 0 {
		return selected
	}
	for _, option := range options {
		if option.Default {
			return option.ID
		}
	}
	return options[0].ID
}

func (manager *Manager) Reconfigure(ctx context.Context, limits RuntimeLimits) error {
	if manager.closed.Load() {
		return ErrClosed
	}
	if limits.MaxAgents <= 0 || limits.MaxVerifiers <= 0 || limits.MaxRetainedJobs <= 0 || limits.MaxTranscriptBytes <= 0 {
		return errors.New("fix runtime limits must all be greater than zero")
	}
	response := make(chan error, 1)
	if err := manager.send(ctx, reconfigureCall{ctx: ctx, limits: limits, response: response}); err != nil {
		return err
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-manager.done:
		return ErrClosed
	}
}

// ReviseDraft atomically keeps form values, the verifier contract and the
// generated prompt in sync. The immutable safety envelope is always rebuilt
// by fixprompt and cannot be replaced by an advanced editor.
func ReviseDraft(draft FixDraft, edits DraftEdits) (FixDraft, error) {
	result := cloneSubmit(SubmitRequest{Draft: draft}).Draft
	result.TargetScore = edits.TargetScore
	result.Focus = append([]fix.MetricGoal(nil), edits.Focus...)
	result.ChangeScope = edits.ChangeScope
	switch edits.ChangeScope {
	case "targets", "targets-only":
		result.AllowedPaths = append([]fix.RepoPath(nil), result.Targets...)
	case "targets-and-tests":
		result.AllowedPaths = append([]fix.RepoPath(nil), result.Preflight.AllowedPaths...)
	case "repository":
		result.AllowedPaths = nil
	default:
		return FixDraft{}, fmt.Errorf("unsupported change scope %q", edits.ChangeScope)
	}
	result.ValidationPlanID = edits.ValidationPlanID
	result.ValidationReadiness = selectedValidationReadiness(edits.ValidationPlanID, result.ValidationReadinessByPlan)
	result.DeliveryMode = edits.DeliveryMode
	result.BranchName = edits.BranchName
	result.Baseline.Contract.Goal.MaximumScore = edits.TargetScore
	result.Baseline.Contract.Goal.Focus = append([]fix.MetricGoal(nil), edits.Focus...)
	instructions, err := fixprompt.Compile(fixprompt.Input{
		Contract: result.Baseline.Contract, AllowedScope: edits.ChangeScope, AllowedPaths: result.AllowedPaths, ValidationPlan: edits.ValidationPlanID,
		Guidance: edits.Guidance, DetachedBody: edits.DetachedBody,
	})
	if err != nil {
		return FixDraft{}, err
	}
	result.Instructions = instructions
	result.Revision++
	return result, nil
}

func configuredFocusGoals(ids []fix.MetricID, targets []fix.TargetSnapshot) ([]fix.MetricGoal, error) {
	goals := make([]fix.MetricGoal, 0, len(ids))
	for _, id := range ids {
		maximum := 0.0
		found := false
		for _, target := range targets {
			metric, ok := target.Metrics[id]
			if !ok || !metric.Complete {
				return nil, fmt.Errorf("configured focus metric %q is unavailable or incomplete for target %q", id, target.Path)
			}
			if !found || metric.Value > maximum {
				maximum, found = metric.Value, true
			}
		}
		if found {
			goals = append(goals, fix.MetricGoal{Metric: id, Maximum: maximum})
		}
	}
	return goals, nil
}

func effectiveBranchTemplate(resolved appconfig.Resolved) string {
	deliveryTemplate := strings.TrimSpace(resolved.Delivery.BranchTemplate)
	legacyTemplate := strings.TrimSpace(resolved.Fix.BranchTemplate)
	if deliveryTemplate == "" {
		return legacyTemplate
	}
	// Preferences written before Delivery.BranchTemplate existed may carry an
	// explicit legacy Fix.BranchTemplate alongside the built-in delivery value.
	// Preserve that choice until the user saves the new delivery setting.
	if resolved.Origins["delivery.branch_template"] == appconfig.OriginBuiltIn &&
		resolved.Origins["fix.branch_template"] != "" &&
		resolved.Origins["fix.branch_template"] != appconfig.OriginBuiltIn && legacyTemplate != "" {
		return legacyTemplate
	}
	return deliveryTemplate
}

func selectedProfile(resolved appconfig.Resolved) (agent.Profile, error) {
	for _, profile := range resolved.Profiles {
		if profile.ID == resolved.Fix.Profile {
			return cloneProfile(profile), nil
		}
	}
	return agent.Profile{}, fmt.Errorf("prepare fix: agent profile %q is not configured", resolved.Fix.Profile)
}

func renderBranch(template string, targets []fix.RepoPath, id fix.DraftID, focus []fix.MetricID, now time.Time) string {
	if template == "" {
		template = "slopwatch/fix/{target-stem}-{job-short-id}"
	}
	target := "target"
	if len(targets) > 0 {
		target = targets[0].String()
		if slash := strings.LastIndex(target, "/"); slash >= 0 {
			target = target[slash+1:]
		}
		if dot := strings.LastIndex(target, "."); dot > 0 {
			target = target[:dot]
		}
	}
	short := strings.TrimPrefix(string(id), "draft-")
	if len(short) > 8 {
		short = short[:8]
	}
	metrics := make([]string, 0, len(focus))
	for _, metric := range focus {
		metrics = append(metrics, sanitizeBranchPart(string(metric)))
	}
	return strings.NewReplacer(
		"{target-stem}", sanitizeBranchPart(target),
		"{job-short-id}", short,
		"{date}", now.UTC().Format("20060102"),
		"{metrics}", sanitizeBranchPart(strings.Join(metrics, "-")),
	).Replace(template)
}

func sanitizeBranchPart(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			result.WriteRune(character)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-_")
}

func (manager *Manager) Submit(ctx context.Context, request SubmitRequest) (fix.JobID, error) {
	request = cloneSubmit(request)
	manager.draftMu.Lock()
	expected, prepared := manager.prepared[request.Draft.ID]
	manager.draftMu.Unlock()
	if !prepared || expected.hash != immutableDraftFingerprint(request.Draft) {
		return "", errors.New("submit fix: draft was not prepared by this service or its immutable fields changed")
	}
	reconstructed, err := reconstructPreparedDraft(expected.draft, request.Draft)
	if err != nil {
		return "", err
	}
	request.Draft = reconstructed
	if err := validateDraft(request.Draft); err != nil {
		return "", err
	}
	if err := validateUserInstructionText(request.Draft.Instructions.UserGuidance, request.Draft.Instructions.DetachedBody); err != nil {
		return "", err
	}
	if manager.deps.SecretAdmission != nil {
		// Check the exact compiled body that the adapter will send. Generated
		// target paths and evidence are repository-controlled input too; checking
		// only the advanced-editor fields would leave a provider-visible gap.
		if err := manager.deps.SecretAdmission.RejectKnownSecret(ctx, request.Draft.Profile.AuthenticationRef, request.Draft.Instructions.EffectiveBody()); err != nil {
			return "", errors.New("submit fix: agent instructions contain protected authentication material")
		}
	}
	strategy, err := manager.deps.Agents.Strategy(request.Draft.Profile.Runtime)
	if err != nil {
		return "", err
	}
	request.Draft.Probe = strategy.Probe(ctx, request.Draft.Profile)
	request.Draft.ValidationReadiness = manager.preflightSelectedValidation(ctx, request.Draft.Workspace, request.Draft.ValidationPlanID, request.Draft.Preferences.Validation)
	preflight, err := manager.deps.Candidates.Preflight(ctx, candidate.PreflightRequest{Workspace: request.Draft.Workspace, Targets: request.Draft.Targets, CommandOutputBytes: request.Draft.Preferences.Delivery.CommandOutputBytes})
	if err != nil {
		return "", fmt.Errorf("submit fix preflight: %w", err)
	}
	preflight.AllowedPaths = append([]fix.RepoPath(nil), request.Draft.Preflight.AllowedPaths...)
	request.Draft.Preflight = preflight
	deliveryTarget, err := manager.preflightDelivery(ctx, request.Draft.Workspace, request.Draft.DeliveryMode, request.Draft.Preferences.Delivery, request.Draft.BranchName, false)
	if err != nil {
		return "", err
	}
	request.Draft.DeliveryTarget = deliveryTarget
	response := make(chan submitResponse, 1)
	call := submitCall{ctx: ctx, request: request, response: response}
	if err := manager.send(ctx, call); err != nil {
		return "", err
	}
	select {
	case result := <-response:
		if result.err == nil {
			manager.draftMu.Lock()
			delete(manager.prepared, request.Draft.ID)
			manager.draftMu.Unlock()
		}
		return result.id, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-manager.done:
		return "", ErrClosed
	}
}

func validateUserInstructionText(values ...string) error {
	for _, value := range values {
		for _, character := range value {
			if character == '\n' || character == '\r' || character == '\t' {
				continue
			}
			if character == '\x1b' || unicode.IsControl(character) {
				return errors.New("submit fix: advanced instructions contain terminal or non-text controls")
			}
		}
	}
	return nil
}

func reconstructPreparedDraft(prepared, submitted FixDraft) (FixDraft, error) {
	metricIDs := make([]fix.MetricID, 0, len(submitted.Focus))
	seen := make(map[fix.MetricID]struct{}, len(submitted.Focus))
	for _, goal := range submitted.Focus {
		if goal.Metric == "" {
			return FixDraft{}, errors.New("submit fix: selected focus metric is empty")
		}
		if _, duplicate := seen[goal.Metric]; duplicate {
			return FixDraft{}, fmt.Errorf("submit fix: selected focus metric %q is duplicated", goal.Metric)
		}
		seen[goal.Metric] = struct{}{}
		metricIDs = append(metricIDs, goal.Metric)
	}
	canonicalFocus, err := configuredFocusGoals(metricIDs, prepared.Baseline.Contract.Targets)
	if err != nil {
		return FixDraft{}, fmt.Errorf("submit fix: %w", err)
	}
	reconstructed, err := ReviseDraft(prepared, DraftEdits{
		TargetScore: submitted.TargetScore,
		Focus:       canonicalFocus, ChangeScope: submitted.ChangeScope,
		ValidationPlanID: submitted.ValidationPlanID, DeliveryMode: submitted.DeliveryMode,
		BranchName: submitted.BranchName, Guidance: submitted.Instructions.UserGuidance,
		DetachedBody: submitted.Instructions.DetachedBody,
	})
	if err != nil {
		return FixDraft{}, fmt.Errorf("submit fix: revise prepared draft: %w", err)
	}
	reconstructed.Model = submitted.Model
	reconstructed.Effort = submitted.Effort
	reconstructed.Delegation = submitted.Delegation

	consistent := submitted.TargetScore == reconstructed.TargetScore &&
		reflect.DeepEqual(submitted.Focus, reconstructed.Focus) &&
		submitted.ChangeScope == reconstructed.ChangeScope &&
		reflect.DeepEqual(submitted.AllowedPaths, reconstructed.AllowedPaths) &&
		reflect.DeepEqual(submitted.Preflight.AllowedPaths, prepared.Preflight.AllowedPaths) &&
		submitted.ValidationPlanID == reconstructed.ValidationPlanID &&
		reflect.DeepEqual(submitted.ValidationReadiness, reconstructed.ValidationReadiness) &&
		submitted.DeliveryMode == reconstructed.DeliveryMode &&
		submitted.BranchName == reconstructed.BranchName &&
		reflect.DeepEqual(submitted.Baseline.Contract.Goal, reconstructed.Baseline.Contract.Goal) &&
		reflect.DeepEqual(submitted.Instructions, reconstructed.Instructions)
	if !consistent {
		return FixDraft{}, errors.New("submit fix: draft edits are inconsistent with the prepared scope, scoring contract, or instructions")
	}
	return reconstructed, nil
}

func (manager *Manager) preflightDelivery(ctx context.Context, workspace fix.WorkspaceIdentity, mode fix.DeliveryMode, config appconfig.Delivery, branch string, publication bool) (delivery.PreflightResult, error) {
	if !mode.Valid() {
		return delivery.PreflightResult{}, fmt.Errorf("unsupported delivery mode %q", mode)
	}
	if mode == fix.DeliveryModeCandidate {
		return delivery.PreflightResult{}, nil
	}
	if config.Remote == "" || branch == "" {
		return delivery.PreflightResult{}, errors.New("branch delivery requires a remote and proposed branch")
	}
	if mode == fix.DeliveryModePullRequest && config.BaseBranch == "" {
		return delivery.PreflightResult{}, errors.New("pull-request delivery requires an explicit base branch")
	}
	preflight := manager.deps.DeliveryPreflight
	if preflight == nil {
		preflight, _ = manager.deps.Delivery.(delivery.PreflightService)
	}
	if preflight == nil {
		return delivery.PreflightResult{}, errors.New("delivery preflight service is unavailable")
	}
	target, err := preflight.Preflight(ctx, delivery.PreflightRequest{Workspace: workspace, Mode: mode, Remote: config.Remote, BaseBranch: config.BaseBranch, Branch: branch, Publication: publication, CommandOutputBytes: config.CommandOutputBytes})
	if err != nil {
		return delivery.PreflightResult{}, fmt.Errorf("delivery preflight: %w", err)
	}
	if mode == fix.DeliveryModePullRequest {
		if config.Publisher != "github-cli" {
			return delivery.PreflightResult{}, fmt.Errorf("pull-request publisher %q is unsupported", config.Publisher)
		}
		if !strings.EqualFold(target.RemoteHost, "github.com") || target.HostRepository == "" {
			return delivery.PreflightResult{}, errors.New("pull-request delivery requires a canonical github.com owner/repository remote")
		}
		publisherPreflight, ok := manager.deps.Publisher.(publisher.PreflightService)
		if !ok {
			return delivery.PreflightResult{}, errors.New("pull-request publisher preflight is unavailable")
		}
		if _, err := publisherPreflight.Preflight(ctx, publisher.PreflightRequest{Provider: config.Publisher, RepositoryRoot: workspace.RepositoryRoot,
			RemoteHost: target.RemoteHost, HostRepository: target.HostRepository, Draft: config.DraftPullRequests, CommandOutputBytes: config.CommandOutputBytes}); err != nil {
			return delivery.PreflightResult{}, fmt.Errorf("publisher preflight: %w", err)
		}
	}
	return target, nil
}

func (manager *Manager) preflightValidationPlans(ctx context.Context, workspace fix.WorkspaceIdentity, plans []validation.Plan) map[string]validation.Readiness {
	result := make(map[string]validation.Readiness, len(plans))
	for _, plan := range plans {
		result[plan.ID] = manager.preflightValidation(ctx, workspace, plan)
	}
	return result
}

func (manager *Manager) preflightSelectedValidation(ctx context.Context, workspace fix.WorkspaceIdentity, id string, plans []validation.Plan) validation.Readiness {
	if id == "" {
		return validation.Readiness{Ready: true}
	}
	for _, plan := range plans {
		if plan.ID == id {
			return manager.preflightValidation(ctx, workspace, plan)
		}
	}
	return validation.Readiness{Required: true, Diagnostic: fmt.Sprintf("validation plan %q is unavailable", id)}
}

func (manager *Manager) preflightValidation(ctx context.Context, workspace fix.WorkspaceIdentity, plan validation.Plan) validation.Readiness {
	if manager.deps.Validation == nil {
		return validation.Readiness{Required: true, Diagnostic: "validation service is unavailable"}
	}
	result := manager.deps.Validation.Preflight(ctx, workspace, plan)
	result.Required = true
	if !result.Ready && result.Diagnostic == "" {
		result.Diagnostic = "validation executor is unavailable"
	}
	return result
}

func selectedValidationReadiness(id string, values map[string]validation.Readiness) validation.Readiness {
	if id == "" {
		return validation.Readiness{Ready: true}
	}
	if value, ok := values[id]; ok {
		value.Required = true
		return value
	}
	return validation.Readiness{Required: true, Diagnostic: fmt.Sprintf("validation plan %q is unavailable", id)}
}

func cloneValidationReadiness(values map[string]validation.Readiness) map[string]validation.Readiness {
	result := make(map[string]validation.Readiness, len(values))
	for id, value := range values {
		result[id] = value
	}
	return result
}

type preparedDraft struct {
	draft FixDraft
	hash  [sha256.Size]byte
}

func immutableDraftFingerprint(draft FixDraft) [sha256.Size]byte {
	contract := cloneContract(draft.Baseline.Contract)
	contract.Goal = fix.ScoringGoal{}
	value := struct {
		ID                        fix.DraftID
		Workspace                 fix.WorkspaceIdentity
		Targets                   []fix.RepoPath
		Contract                  fix.ScoringContract
		Preferences               appconfig.Resolved
		Profile                   agent.Profile
		Envelope                  string
		Version                   string
		ValidationReadinessByPlan map[string]validation.Readiness
	}{draft.ID, draft.Workspace, draft.Targets, contract, cloneResolved(draft.Preferences), cloneProfile(draft.Profile), draft.Instructions.Envelope, draft.Instructions.Version, cloneValidationReadiness(draft.ValidationReadinessByPlan)}
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}

func (manager *Manager) Execute(ctx context.Context, command fix.JobCommand) (CommandReceipt, error) {
	response := make(chan commandResponse, 1)
	if err := manager.send(ctx, commandCall{ctx: ctx, command: command, response: response}); err != nil {
		return CommandReceipt{}, err
	}
	select {
	case result := <-response:
		return result.receipt, result.err
	case <-ctx.Done():
		return CommandReceipt{}, ctx.Err()
	case <-manager.done:
		return CommandReceipt{}, ErrClosed
	}
}

func (manager *Manager) send(ctx context.Context, request any) error {
	if manager.closed.Load() {
		return ErrClosed
	}
	select {
	case manager.requests <- request:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-manager.done:
		return ErrClosed
	}
}

func (manager *Manager) Jobs(filter JobFilter) JobListSnapshot {
	snapshot := cloneJobList(manager.current.Load().(JobListSnapshot))
	if !filter.ActiveOnly && filter.IncludeArchived {
		return snapshot
	}
	filtered := snapshot.Jobs[:0]
	for _, job := range snapshot.Jobs {
		if !filter.IncludeArchived && job.Phase == fix.PhaseArchived {
			continue
		}
		if filter.ActiveOnly && isQuiescent(job.Phase) {
			continue
		}
		filtered = append(filtered, job)
	}
	snapshot.Jobs = filtered
	return snapshot
}

func (manager *Manager) Job(id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range manager.Jobs(JobFilter{IncludeArchived: true}).Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func (manager *Manager) Subscribe() Subscription {
	return &subscription{manager: manager, closed: make(chan struct{})}
}

func (manager *Manager) Shutdown(ctx context.Context) error {
	if manager.closed.Load() {
		return nil
	}
	response := make(chan error, 1)
	if err := manager.send(ctx, shutdownCall{response: response}); err != nil {
		if errors.Is(err, ErrClosed) {
			return nil
		}
		return err
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (manager *Manager) CandidateFile(ctx context.Context, id fix.JobID, path fix.RepoPath) (candidate.File, error) {
	identity, previewBytes, previewLines, err := manager.candidateIdentity(ctx, id)
	if err != nil {
		return candidate.File{}, err
	}
	file, err := manager.deps.Candidates.ReadFile(ctx, identity, path, previewBytes)
	if err != nil {
		return candidate.File{}, err
	}
	if previewLines <= 0 {
		return candidate.File{}, errors.New("candidate preview line limit is not configured")
	}
	lines := bytes.Split(file.Contents, []byte("\n"))
	if len(lines) > previewLines {
		file.Contents = bytes.Join(lines[:previewLines], []byte("\n"))
		file.Truncated = true
	}
	return file, nil
}

func (manager *Manager) Diff(ctx context.Context, id fix.JobID, request DiffRequest) (DiffPage, error) {
	identity, _, _, err := manager.candidateIdentity(ctx, id)
	if err != nil {
		return DiffPage{}, err
	}
	snapshot, err := manager.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		return DiffPage{}, err
	}
	offset := max(0, min(request.Offset, len(snapshot.Files)))
	limit := request.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	end := min(len(snapshot.Files), offset+limit)
	return DiffPage{
		Files: append([]candidate.DiffFile(nil), snapshot.Files[offset:end]...), Offset: offset,
		NextOffset: end, Complete: end == len(snapshot.Files), Fingerprint: snapshot.Fingerprint,
	}, nil
}

func (manager *Manager) candidateIdentity(ctx context.Context, id fix.JobID) (fix.CandidateIdentity, int64, int, error) {
	response := make(chan candidateResponse, 1)
	if err := manager.send(ctx, candidateCall{id: id, response: response}); err != nil {
		return fix.CandidateIdentity{}, 0, 0, err
	}
	select {
	case result := <-response:
		if !result.ok {
			return fix.CandidateIdentity{}, 0, 0, ErrJobNotFound
		}
		return result.identity, result.previewBytes, result.previewLines, nil
	case <-ctx.Done():
		return fix.CandidateIdentity{}, 0, 0, ctx.Err()
	}
}

type logSnapshot struct {
	entries   []LogEntry
	truncated bool
}

func (manager *Manager) Transcript(_ context.Context, id fix.JobID, cursor LogCursor, limit int) (LogPage, error) {
	all := manager.logs.Load().(map[fix.JobID]logSnapshot)
	snapshot, exists := all[id]
	if !exists {
		return LogPage{}, ErrJobNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	start := max(0, min(int(cursor), len(snapshot.entries)))
	end := min(len(snapshot.entries), start+limit)
	return LogPage{
		Entries: append([]LogEntry(nil), snapshot.entries[start:end]...), Next: LogCursor(end),
		Complete: end == len(snapshot.entries), Truncated: snapshot.truncated,
	}, nil
}

func cloneSubmit(request SubmitRequest) SubmitRequest {
	request.Draft.Targets = append([]fix.RepoPath(nil), request.Draft.Targets...)
	request.Draft.AllowedPaths = append([]fix.RepoPath(nil), request.Draft.AllowedPaths...)
	request.Draft.Preflight.AllowedPaths = append([]fix.RepoPath(nil), request.Draft.Preflight.AllowedPaths...)
	request.Draft.Preflight.TargetBlobs = cloneObjectIDs(request.Draft.Preflight.TargetBlobs)
	request.Draft.ValidationReadinessByPlan = cloneValidationReadiness(request.Draft.ValidationReadinessByPlan)
	request.Draft.Profile = cloneProfile(request.Draft.Profile)
	request.Draft.Probe = cloneProbe(request.Draft.Probe)
	request.Draft.Preferences = cloneResolved(request.Draft.Preferences)
	request.Draft.Baseline.Contract = cloneContract(request.Draft.Baseline.Contract)
	request.Draft.Focus = append([]fix.MetricGoal(nil), request.Draft.Focus...)
	return request
}

func cloneObjectIDs(value map[fix.RepoPath]fix.ObjectID) map[fix.RepoPath]fix.ObjectID {
	result := make(map[fix.RepoPath]fix.ObjectID, len(value))
	for path, id := range value {
		result[path] = id
	}
	return result
}

func cloneResolved(value appconfig.Resolved) appconfig.Resolved {
	result := value
	result.Origins = make(map[string]appconfig.Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix.Focus = append([]fix.MetricID(nil), value.Fix.Focus...)
	result.Profiles = make([]agent.Profile, len(value.Profiles))
	for index, profile := range value.Profiles {
		result.Profiles[index] = cloneProfile(profile)
	}
	result.Validation = make([]validation.Plan, len(value.Validation))
	for index, plan := range value.Validation {
		result.Validation[index] = plan
		result.Validation[index].Checks = make([]validation.Check, len(plan.Checks))
		for checkIndex, check := range plan.Checks {
			result.Validation[index].Checks[checkIndex] = check
			result.Validation[index].Checks[checkIndex].Arguments = append([]string(nil), check.Arguments...)
		}
	}
	return result
}

func cloneContract(value fix.ScoringContract) fix.ScoringContract {
	result := value
	result.Goal.Focus = append([]fix.MetricGoal(nil), value.Goal.Focus...)
	result.Goal.AllowedRegression = make(map[fix.MetricID]float64, len(value.Goal.AllowedRegression))
	for metric, allowance := range value.Goal.AllowedRegression {
		result.Goal.AllowedRegression[metric] = allowance
	}
	result.Targets = make([]fix.TargetSnapshot, len(value.Targets))
	for index, target := range value.Targets {
		result.Targets[index] = target
		result.Targets[index].Metrics = make(map[fix.MetricID]fix.MetricValue, len(target.Metrics))
		for metric, metricValue := range target.Metrics {
			result.Targets[index].Metrics[metric] = metricValue
		}
		result.Targets[index].Evidence = append([]fix.MetricEvidence(nil), target.Evidence...)
	}
	return result
}

func cloneProfile(profile agent.Profile) agent.Profile {
	profile.Options = cloneStringMap(profile.Options)
	return profile
}

func cloneProbe(probe agent.ProbeResult) agent.ProbeResult {
	probe.Capabilities.Models = append([]agent.Option[agent.ModelID](nil), probe.Capabilities.Models...)
	probe.Capabilities.Efforts = append([]agent.Option[agent.EffortID](nil), probe.Capabilities.Efforts...)
	probe.Capabilities.Delegation = append([]agent.Option[agent.DelegationMode](nil), probe.Capabilities.Delegation...)
	probe.Capabilities.Network.ToolDomains = append([]string(nil), probe.Capabilities.Network.ToolDomains...)
	return probe
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func isQuiescent(phase fix.Phase) bool {
	return phase == fix.PhaseAwaitingAction || phase == fix.PhaseAwaitingReview ||
		phase == fix.PhaseCompleted || phase == fix.PhaseArchived || phase == fix.PhaseDiscarded
}

func admissionPayload(draft FixDraft) json.RawMessage {
	payload, _ := json.Marshal(struct {
		Draft       fix.DraftID           `json:"draft"`
		Workspace   fix.WorkspaceIdentity `json:"workspace"`
		Targets     []fix.RepoPath        `json:"targets"`
		Contract    fix.ScoringContract   `json:"scoring_contract"`
		Profile     agent.ProfileID       `json:"profile"`
		Runtime     agent.RuntimeKind     `json:"runtime"`
		Fingerprint string                `json:"profile_fingerprint"`
		Model       agent.ModelID         `json:"model"`
		Effort      agent.EffortID        `json:"effort"`
		Delegation  agent.DelegationMode  `json:"delegation"`
		Scope       string                `json:"scope"`
	}{draft.ID, draft.Workspace, draft.Targets, draft.Baseline.Contract, draft.Profile.ID,
		draft.Profile.Runtime, draft.Profile.Fingerprint, draft.Model, draft.Effort, draft.Delegation, draft.ChangeScope})
	return payload
}

func sortPresentations(values []fix.JobPresentation) {
	sort.SliceStable(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
}

package fixapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/appconfig"
	"github.com/blater/slopmochi/internal/candidate"
	"github.com/blater/slopmochi/internal/delivery"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixanalysis"
	"github.com/blater/slopmochi/internal/fixprompt"
	"github.com/blater/slopmochi/internal/jobstore"
	"github.com/blater/slopmochi/internal/publisher"
)

type Dependencies struct {
	Config            appconfig.Resolver
	Analysis          fixanalysis.Service
	Candidates        candidate.Service
	ScopePlanner      candidate.ScopePlanner
	Agents            *agent.Registry
	Store             jobstore.Store
	Delivery          delivery.SagaService
	DeliveryPreflight delivery.PreflightService
	Publisher         publisher.Service
}

type Options struct {
	MaxAgents    int
	MaxVerifiers int
	JobIndexPath string
	Clock        func() time.Time
}

type Manager struct {
	deps     Dependencies
	options  Options
	requests chan any
	events   chan agentUpdate
	results  chan workerResult
	done     chan struct{}
	ready    chan struct{}

	notifyMu sync.Mutex
	jobLogMu sync.Mutex
	notify   chan struct{}
	closed   atomic.Bool
	initial  []jobstore.Record
}

type jobRecord struct {
	input            FixInput
	presentation     fix.JobPresentation
	attempt          fix.AttemptID
	nextAttemptNotes string
	candidate        *fix.CandidateIdentity
	cancel           context.CancelFunc
	logs             []LogEntry
	commands         map[fix.CommandID]CommandReceipt
	actors           map[string]bool
	diffHash         string
	diffPaths        map[fix.RepoPath]bool
	baseScope        fix.ScopeState
	delivery         delivery.Result
	published        publisher.Result
	canceled         bool
	publicationStep  publicationStep
	resultLogged     bool
	agentReferences  []string
	runLock          jobstore.Lock
	runsHere         bool
	runsElsewhere    bool
	storedAt         time.Time
}

type controllerState struct {
	jobs             map[fix.JobID]*jobRecord
	order            []fix.JobID
	reservations     map[string]fix.JobID
	agentsRunning    int
	verifiersRunning int
	otherRunning     int
	shuttingDown     bool
	shutdownWaiters  []chan error
}

type runCall struct {
	ctx      context.Context
	input    FixInput
	response chan runResponse
}

type runResponse struct {
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

type jobsCall struct {
	filter   JobFilter
	response chan JobListSnapshot
}

type transcriptCall struct {
	id       fix.JobID
	cursor   LogCursor
	limit    int
	response chan transcriptResponse
}

type transcriptResponse struct {
	page LogPage
	err  error
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
	workerCleanup
	workerPublish
)

type workerResult struct {
	kind           workerKind
	job            fix.JobID
	attempt        fix.AttemptID
	candidate      *fix.CandidateIdentity
	agent          agent.Result
	verify         fixanalysis.VerificationResult
	diff           candidate.DiffSnapshot
	delivery       delivery.Result
	deliveryTarget delivery.PreflightResult
	published      publisher.Result
	err            error
	inventoryErr   error
}

type agentUpdate struct {
	event   agent.Event
	prompt  *string
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
	if options.MaxAgents <= 0 || options.MaxVerifiers <= 0 {
		return nil, errors.New("fix service requires explicit positive scheduler settings")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	records, err := dependencies.Store.Load(context.Background())
	if err != nil {
		_ = dependencies.Store.Close()
		return nil, fmt.Errorf("load fix job state: %w", err)
	}
	manager := &Manager{
		deps: dependencies, options: options,
		requests: make(chan any), events: make(chan agentUpdate, 256), results: make(chan workerResult, 32),
		done: make(chan struct{}), ready: make(chan struct{}), notify: make(chan struct{}), initial: records,
	}
	go manager.run()
	<-manager.ready
	return manager, nil
}

func (manager *Manager) LoadFix(ctx context.Context, request LoadRequest) (FixInput, error) {
	if manager.closed.Load() {
		return FixInput{}, ErrClosed
	}
	if len(request.Targets) == 0 {
		return FixInput{}, errors.New("load fix: at least one target is required")
	}
	resolved, err := manager.deps.Config.Resolve(ctx, request.Workspace, request.Overrides)
	if err != nil {
		return FixInput{}, fmt.Errorf("load fix preferences: %w", err)
	}
	profile, err := selectedProfile(resolved)
	if err != nil {
		return FixInput{}, err
	}
	goal := fix.ScoringGoal{MaximumScore: resolved.Fix.TargetScore}
	baseline, err := manager.deps.Analysis.PrepareBaseline(ctx, fixanalysis.BaselineRequest{
		Workspace: request.Workspace, Targets: append([]fix.RepoPath(nil), request.Targets...), Goal: goal,
		RequiredMetrics: measuredFocusMetrics(resolved.Fix.Focus),
	})
	if err != nil {
		return FixInput{}, fmt.Errorf("load fix baseline: %w", err)
	}
	// Load the supporting paths once while opening the form.
	plannedPaths := append([]fix.RepoPath(nil), request.Targets...)
	if manager.deps.ScopePlanner != nil {
		plannedPaths, err = manager.deps.ScopePlanner.Plan(ctx, request.Workspace, request.Targets, "targets-and-tests")
		if err != nil {
			return FixInput{}, fmt.Errorf("load fix change scope: %w", err)
		}
	}
	allowedPaths := append([]fix.RepoPath(nil), plannedPaths...)
	switch resolved.Fix.ChangeScope {
	case "targets", "targets-only":
		allowedPaths = append([]fix.RepoPath(nil), request.Targets...)
	case "targets-and-tests":
	case "repository":
		allowedPaths = nil
	default:
		return FixInput{}, fmt.Errorf("unsupported change scope %q", resolved.Fix.ChangeScope)
	}
	probe := manager.deps.Agents.Probe(ctx, profile)
	model, effort := resolved.Fix.Model, resolved.Fix.Effort
	if selected, ok := agent.ResolveOption(probe.Capabilities.Models, model); ok {
		model = selected
	}
	if selected, ok := agent.ResolveOption(probe.Capabilities.Efforts, effort); ok {
		effort = selected
	}
	branchSeed, err := fix.NewJobID()
	if err != nil {
		return FixInput{}, err
	}
	branchName := renderBranch(effectiveBranchTemplate(resolved), request.Targets, string(branchSeed), resolved.Fix.Focus, manager.options.Clock())
	deliveryPlan := resolved.Delivery.DefaultPlan
	if request.Delivery != nil {
		deliveryPlan = request.Delivery.Plan
		branchName = request.Delivery.Branch
	}
	if request.Workspace.GitCommonDir == "" {
		deliveryPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitLeaveUncommitted, Publish: fix.PublishLocal}
	}
	if deliveryPlan.Workspace == fix.WorkspaceWorktree && deliveryPlan.Git == fix.GitCommitCurrent {
		deliveryPlan.Git, deliveryPlan.Publish = fix.GitLeaveUncommitted, fix.PublishLocal
	}
	if deliveryPlan.Git == fix.GitCommitCurrent {
		branchName = request.Workspace.CurrentBranch
	} else if deliveryPlan.Git == fix.GitLeaveUncommitted {
		branchName = ""
	}
	baseline.Contract.Goal.Focus, err = configuredFocusGoals(resolved.Fix.Focus, baseline.Contract.Targets, resolved.Fix.TargetScore)
	if err != nil {
		return FixInput{}, err
	}
	instructions, err := fixprompt.Compile(fixprompt.Input{
		Contract: baseline.Contract, AllowedScope: resolved.Fix.ChangeScope, AllowedPaths: allowedPaths,
		BranchName: branchName, Template: resolved.Fix.PromptTemplate,
	})
	if err != nil {
		return FixInput{}, err
	}
	input := FixInput{
		Workspace: request.Workspace,
		Targets:   append([]fix.RepoPath(nil), request.Targets...), Baseline: baseline,
		Preferences: resolved, Profile: cloneProfile(profile), Probe: cloneProbe(probe),
		Model: model, Effort: effort,
		TargetScore: resolved.Fix.TargetScore,
		Focus:       append([]fix.MetricGoal(nil), baseline.Contract.Goal.Focus...), ChangeScope: resolved.Fix.ChangeScope,
		AllowedPaths: append([]fix.RepoPath(nil), allowedPaths...),
		DeliveryPlan: deliveryPlan, BranchName: branchName,
		Instructions: instructions, PlannedPaths: append([]fix.RepoPath(nil), plannedPaths...),
	}
	return input, nil
}

func (manager *Manager) Reconfigure(ctx context.Context, limits RuntimeLimits) error {
	if manager.closed.Load() {
		return ErrClosed
	}
	if limits.MaxAgents <= 0 || limits.MaxVerifiers <= 0 {
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

func ApplyFormValues(input FixInput, values FormValues) (FixInput, error) {
	result := cloneFixInput(input)
	result.TargetScore = values.TargetScore
	result.Focus = append([]fix.MetricGoal(nil), values.Focus...)
	result.ChangeScope = values.ChangeScope
	switch values.ChangeScope {
	case "targets", "targets-only":
		result.AllowedPaths = append([]fix.RepoPath(nil), result.Targets...)
	case "targets-and-tests":
		result.AllowedPaths = append([]fix.RepoPath(nil), result.PlannedPaths...)
	case "repository":
		result.AllowedPaths = nil
	default:
		return FixInput{}, fmt.Errorf("unsupported change scope %q", values.ChangeScope)
	}
	result.DeliveryPlan = values.DeliveryPlan
	result.BranchName = values.BranchName
	if values.DeliveryPlan.Git == fix.GitCommitCurrent {
		result.BranchName = result.Workspace.CurrentBranch
	} else if values.DeliveryPlan.Git == fix.GitLeaveUncommitted {
		result.BranchName = ""
	}
	result.Baseline.Contract.Goal.MaximumScore = values.TargetScore
	result.Baseline.Contract.Goal.Focus = append([]fix.MetricGoal(nil), values.Focus...)
	instructions, err := fixprompt.Compile(fixprompt.Input{
		Contract: result.Baseline.Contract, AllowedScope: values.ChangeScope, AllowedPaths: result.AllowedPaths,
		BranchName: result.BranchName, Template: result.Preferences.Fix.PromptTemplate,
	})
	if err != nil {
		return FixInput{}, err
	}
	result.Instructions = instructions
	return result, nil
}

func measuredFocusMetrics(ids []fix.MetricID) []fix.MetricID {
	result := make([]fix.MetricID, 0, len(ids))
	for _, id := range ids {
		if id != fix.MetricScore {
			result = append(result, id)
		}
	}
	return result
}

func configuredFocusGoals(ids []fix.MetricID, targets []fix.TargetSnapshot, targetScore float64) ([]fix.MetricGoal, error) {
	goals := make([]fix.MetricGoal, 0, len(ids))
	for _, id := range ids {
		if id == fix.MetricScore {
			goals = append(goals, fix.MetricGoal{Metric: id, Maximum: targetScore})
			continue
		}
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
	return strings.TrimSpace(resolved.Delivery.BranchTemplate)
}

func selectedProfile(resolved appconfig.Resolved) (agent.Profile, error) {
	for _, profile := range resolved.Profiles {
		if profile.ID == resolved.Fix.Profile {
			return cloneProfile(profile), nil
		}
	}
	return agent.Profile{}, fmt.Errorf("load fix: agent profile %q is not configured", resolved.Fix.Profile)
}

func renderBranch(template string, targets []fix.RepoPath, seed string, focus []fix.MetricID, now time.Time) string {
	if template == "" {
		template = "slopmochi/fix/{target-stem}-{job-short-id}"
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
	short := strings.TrimPrefix(seed, "job-")
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

func (manager *Manager) Run(ctx context.Context, input FixInput) (fix.JobID, error) {
	input = cloneFixInput(input)
	response := make(chan runResponse, 1)
	call := runCall{ctx: ctx, input: input, response: response}
	if err := manager.send(ctx, call); err != nil {
		return "", err
	}
	select {
	case result := <-response:
		return result.id, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-manager.done:
		return "", ErrClosed
	}
}

func (manager *Manager) preflightDelivery(ctx context.Context, workspace fix.WorkspaceIdentity, plan fix.DeliveryPlan, config appconfig.Delivery, branch string, publication bool) (delivery.PreflightResult, error) {
	if !plan.Valid() {
		return delivery.PreflightResult{}, fmt.Errorf("unsupported delivery plan %+v", plan)
	}
	if plan.Git == fix.GitLeaveUncommitted {
		return delivery.PreflightResult{}, nil
	}
	if plan.Git == fix.GitCommitNewBranch && branch == "" {
		return delivery.PreflightResult{}, errors.New("new-branch delivery requires a branch name")
	}
	if plan.Publish != fix.PublishLocal && config.Remote == "" {
		return delivery.PreflightResult{}, errors.New("pushing requires a configured remote")
	}
	if plan.Publish == fix.PublishPullRequest && config.BaseBranch == "" {
		return delivery.PreflightResult{}, errors.New("pull-request delivery requires an explicit base branch")
	}
	preflight := manager.deps.DeliveryPreflight
	if preflight == nil {
		preflight, _ = manager.deps.Delivery.(delivery.PreflightService)
	}
	if preflight == nil {
		return delivery.PreflightResult{}, errors.New("delivery preflight service is unavailable")
	}
	target, err := preflight.Preflight(ctx, delivery.PreflightRequest{Workspace: workspace, Plan: plan, Remote: config.Remote, BaseBranch: config.BaseBranch, Branch: branch, Publication: publication, CommandOutputBytes: config.CommandOutputBytes})
	if err != nil {
		return delivery.PreflightResult{}, fmt.Errorf("delivery preflight: %w", err)
	}
	if plan.Publish == fix.PublishPullRequest {
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
	response := make(chan JobListSnapshot, 1)
	if err := manager.send(context.Background(), jobsCall{filter: filter, response: response}); err != nil {
		return JobListSnapshot{}
	}
	select {
	case result := <-response:
		return result
	case <-manager.done:
		return JobListSnapshot{}
	}
}

func (manager *Manager) Job(id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range manager.Jobs(JobFilter{IncludeFinished: true}).Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func (manager *Manager) Subscribe() Subscription {
	manager.notifyMu.Lock()
	notify := manager.notify
	manager.notifyMu.Unlock()
	return &subscription{manager: manager, notify: notify, closed: make(chan struct{})}
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

func (manager *Manager) Transcript(ctx context.Context, id fix.JobID, cursor LogCursor, limit int) (LogPage, error) {
	response := make(chan transcriptResponse, 1)
	if err := manager.send(ctx, transcriptCall{id: id, cursor: cursor, limit: limit, response: response}); err != nil {
		return LogPage{}, err
	}
	select {
	case result := <-response:
		return result.page, result.err
	case <-ctx.Done():
		return LogPage{}, ctx.Err()
	case <-manager.done:
		return LogPage{}, ErrClosed
	}
}

func cloneFixInput(input FixInput) FixInput {
	input.Targets = append([]fix.RepoPath(nil), input.Targets...)
	input.AllowedPaths = append([]fix.RepoPath(nil), input.AllowedPaths...)
	input.PlannedPaths = append([]fix.RepoPath(nil), input.PlannedPaths...)
	input.Profile = cloneProfile(input.Profile)
	input.Probe = cloneProbe(input.Probe)
	input.Preferences = cloneResolved(input.Preferences)
	input.Baseline.Contract = cloneContract(input.Baseline.Contract)
	input.Focus = append([]fix.MetricGoal(nil), input.Focus...)
	return input
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
	return phase == fix.PhaseFailed || phase == fix.PhaseCompleted || phase == fix.PhaseCanceled || phase == fix.PhaseDiscarded
}

func sortPresentations(values []fix.JobPresentation) {
	sort.SliceStable(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
}

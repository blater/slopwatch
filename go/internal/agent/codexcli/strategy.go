package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

const (
	defaultProbeTimeout     = 15 * time.Second
	defaultTerminationGrace = 5 * time.Second
	defaultReconcileEvery   = time.Second
	diagnosticCaptureLimit  = 8 << 20
)

type probePolicy struct {
	timeout          time.Duration
	terminationGrace time.Duration
}

var (
	appServerVersionPattern = regexp.MustCompile(`/([^\s]+)`)
	providerControlPattern  = regexp.MustCompile(`[^\x09\x0a\x0d\x20-\x7e]`)
	providerBearerPattern   = regexp.MustCompile(`(?i)(authorization\s*[:=]?\s*bearer|bearer)\s+[^\s]+`)
	providerKeyPattern      = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
)

// Strategy adapts Codex App Server to Slopwatch's provider-neutral agent
// contract. Each probe and each fix attempt owns its App Server process, so
// concurrent jobs and job-scoped cancellation remain naturally isolated.
type Strategy struct {
	workingDir     func() (string, error)
	start          func(string, string, []string, int64, func(rpcMessage)) (*appServerClient, error)
	reconcileEvery time.Duration
}

func New() *Strategy {
	return &Strategy{workingDir: os.Getwd, start: startAppServer, reconcileEvery: defaultReconcileEvery}
}

func (*Strategy) ProfileDescriptor() agent.ProfileDescriptor {
	return agent.ProfileDescriptor{
		Runtime: RuntimeKind, Label: "Codex",
		ConnectionInstructions: "Run `codex login`, then complete the browser sign-in. Slopwatch uses the resulting Codex-managed session and does not store the credential.",
		DocumentationURL:       "https://developers.openai.com/codex/auth",
		Fields: []agent.ProfileField{
			{Key: "executable", Label: "Executable", Kind: agent.ProfileFieldExecutable, Required: true, Default: "codex", PreferencesOnly: true},
			{Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Description: "Managed by Codex sign-in", Required: true, Default: "provider-owned", PreferencesOnly: true},
			{Key: "options.probe_timeout", OptionKey: "probe_timeout", Label: "Probe timeout", Kind: agent.ProfileFieldText, Description: "Wall-clock deadline for the readiness test only; never times an active fix job.", Default: defaultProbeTimeout.String(), PreferencesOnly: true},
			{Key: "options.termination_grace", OptionKey: "termination_grace", Label: "Cancellation grace", Kind: agent.ProfileFieldText, Description: "How long a cancelled Codex turn may finish interrupting before its owned App Server is stopped; this never cancels a live job by itself.", Default: defaultTerminationGrace.String(), PreferencesOnly: true},
		}}
}

func (*Strategy) ValidateProfile(profile agent.Profile) error {
	if profile.ID == "" || profile.Runtime != RuntimeKind {
		return errors.New("Codex profile ID and runtime are required")
	}
	if profile.Executable == "" {
		return errors.New("Codex executable is required")
	}
	if profile.AuthenticationRef != "provider" && profile.AuthenticationRef != "provider-owned" {
		return errors.New("Codex supports provider-owned authentication only")
	}
	for key := range profile.Options {
		if key != "probe_timeout" && key != "termination_grace" {
			return fmt.Errorf("unsupported Codex profile option %q", key)
		}
	}
	_, err := configuredProbePolicy(profile)
	return err
}

func (strategy *Strategy) Probe(parent context.Context, profile agent.Profile) agent.ProbeResult {
	result := agent.ProbeResult{Runtime: RuntimeKind, State: agent.ProbeUnavailable}
	if strategy == nil {
		result.Diagnostic = "Codex App Server adapter is not configured"
		return result
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	executable, err := resolveExecutable(profile.Executable)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	directory, err := strategy.workingDirectory()
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(parent, policy.timeout)
	defer cancel()
	client, err := strategy.appServer(executable, directory, diagnosticCaptureLimit, nil)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	result = inspectAppServer(ctx, client)
	if closeErr := client.Close(policy.terminationGrace); closeErr != nil {
		result.State = agent.ProbeUnavailable
		result.Diagnostic = sanitizeProviderText(closeErr.Error())
	}
	return result
}

func (strategy *Strategy) Execute(ctx context.Context, profile agent.Profile, request agent.Request, sink agent.EventSink) (result agent.Result) {
	result = agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultFailed}
	if err := ctx.Err(); err != nil {
		result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
		return result
	}
	executable, err := resolveExecutable(profile.Executable)
	if err != nil {
		result.Failure = agent.FailureUnavailable
		result.Diagnostic = err.Error()
		return result
	}
	root, err := canonicalAbsolute(request.Workspace.RepositoryRoot)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = "candidate root: " + err.Error()
		return result
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	run := newAppServerRun(request, sink)
	client, err := strategy.appServer(executable, root, request.Limits.MaxOutputBytes, run.handle)
	if err != nil {
		result.Failure = agent.FailureLaunch
		result.Diagnostic = err.Error()
		return result
	}
	defer func() {
		run.stop()
		if closeErr := client.Close(policy.terminationGrace); closeErr != nil {
			if result.Status == agent.ResultCompleted {
				result.Status, result.Failure = agent.ResultFailed, agent.FailureLaunch
			}
			if result.Diagnostic == "" {
				result.Diagnostic = sanitizeProviderText(closeErr.Error())
			}
		}
	}()
	probe := inspectAppServer(ctx, client)
	if probe.State != agent.ProbeReady || !probe.Capabilities.Isolation.EligibleForMutation() {
		if err := ctx.Err(); err != nil {
			return failedExecution(result, ctx, agent.FailureCancellation, err)
		}
		result.Failure = failureForProbe(probe.State)
		result.Diagnostic = probe.Diagnostic
		return result
	}
	if !containsOption(probe.Capabilities.Models, request.Model) || !containsOption(probe.Capabilities.Efforts, request.Effort) || request.Delegation != agent.DelegationSingle {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = "requested Codex model, effort, or delegation mode is unavailable"
		return result
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := client.Request(ctx, "thread/start", map[string]any{
		"cwd": root, "model": string(request.Model), "approvalPolicy": "never",
		"sandbox": "workspace-write", "ephemeral": true, "serviceName": "slopwatch",
	}, &thread); err != nil {
		return failedExecution(result, ctx, agent.FailureProvider, err)
	}
	if thread.Thread.ID == "" {
		return failedExecution(result, ctx, agent.FailureProtocol, errors.New("Codex thread/start returned no thread id"))
	}
	run.setThread(thread.Thread.ID)
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := client.Request(ctx, "turn/start", map[string]any{
		"threadId": thread.Thread.ID,
		"input":    []map[string]any{{"type": "text", "text": request.Task.Instructions.EffectiveBody()}},
		"cwd":      root, "model": string(request.Model), "effort": string(request.Effort),
		"approvalPolicy": "never",
		"sandboxPolicy":  map[string]any{"type": "workspaceWrite", "writableRoots": []string{root}, "networkAccess": false},
	}, &turn); err != nil {
		return failedExecution(result, ctx, agent.FailureProvider, err)
	}
	if turn.Turn.ID == "" {
		return failedExecution(result, ctx, agent.FailureProtocol, errors.New("Codex turn/start returned no turn id"))
	}
	run.setTurn(turn.Turn.ID)

	reconcileEvery := strategy.reconcileEvery
	if reconcileEvery <= 0 {
		reconcileEvery = defaultReconcileEvery
	}
	reconcile := time.NewTicker(reconcileEvery)
	defer reconcile.Stop()
	reconcileCtx, stopReconciliation := context.WithCancel(ctx)
	var reconciliationWorkers sync.WaitGroup
	defer func() {
		stopReconciliation()
		reconciliationWorkers.Wait()
	}()
	reconciled := make(chan turnReconciliation, 1)
	reconciliationInFlight := false
	for {
		select {
		case completed := <-run.completed:
			return completedExecution(result, ctx, run, thread.Thread.ID, completed)
		case <-reconcile.C:
			if !run.hasFinalAnswer() || reconciliationInFlight {
				continue
			}
			reconciliationInFlight = true
			reconciliationWorkers.Add(1)
			go func() {
				defer reconciliationWorkers.Done()
				completed, terminal, reconcileErr := reconcileCodexTurn(reconcileCtx, client, thread.Thread.ID)
				reconciled <- turnReconciliation{completed: completed, terminal: terminal, err: reconcileErr}
			}()
		case reconciliation := <-reconciled:
			reconciliationInFlight = false
			if reconciliation.err != nil {
				if errors.Is(reconciliation.err, context.Canceled) && reconcileCtx.Err() != nil {
					continue
				}
				run.setError(fmt.Errorf("reconcile Codex completion: %w", reconciliation.err))
			} else if reconciliation.terminal {
				run.complete(reconciliation.completed)
			} else {
				continue
			}
			completed := <-run.completed
			return completedExecution(result, ctx, run, thread.Thread.ID, completed)
		case <-ctx.Done():
			// The grace period starts only after this individual job is canceled.
			cancelCtx, cancel := context.WithTimeout(context.Background(), policy.terminationGrace)
			_ = client.Request(cancelCtx, "turn/interrupt", map[string]any{"threadId": thread.Thread.ID, "turnId": turn.Turn.ID}, nil)
			select {
			case <-run.completed:
			case <-cancelCtx.Done():
			}
			cancel()
			result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
			result.SessionReference = thread.Thread.ID
			result.Summary, result.Usage = run.snapshot()
			return result
		case <-client.done:
			// The reader publishes owned terminal notifications before closing
			// done. Prefer that already-buffered completion when both become ready.
			select {
			case completed := <-run.completed:
				return completedExecution(result, ctx, run, thread.Thread.ID, completed)
			default:
			}
			exitErr := client.protocolExitError()
			failure := agent.FailureProvider
			if strings.Contains(exitErr.Error(), "configured") || strings.Contains(exitErr.Error(), "decode Codex App Server") {
				failure = agent.FailureProtocol
			}
			return failedExecution(result, ctx, failure, exitErr)
		}
	}
}

type turnReconciliation struct {
	completed turnCompletion
	terminal  bool
	err       error
}

func reconcileCodexTurn(ctx context.Context, client *appServerClient, threadID string) (turnCompletion, bool, error) {
	var response struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if err := client.Request(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &response); err != nil {
		return turnCompletion{}, false, err
	}
	if response.Thread.ID == "" {
		return turnCompletion{}, false, errors.New("Codex thread/read returned no thread id")
	}
	if response.Thread.ID != threadID {
		return turnCompletion{}, false, fmt.Errorf("Codex thread/read returned foreign thread %q", response.Thread.ID)
	}
	switch response.Thread.Status.Type {
	case "idle":
		return turnCompletion{Status: "completed"}, true, nil
	case "systemError":
		failed := turnCompletion{Status: "failed"}
		failed.Error.Message = "Codex thread entered system error state"
		return failed, true, nil
	default:
		return turnCompletion{}, false, nil
	}
}

func completedExecution(result agent.Result, ctx context.Context, run *appServerRun, threadID string, completed turnCompletion) agent.Result {
	result.SessionReference = threadID
	result.Summary, result.Usage = run.snapshot()
	if emitErr := run.err(); emitErr != nil {
		return failedExecution(result, ctx, agent.FailureProtocol, emitErr)
	}
	switch completed.Status {
	case "completed":
		result.Status, result.Failure = agent.ResultCompleted, agent.FailureNone
	case "interrupted":
		result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
	default:
		result.Failure = agent.FailureProvider
		result.Diagnostic = sanitizeProviderText(completed.Error.Message)
		if result.Diagnostic == "" {
			result.Diagnostic = "Codex turn failed"
		}
	}
	return result
}

func (strategy *Strategy) appServer(executable, directory string, maximum int64, handler func(rpcMessage)) (*appServerClient, error) {
	start := strategy.start
	if start == nil {
		start = startAppServer
	}
	return start(executable, directory, os.Environ(), maximum, handler)
}

func (strategy *Strategy) workingDirectory() (string, error) {
	workingDir := strategy.workingDir
	if workingDir == nil {
		workingDir = os.Getwd
	}
	directory, err := workingDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(directory)
}

func initializeAppServer(ctx context.Context, client *appServerClient) (string, error) {
	var response struct {
		UserAgent string `json:"userAgent"`
	}
	if err := client.Request(ctx, "initialize", map[string]any{"clientInfo": map[string]any{
		"name": "slopwatch", "title": "Slopwatch", "version": "0.1.0",
	}}, &response); err != nil {
		return "", err
	}
	if err := client.Notify(ctx, "initialized", map[string]any{}); err != nil {
		return "", err
	}
	match := appServerVersionPattern.FindStringSubmatch(response.UserAgent)
	if len(match) == 2 {
		return match[1], nil
	}
	return response.UserAgent, nil
}

func inspectAppServer(ctx context.Context, client *appServerClient) agent.ProbeResult {
	result := agent.ProbeResult{Runtime: RuntimeKind, State: agent.ProbeUnavailable}
	version, err := initializeAppServer(ctx, client)
	if err != nil {
		result.State = probeStateForError(err)
		result.Diagnostic = "Codex App Server initialization failed: " + sanitizeProviderText(err.Error())
		return result
	}
	result.Version = version
	authentication, signedIn, err := readAccount(ctx, client)
	if err != nil {
		result.State = probeStateForError(err)
		result.Diagnostic = "Codex account probe failed: " + sanitizeProviderText(err.Error())
		return result
	}
	if !signedIn {
		result.State = agent.ProbeUnauthenticated
		result.Diagnostic = "Not signed in — run `codex login`, then Test again"
		return result
	}
	result.Authentication = authentication
	models, efforts, err := readModels(ctx, client)
	if err != nil || len(models) == 0 || len(efforts) == 0 {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Codex model catalog is incompatible"
		if err != nil {
			result.Diagnostic += ": " + sanitizeProviderText(err.Error())
		}
		return result
	}
	result.Capabilities = appServerCapabilities(models, efforts)
	result.State = agent.ProbeReady
	result.Diagnostic = "Codex App Server is ready"
	return result
}

func readAccount(ctx context.Context, client *appServerClient) (agent.Authentication, bool, error) {
	var response struct {
		Account *struct {
			Type     string `json:"type"`
			PlanType string `json:"planType"`
		} `json:"account"`
		RequiresOpenAIAuth bool `json:"requiresOpenaiAuth"`
	}
	if err := client.Request(ctx, "account/read", map[string]any{}, &response); err != nil {
		return agent.Authentication{}, false, err
	}
	if response.Account == nil {
		return agent.Authentication{}, !response.RequiresOpenAIAuth, nil
	}
	switch response.Account.Type {
	case "chatgpt":
		label := "Signed in with ChatGPT"
		if plan := accountPlanLabel(response.Account.PlanType); plan != "" {
			label += " (" + plan + ")"
		}
		return agent.Authentication{Method: "chatgpt", Label: label}, true, nil
	case "apiKey":
		return agent.Authentication{Method: "api-key", Label: "Signed in with an API key"}, true, nil
	default:
		return agent.Authentication{Method: "provider-owned", Label: "Signed in with Codex"}, true, nil
	}
}

func accountPlanLabel(value string) string {
	switch value {
	case "free":
		return "Free"
	case "go":
		return "Go"
	case "plus":
		return "Plus"
	case "pro", "prolite":
		return "Pro"
	case "team":
		return "Team"
	case "business", "self_serve_business_usage_based", "self_serve_business_prolite":
		return "Business"
	case "enterprise", "enterprise_cbp_usage_based", "enterprise_cbp_automation", "ent26":
		return "Enterprise"
	case "edu", "edu_plus", "edu_pro":
		return "Education"
	default:
		return ""
	}
}

func readModels(ctx context.Context, client *appServerClient) ([]agent.Option[agent.ModelID], []agent.Option[agent.EffortID], error) {
	type model struct {
		Model                  string `json:"model"`
		DisplayName            string `json:"displayName"`
		IsDefault              bool   `json:"isDefault"`
		DefaultReasoningEffort string `json:"defaultReasoningEffort"`
		Supported              []struct {
			Effort      string `json:"reasoningEffort"`
			Description string `json:"description"`
		} `json:"supportedReasoningEfforts"`
	}
	var all []model
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		var response struct {
			Data       []model `json:"data"`
			NextCursor string  `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Request(ctx, "model/list", params, &response); err != nil {
			return nil, nil, err
		}
		all = append(all, response.Data...)
		if response.NextCursor == "" {
			break
		}
		if _, seen := seenCursors[response.NextCursor]; seen {
			return nil, nil, errors.New("Codex model catalog repeated a pagination cursor")
		}
		seenCursors[response.NextCursor] = struct{}{}
		cursor = response.NextCursor
	}
	models := make([]agent.Option[agent.ModelID], 0, len(all))
	effortsByID := make(map[string]agent.Option[agent.EffortID])
	for _, current := range all {
		if current.Model == "" {
			continue
		}
		models = append(models, agent.Option[agent.ModelID]{ID: agent.ModelID(current.Model), Label: current.DisplayName, Default: current.IsDefault})
		for _, value := range current.Supported {
			if value.Effort == "" {
				continue
			}
			option := effortsByID[value.Effort]
			option.ID, option.Label, option.Description = agent.EffortID(value.Effort), value.Effort, value.Description
			if value.Effort == current.DefaultReasoningEffort {
				option.Default = true
			}
			effortsByID[value.Effort] = option
		}
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Default != models[j].Default {
			return models[i].Default
		}
		return models[i].ID < models[j].ID
	})
	efforts := make([]agent.Option[agent.EffortID], 0, len(effortsByID))
	for _, effort := range effortsByID {
		efforts = append(efforts, effort)
	}
	sort.Slice(efforts, func(i, j int) bool { return efforts[i].ID < efforts[j].ID })
	return models, efforts, nil
}

func appServerCapabilities(models []agent.Option[agent.ModelID], efforts []agent.Option[agent.EffortID]) agent.Capabilities {
	return agent.Capabilities{
		Models: models, Efforts: efforts,
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle, Label: "Single agent", Default: true}},
		Resume:     false, Progress: agent.ProgressStructured,
		Network: agent.NetworkCapability{TransportRequired: true, ToolNetwork: false},
		Isolation: agent.RuntimeIsolation{
			Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true,
		},
	}
}

type turnCompletion struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
}

type appServerRun struct {
	request   agent.Request
	sink      agent.EventSink
	completed chan turnCompletion

	emitMu      sync.Mutex
	mu          sync.Mutex
	threadID    string
	turnID      string
	sequence    uint64
	events      int
	summary     string
	usage       agent.Usage
	emitErr     error
	terminal    bool
	finalAnswer bool
	actors      map[string]struct{}
	pendingTurn []rpcMessage
	turnReady   bool
}

func newAppServerRun(request agent.Request, sink agent.EventSink) *appServerRun {
	if sink == nil {
		sink = agent.EventSinkFunc(func(agent.Event) error { return nil })
	}
	return &appServerRun{request: request, sink: sink, completed: make(chan turnCompletion, 1), actors: make(map[string]struct{})}
}

func (run *appServerRun) setThread(value string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.threadID != "" && run.threadID != value {
		run.setErrorLocked(errors.New("Codex App Server changed the owned thread id"))
		return
	}
	run.threadID = value
}

func (run *appServerRun) setTurn(value string) {
	run.mu.Lock()
	if run.turnID != "" && run.turnID != value {
		run.setErrorLocked(errors.New("Codex App Server changed the owned turn id"))
		run.mu.Unlock()
		return
	}
	run.turnID = value
	run.mu.Unlock()
	for {
		run.mu.Lock()
		pending := append([]rpcMessage(nil), run.pendingTurn...)
		run.pendingTurn = nil
		if len(pending) == 0 {
			run.turnReady = true
			run.mu.Unlock()
			return
		}
		run.mu.Unlock()
		for _, message := range pending {
			if message.Method == "turn/started" {
				run.handleBound(message)
			}
		}
		for _, message := range pending {
			if message.Method != "turn/started" {
				run.handleBound(message)
			}
		}
	}
}

func (run *appServerRun) handle(message rpcMessage) {
	run.mu.Lock()
	terminal := run.terminal
	run.mu.Unlock()
	if terminal {
		return
	}
	var params map[string]any
	if err := json.Unmarshal(message.Params, &params); err != nil {
		run.setError(err)
		return
	}
	if run.bufferUntilTurnBound(message, params) {
		return
	}
	run.handleBoundParams(message, params)
}

func (run *appServerRun) handleBound(message rpcMessage) {
	run.mu.Lock()
	terminal := run.terminal
	run.mu.Unlock()
	if terminal {
		return
	}
	var params map[string]any
	if err := json.Unmarshal(message.Params, &params); err != nil {
		run.setError(err)
		return
	}
	run.handleBoundParams(message, params)
}

func (run *appServerRun) handleBoundParams(message rpcMessage, params map[string]any) {
	switch message.Method {
	case "turn/started":
		if !run.matchesOwned(params, true) {
			return
		}
		run.emit(agent.Event{Kind: agent.EventStarted, Summary: "Codex turn started"})
	case "item/started", "item/completed":
		if !run.matchesOwned(params, false) {
			return
		}
		item, _ := params["item"].(map[string]any)
		if item != nil {
			run.handleItem(message.Method, item)
		}
	case "thread/tokenUsage/updated":
		if !run.matchesOwned(params, false) {
			return
		}
		usage, _ := params["tokenUsage"].(map[string]any)
		total, _ := usage["total"].(map[string]any)
		value := parseAppServerUsage(total)
		run.emitWithUpdate(agent.Event{Kind: agent.EventUsage, Summary: "Token usage updated", Usage: &value}, func() {
			run.usage = value
		})
	case "turn/plan/updated":
		if !run.matchesOwned(params, false) {
			return
		}
		run.emit(agent.Event{Kind: agent.EventActivity, Summary: "Codex updated its plan"})
	case "warning":
		run.emit(agent.Event{Kind: agent.EventWarning, Summary: stringValue(params, "message")})
	case "configWarning":
		run.emit(agent.Event{Kind: agent.EventWarning, Summary: stringValue(params, "summary")})
	case "turn/completed":
		if !run.matchesOwned(params, true) {
			return
		}
		turn, _ := params["turn"].(map[string]any)
		encoded, _ := json.Marshal(turn)
		var completed turnCompletion
		if err := json.Unmarshal(encoded, &completed); err != nil {
			run.setError(err)
			return
		}
		run.complete(completed)
	}
}

func (run *appServerRun) bufferUntilTurnBound(message rpcMessage, params map[string]any) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.turnReady || run.threadID == "" {
		return false
	}
	if threadID := stringValue(params, "threadId"); threadID != "" && threadID != run.threadID {
		return false
	}
	run.pendingTurn = append(run.pendingTurn, message)
	return true
}

func (run *appServerRun) complete(completed turnCompletion) {
	// Terminalize synchronously with sink emission before waking Execute. The
	// reader may already have the next JSONL notification buffered.
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.terminal {
		return
	}
	run.terminal = true
	select {
	case run.completed <- completed:
	default:
	}
}

func (run *appServerRun) matchesOwned(params map[string]any, allowTurnStart bool) bool {
	threadID, turnID := stringValue(params, "threadId"), stringValue(params, "turnId")
	if allowTurnStart && turnID == "" {
		if turn, ok := params["turn"].(map[string]any); ok {
			turnID = stringValue(turn, "id")
		}
	}
	if threadID == "" || turnID == "" {
		run.setError(errors.New("Codex App Server event omitted thread or turn identity"))
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.threadID == "" || threadID != run.threadID {
		return false
	}
	return run.turnID != "" && turnID == run.turnID
}

func (run *appServerRun) handleItem(method string, item map[string]any) {
	kind, id := stringValue(item, "type"), stringValue(item, "id")
	switch kind {
	case "commandExecution":
		eventKind := agent.EventCommandStarted
		if method == "item/completed" {
			eventKind = agent.EventCommandFinished
		}
		run.emit(agent.Event{Kind: eventKind, CommandID: id, Summary: stringValue(item, "command")})
	case "agentMessage":
		text := stringValue(item, "text")
		if method == "item/completed" && text != "" {
			finalAnswer := stringValue(item, "phase") == "final_answer"
			run.emitWithUpdate(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: text}, func() {
				run.summary = text
				if finalAnswer {
					run.finalAnswer = true
				}
			})
			return
		}
		run.emit(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: text})
	case "reasoning":
		if summary := stringSliceValue(item, "summary"); summary != "" {
			run.emit(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: summary})
		}
	case "fileChange":
		changes, _ := item["changes"].([]any)
		for _, raw := range changes {
			change, _ := raw.(map[string]any)
			if path, ok := run.repoPath(stringValue(change, "path")); ok {
				run.emit(agent.Event{Kind: agent.EventFileChanged, CommandID: id, Path: path, Summary: "Changed " + path.String()})
			}
		}
	case "collabAgentToolCall":
		actor := firstStringValue(item, "receiverThreadIds")
		if actor == "" {
			actor = id
		}
		run.emit(agent.Event{Kind: agent.EventActivity, CommandID: id, ActorID: actor, Summary: "Codex agent activity"})
	}
}

func (run *appServerRun) emit(event agent.Event) {
	run.emitWithUpdate(event, nil)
}

func (run *appServerRun) emitWithUpdate(event agent.Event, update func()) {
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	if run.emitErr != nil || run.terminal {
		run.mu.Unlock()
		return
	}
	if update != nil {
		update()
	}
	run.events++
	if run.request.Limits.MaxEvents > 0 && run.events > run.request.Limits.MaxEvents {
		run.setErrorLocked(fmt.Errorf("Codex emitted more than %d events", run.request.Limits.MaxEvents))
		run.mu.Unlock()
		return
	}
	if event.ActorID != "" {
		run.actors[event.ActorID] = struct{}{}
		if run.request.Limits.MaxActors > 0 && len(run.actors) > run.request.Limits.MaxActors {
			run.setErrorLocked(fmt.Errorf("Codex reported more than %d actors", run.request.Limits.MaxActors))
			run.mu.Unlock()
			return
		}
	}
	run.sequence++
	event.JobID, event.AttemptID, event.Sequence, event.At = run.request.JobID, run.request.AttemptID, run.sequence, time.Now()
	run.mu.Unlock()
	err := run.sink.Emit(event)
	if err != nil {
		run.mu.Lock()
		run.setErrorLocked(fmt.Errorf("emit Codex event: %w", err))
		run.mu.Unlock()
	}
}

func (run *appServerRun) stop() {
	// Serialize with the sink call so that returning from stop means no provider
	// event can still be entering the controller for this attempt.
	run.emitMu.Lock()
	run.mu.Lock()
	run.terminal = true
	run.mu.Unlock()
	run.emitMu.Unlock()
}

func (run *appServerRun) repoPath(value string) (fix.RepoPath, bool) {
	if filepath.IsAbs(value) {
		relative, err := filepath.Rel(run.request.Workspace.RepositoryRoot, value)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", false
		}
		value = relative
	}
	path, err := fix.ParseRepoPath(filepath.ToSlash(strings.TrimPrefix(value, "./")))
	return path, err == nil
}

func (run *appServerRun) setError(err error) {
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	defer run.mu.Unlock()
	run.setErrorLocked(err)
}

func (run *appServerRun) setErrorLocked(err error) {
	if run.emitErr == nil && !run.terminal {
		run.emitErr = err
		run.terminal = true
		failed := turnCompletion{Status: "failed"}
		failed.Error.Message = err.Error()
		select {
		case run.completed <- failed:
		default:
		}
	}
}

func (run *appServerRun) err() error { run.mu.Lock(); defer run.mu.Unlock(); return run.emitErr }
func (run *appServerRun) hasFinalAnswer() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.finalAnswer
}
func (run *appServerRun) snapshot() (string, agent.Usage) {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.summary, run.usage
}

func parseAppServerUsage(value map[string]any) agent.Usage {
	return agent.Usage{
		InputTokens: integerValue(value, "inputTokens"), CachedTokens: integerValue(value, "cachedInputTokens"),
		OutputTokens: integerValue(value, "outputTokens"), ReasoningTokens: integerValue(value, "reasoningOutputTokens"), Cumulative: true,
	}
}

func stringValue(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}
func integerValue(value map[string]any, key string) int64 {
	if result, ok := value[key].(float64); ok {
		return int64(result)
	}
	return 0
}
func stringSliceValue(value map[string]any, key string) string {
	raw, _ := value[key].([]any)
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && text != "" {
			items = append(items, text)
		}
	}
	return strings.Join(items, " ")
}

func firstStringValue(value map[string]any, key string) string {
	raw, _ := value[key].([]any)
	if len(raw) == 0 {
		return ""
	}
	result, _ := raw[0].(string)
	return result
}

func sanitizeProviderText(value string) string {
	value = providerControlPattern.ReplaceAllString(value, "?")
	value = providerBearerPattern.ReplaceAllString(value, "$1 [REDACTED]")
	value = providerKeyPattern.ReplaceAllString(value, "[REDACTED]")
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		value = value[:4096] + "..."
	}
	return value
}

func configuredProbePolicy(profile agent.Profile) (probePolicy, error) {
	result := probePolicy{timeout: defaultProbeTimeout, terminationGrace: defaultTerminationGrace}
	if raw := strings.TrimSpace(profile.Options["probe_timeout"]); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return probePolicy{}, errors.New("Codex probe timeout must be a positive duration such as 15s")
		}
		result.timeout = value
	}
	if raw := strings.TrimSpace(profile.Options["termination_grace"]); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return probePolicy{}, errors.New("Codex cancellation grace must be a positive duration such as 5s")
		}
		result.terminationGrace = value
	}
	return result, nil
}

func containsOption[T ~string](options []agent.Option[T], wanted T) bool {
	for _, option := range options {
		if option.ID == wanted {
			return true
		}
	}
	return false
}

func probeStateForError(err error) agent.ProbeState {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return agent.ProbeUnavailable
	}
	return agent.ProbeIncompatible
}

func failureForProbe(state agent.ProbeState) agent.FailureClass {
	switch state {
	case agent.ProbeUnauthenticated:
		return agent.FailureUnauthenticated
	case agent.ProbeIncompatible:
		return agent.FailureIncompatible
	case agent.ProbeUnavailable:
		return agent.FailureUnavailable
	default:
		return agent.FailureUnsupportedCapability
	}
}

func failedExecution(result agent.Result, ctx context.Context, class agent.FailureClass, err error) agent.Result {
	if ctx.Err() != nil {
		result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
		return result
	}
	result.Failure = class
	if err != nil {
		result.Diagnostic = err.Error()
	}
	return result
}

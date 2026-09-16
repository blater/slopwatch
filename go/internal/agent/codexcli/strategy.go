package codexcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

func targetManifestDirectory(identity fix.CandidateIdentity, manifest agent.TargetManifest) (string, error) {
	staging := identity.StagingRoot
	if staging == "" || !filepath.IsAbs(staging) || filepath.Clean(staging) != staging || manifest.Path == "" || !filepath.IsAbs(manifest.Path) || filepath.Clean(manifest.Path) != manifest.Path || manifest.Count <= 0 {
		return "", errors.New("target manifest must be a file in candidate staging")
	}
	relative, err := filepath.Rel(staging, manifest.Path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("target manifest must be a file in candidate staging")
	}
	manifestInfo, err := os.Lstat(manifest.Path)
	if err != nil || !manifestInfo.Mode().IsRegular() {
		return "", errors.New("target manifest is unavailable")
	}
	return filepath.Dir(manifest.Path), nil
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
		Resume: false, Progress: agent.ProgressStructured,
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

package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

const (
	defaultProbeTimeout     = 15 * time.Second
	defaultProbeOutputBytes = int64(8 << 20)
	defaultTerminationGrace = 5 * time.Second
)

type probePolicy struct {
	timeout          time.Duration
	outputBytes      int64
	terminationGrace time.Duration
}

var versionPattern = regexp.MustCompile(`(?m)codex-cli\s+([0-9]+\.[0-9]+\.[0-9]+(?:[-+][^\s]+)?)`)

type Strategy struct {
	runner      isolation.Executor
	conformance isolation.Checker
	getenv      func(string) string
	workingDir  func() (string, error)
}

func New(runner isolation.Executor, conformance isolation.Checker) *Strategy {
	if conformance == nil {
		conformance = isolation.DenyAllChecker{}
	}
	return &Strategy{runner: runner, conformance: conformance, getenv: os.Getenv, workingDir: os.Getwd}
}

func (*Strategy) ProfileDescriptor() agent.ProfileDescriptor {
	return agent.ProfileDescriptor{Runtime: RuntimeKind, Label: "Codex CLI — managed sign-in", Fields: []agent.ProfileField{
		{Key: "executable", Label: "Executable", Kind: agent.ProfileFieldExecutable, Required: true, Default: "codex"},
		{Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Description: "Codex-managed sign-in; run `codex login` to use a ChatGPT account (recommended)", Required: true, Default: "provider-owned"},
		{Key: "options.denied_read_roots", OptionKey: "denied_read_roots", Label: "Additional denied roots", Kind: agent.ProfileFieldPathList, Description: "Path-list of sensitive roots"},
		{Key: "options.probe_timeout", OptionKey: "probe_timeout", Label: "Probe timeout", Kind: agent.ProfileFieldText, Description: "Wall-clock deadline for Codex readiness and confinement probes; never times an active fix job.", Default: defaultProbeTimeout.String()},
		{Key: "options.probe_output_bytes", OptionKey: "probe_output_bytes", Label: "Probe output bytes", Kind: agent.ProfileFieldText, Description: "Captured stdout/stderr budget for each Codex readiness or confinement probe.", Default: fmt.Sprint(defaultProbeOutputBytes), Pattern: `^[1-9][0-9]*$`},
		{Key: "options.termination_grace", OptionKey: "termination_grace", Label: "Cancellation grace", Kind: agent.ProfileFieldText, Description: "How long a cancelled Codex process may exit cleanly before it is killed; this never cancels a live job by itself.", Default: defaultTerminationGrace.String()},
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
		return errors.New("Codex CLI supports provider-owned authentication only")
	}
	for key := range profile.Options {
		if key != "denied_read_roots" && key != "probe_timeout" && key != "probe_output_bytes" && key != "termination_grace" {
			return fmt.Errorf("unsupported Codex profile option %q", key)
		}
	}
	if _, err := configuredProbePolicy(profile); err != nil {
		return err
	}
	return nil
}

func (strategy *Strategy) Probe(ctx context.Context, profile agent.Profile) agent.ProbeResult {
	result := agent.ProbeResult{Runtime: RuntimeKind, State: agent.ProbeUnavailable}
	if strategy == nil || strategy.runner == nil {
		result.Diagnostic = "Codex process runner is not configured"
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
	directory, err := strategy.workingDir()
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	versionRun, err := strategy.runProbe(ctx, policy, executable, directory, []string{"--version"})
	if err != nil || !versionRun.Successful() {
		result.Diagnostic = probeDiagnostic("Codex version probe failed", versionRun, err)
		return result
	}
	match := versionPattern.FindSubmatch(versionRun.Stdout)
	if len(match) != 2 {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Codex returned an unrecognized version"
		return result
	}
	result.Version = string(match[1])

	authRun, err := strategy.runProbe(ctx, policy, executable, directory, []string{"login", "status"})
	authText := strings.ToLower(string(authRun.Stdout) + "\n" + string(authRun.Stderr))
	authentication, signedIn := parseAuthenticationStatus(authText)
	if err != nil || !authRun.Successful() || !signedIn {
		result.State = agent.ProbeUnauthenticated
		result.Diagnostic = "Not signed in — run `codex login`, then Test again"
		return result
	}
	result.Authentication = authentication
	modelsRun, err := strategy.runProbe(ctx, policy, executable, directory, []string{"debug", "models", "--bundled"})
	if err != nil || !modelsRun.Successful() || modelsRun.StdoutTruncated {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = probeDiagnostic("Codex model catalog probe failed", modelsRun, err)
		return result
	}
	models, efforts, err := parseModelCatalog(modelsRun.Stdout)
	if err != nil || len(models) == 0 || len(efforts) == 0 {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = fmt.Sprintf("Codex model catalog is incompatible: %v", err)
		return result
	}

	conformance := strategy.probeConfinement(ctx, executable, profile, policy)
	result.Capabilities = capabilities(models, efforts, conformance)
	if !conformance.MutationEligible() {
		result.State = agent.ProbeDegraded
		result.Diagnostic = conformance.Diagnostic
		if result.Diagnostic == "" {
			result.Diagnostic = fmt.Sprintf("Codex confinement gates failed: %v", conformance.FailedGates())
		}
		return result
	}
	result.State = agent.ProbeReady
	return result
}

func parseAuthenticationStatus(value string) (agent.Authentication, bool) {
	value = strings.ToLower(value)
	if strings.Contains(value, "not logged in") || strings.Contains(value, "login required") ||
		strings.Contains(value, "authentication required") || !strings.Contains(value, "logged in") {
		return agent.Authentication{}, false
	}
	switch {
	case strings.Contains(value, "chatgpt"):
		return agent.Authentication{Method: "chatgpt", Label: "Signed in with ChatGPT"}, true
	case strings.Contains(value, "api key") || strings.Contains(value, "apikey"):
		return agent.Authentication{Method: "api-key", Label: "Signed in with an API key"}, true
	default:
		return agent.Authentication{Method: "provider-owned", Label: "Signed in with Codex"}, true
	}
}

func (strategy *Strategy) Execute(ctx context.Context, profile agent.Profile, request agent.Request, sink agent.EventSink) agent.Result {
	result := agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultFailed}
	probe := strategy.Probe(ctx, profile)
	if probe.State != agent.ProbeReady || !probe.Capabilities.Isolation.EligibleForMutation() {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = probe.Diagnostic
		return result
	}
	if !containsOption(probe.Capabilities.Models, request.Model) || !containsOption(probe.Capabilities.Efforts, request.Effort) || request.Delegation != agent.DelegationSingle {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = "requested Codex model, effort, or delegation mode is unavailable"
		return result
	}
	if request.Limits.MaxOutputBytes <= 0 {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "Codex execution requires a configured output limit"
		return result
	}
	executable, err := resolveExecutable(profile.Executable)
	if err != nil {
		result.Failure = agent.FailureUnavailable
		result.Diagnostic = err.Error()
		return result
	}
	permissionArgs, err := permissionArguments(profile, request.Workspace)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	outsideRoot, err := os.MkdirTemp("", "slopwatch-conformance-outside-")
	if err != nil {
		result.Failure = agent.FailureLaunch
		result.Diagnostic = err.Error()
		return result
	}
	defer os.RemoveAll(outsideRoot)
	sensitiveRoots, err := configuredSensitiveRoots(profile)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	exactConformance := strategy.conformance.Check(ctx, isolation.ConformanceRequest{
		Executable: executable, ProfileArguments: append([]string(nil), permissionArgs...), ProfileFingerprint: profile.Fingerprint,
		CandidateRoot: request.Workspace.RepositoryRoot, GitCommonDir: request.Workspace.GitCommonDir,
		OutsideRoot: outsideRoot, SensitiveRoots: sensitiveRoots, TransportAuthVerified: true,
		Limits: isolation.Limits{WallTime: policy.timeout, TerminateGrace: policy.terminationGrace, MaxStdoutBytes: policy.outputBytes, MaxStderrBytes: policy.outputBytes},
	})
	if !exactConformance.MutationEligible() {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = exactConformance.Diagnostic
		if result.Diagnostic == "" {
			result.Diagnostic = fmt.Sprintf("Codex candidate confinement gates failed: %v", exactConformance.FailedGates())
		}
		return result
	}
	arguments := append([]string{"-a", "never"}, permissionArgs...)
	arguments = append(arguments,
		"-c", "model_reasoning_effort="+quoted(string(request.Effort)),
		"--disable", "hooks", "--disable", "plugins", "--disable", "apps",
		"--disable", "multi_agent", "-C", request.Workspace.RepositoryRoot,
		"-m", string(request.Model), "exec", "--ephemeral", "--ignore-user-config",
		"--ignore-rules", "--strict-config", "--json", "--color", "never", "-",
	)
	processRequest := isolation.Request{
		Executable: executable, Arguments: arguments, Directory: request.Workspace.RepositoryRoot,
		Environment: transportEnvironment(strategy.getenv), Stdin: []byte(request.Task.Instructions.EffectiveBody()),
		Limits: isolation.Limits{
			TerminateGrace: policy.terminationGrace,
			MaxStdoutBytes: request.Limits.MaxOutputBytes,
			MaxStderrBytes: request.Limits.MaxOutputBytes,
		},
	}
	var run isolation.Result
	var runErr error
	var parsed normalized
	var parseErr error
	streamed := false
	if streaming, ok := strategy.runner.(isolation.StreamingExecutor); ok {
		streamed = true
		reader, writer := io.Pipe()
		type parseResult struct {
			value normalized
			err   error
		}
		parsedResult := make(chan parseResult, 1)
		go func() {
			value, err := normalizeEventReader(reader, request, sink)
			_ = reader.Close()
			parsedResult <- parseResult{value: value, err: err}
		}()
		run, runErr = streaming.RunStreaming(ctx, processRequest, func(chunk []byte) {
			_, _ = writer.Write(chunk)
		})
		_ = writer.Close()
		finished := <-parsedResult
		parsed, parseErr = finished.value, finished.err
	} else {
		run, runErr = strategy.runner.Run(ctx, processRequest)
	}
	if runErr != nil {
		result.Failure = agent.FailureLaunch
		result.Diagnostic = runErr.Error()
		return result
	}
	result.ExitCode = run.ExitCode
	if run.Canceled {
		result.Status = agent.ResultCanceled
		result.Failure = agent.FailureCancellation
		return result
	}
	if run.TimedOut {
		result.Status = agent.ResultTimedOut
		result.Failure = agent.FailureTimeout
		return result
	}
	if run.StdoutTruncated || run.StderrTruncated {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "Codex output exceeded the configured byte limit"
		return result
	}
	if !streamed {
		parsed, parseErr = normalizeEvents(run.Stdout, request, sink)
	}
	if parseErr != nil {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = parseErr.Error()
		return result
	}
	result.Summary = parsed.summary
	result.Usage = parsed.usage
	result.SessionReference = parsed.sessionReference
	if run.ExitCode != 0 {
		result.Failure = agent.FailureProvider
		result.Diagnostic = boundedDiagnostic(run.Stderr)
		return result
	}
	result.Status = agent.ResultCompleted
	result.Failure = agent.FailureNone
	return result
}

func (strategy *Strategy) probeConfinement(ctx context.Context, executable string, profile agent.Profile, policy probePolicy) isolation.Conformance {
	root, err := os.MkdirTemp("", "slopwatch-codex-conformance-")
	if err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	candidate := filepath.Join(root, "candidate")
	common := filepath.Join(root, "common.git")
	outside := filepath.Join(root, "outside")
	sensitive := filepath.Join(root, "sensitive")
	for _, directory := range []string{candidate, common, outside, sensitive} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return isolation.Conformance{Diagnostic: err.Error()}
		}
	}
	if err := os.WriteFile(filepath.Join(candidate, ".git"), []byte("gitdir: "+common+"\n"), 0o600); err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	if err := os.WriteFile(filepath.Join(common, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	if err := os.WriteFile(filepath.Join(sensitive, "sentinel"), []byte("private"), 0o600); err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	probeProfile := profile
	probeProfile.Options = cloneOptions(profile.Options)
	existing := probeProfile.Options["denied_read_roots"]
	if existing == "" {
		probeProfile.Options["denied_read_roots"] = sensitive
	} else {
		probeProfile.Options["denied_read_roots"] = existing + string(os.PathListSeparator) + sensitive
	}
	arguments, err := permissionArguments(probeProfile, fix.CandidateIdentity{RepositoryRoot: candidate, GitCommonDir: common})
	if err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	sensitiveRoots, err := configuredSensitiveRoots(probeProfile)
	if err != nil {
		return isolation.Conformance{Diagnostic: err.Error()}
	}
	return strategy.conformance.Check(ctx, isolation.ConformanceRequest{
		Executable: executable, ProfileArguments: arguments, ProfileFingerprint: profile.Fingerprint,
		CandidateRoot: candidate, GitCommonDir: common, OutsideRoot: outside,
		SensitiveRoots: sensitiveRoots, TransportAuthVerified: true,
		Limits: isolation.Limits{WallTime: policy.timeout, TerminateGrace: policy.terminationGrace, MaxStdoutBytes: policy.outputBytes, MaxStderrBytes: policy.outputBytes},
	})
}

func configuredSensitiveRoots(profile agent.Profile) ([]string, error) {
	raw := profile.Options["denied_read_roots"]
	if raw == "" {
		return nil, nil
	}
	result := make([]string, 0)
	for _, value := range filepath.SplitList(raw) {
		absolute, err := canonicalAbsolute(value)
		if err != nil {
			return nil, err
		}
		result = append(result, absolute)
	}
	return result, nil
}

func cloneOptions(source map[string]string) map[string]string {
	result := make(map[string]string, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (strategy *Strategy) runProbe(ctx context.Context, policy probePolicy, executable, directory string, arguments []string) (isolation.Result, error) {
	return strategy.runner.Run(ctx, isolation.Request{
		Executable: executable, Arguments: arguments, Directory: directory,
		Environment: transportEnvironment(strategy.getenv),
		Limits:      isolation.Limits{WallTime: policy.timeout, TerminateGrace: policy.terminationGrace, MaxStdoutBytes: policy.outputBytes, MaxStderrBytes: policy.outputBytes},
	})
}

func configuredProbePolicy(profile agent.Profile) (probePolicy, error) {
	result := probePolicy{timeout: defaultProbeTimeout, outputBytes: defaultProbeOutputBytes, terminationGrace: defaultTerminationGrace}
	if raw := strings.TrimSpace(profile.Options["probe_timeout"]); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return probePolicy{}, errors.New("Codex probe timeout must be a positive duration such as 15s")
		}
		result.timeout = value
	}
	if raw := strings.TrimSpace(profile.Options["probe_output_bytes"]); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return probePolicy{}, errors.New("Codex probe output bytes must be a positive integer")
		}
		result.outputBytes = value
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

func capabilities(models []agent.Option[agent.ModelID], efforts []agent.Option[agent.EffortID], conformance isolation.Conformance) agent.Capabilities {
	writes := agent.AdvisoryOnly
	if conformance.CandidateWrite && conformance.OutsideWriteDenied {
		writes = agent.CandidateTreeEnforced
		if conformance.GitMetadataDenied {
			writes = agent.CandidateTreeAndGitMetadataProtected
		}
	}
	return agent.Capabilities{
		Models: models, Efforts: efforts,
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle, Label: "Single agent", Default: true}},
		Resume:     false, Progress: agent.ProgressStructured,
		Network: agent.NetworkCapability{TransportRequired: true, ToolNetwork: !conformance.ToolNetworkPolicy},
		Isolation: agent.RuntimeIsolation{
			Writes: writes, SensitiveReadsDenied: conformance.SensitiveReadsDenied,
			TransportAuthIsolated: conformance.TransportAuth, CrashContainment: conformance.CrashContainment,
		},
	}
}

type catalog struct {
	Models []struct {
		Slug                  string `json:"slug"`
		DisplayName           string `json:"display_name"`
		DefaultReasoningLevel string `json:"default_reasoning_level"`
		SupportedReasoning    []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

func parseModelCatalog(data []byte) ([]agent.Option[agent.ModelID], []agent.Option[agent.EffortID], error) {
	var value catalog
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, nil, err
	}
	models := make([]agent.Option[agent.ModelID], 0, len(value.Models))
	effortByID := map[string]agent.Option[agent.EffortID]{}
	for _, model := range value.Models {
		if model.Slug == "" {
			continue
		}
		models = append(models, agent.Option[agent.ModelID]{ID: agent.ModelID(model.Slug), Label: model.DisplayName, Default: len(models) == 0})
		for _, effort := range model.SupportedReasoning {
			if effort.Effort == "" || effort.Effort == "ultra" {
				continue
			}
			current := effortByID[effort.Effort]
			current.ID = agent.EffortID(effort.Effort)
			current.Label = effort.Effort
			current.Description = effort.Description
			if effort.Effort == model.DefaultReasoningLevel {
				current.Default = true
			}
			effortByID[effort.Effort] = current
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	efforts := make([]agent.Option[agent.EffortID], 0, len(effortByID))
	for _, effort := range effortByID {
		efforts = append(efforts, effort)
	}
	sort.Slice(efforts, func(i, j int) bool { return efforts[i].ID < efforts[j].ID })
	return models, efforts, nil
}

func containsOption[T ~string](options []agent.Option[T], wanted T) bool {
	for _, option := range options {
		if option.ID == wanted {
			return true
		}
	}
	return false
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func probeDiagnostic(prefix string, result isolation.Result, err error) string {
	if err != nil {
		return prefix + ": " + err.Error()
	}
	if text := boundedDiagnostic(result.Stderr); text != "" {
		return prefix + ": " + text
	}
	return prefix
}

func boundedDiagnostic(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 4096 {
		text = text[:4096] + "..."
	}
	return text
}

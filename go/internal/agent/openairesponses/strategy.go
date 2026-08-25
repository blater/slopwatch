package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

type Strategy struct {
	config   resolvedConfig
	resolver SecretResolver
}

const primaryActorID = "primary"

func New(config Config, resolver SecretResolver) (*Strategy, error) {
	if resolver == nil {
		return nil, errors.New("Responses API secret resolver is required")
	}
	resolved, err := resolveConfig(config)
	if err != nil {
		return nil, err
	}
	return &Strategy{config: resolved, resolver: resolver}, nil
}

func (strategy *Strategy) ProfileDescriptor() agent.ProfileDescriptor {
	fields := []agent.ProfileField{
		{
			Key: "authentication_ref", Label: "API authentication", Kind: agent.ProfileFieldAuthReference,
			Description: "Secret reference resolved only at execution time, for example env:OPENAI_API_KEY",
			Required:    true, Default: "env:OPENAI_API_KEY", Pattern: `^[A-Za-z][A-Za-z0-9+.-]*:[^[:space:]]+$`,
		},
	}
	fields = append(fields, strategy.config.profileFields()...)
	return agent.ProfileDescriptor{Runtime: RuntimeKind, Label: "OpenAI Responses API", Fields: fields}
}

func (strategy *Strategy) ValidateProfile(profile agent.Profile) error {
	if profile.ID == "" || profile.Runtime != RuntimeKind {
		return errors.New("Responses API profile ID and runtime are required")
	}
	if profile.Executable != "" || profile.RuntimeProfile != "" {
		return errors.New("Responses API profiles cannot configure a process runtime")
	}
	if !validSecretReference(profile.AuthenticationRef) {
		return errors.New("Responses API authentication must be a secret reference, not secret material")
	}
	_, err := strategy.config.withProfile(profile)
	return err
}

func validSecretReference(value string) bool {
	if len(value) < 3 || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n\t ") {
		return false
	}
	scheme, reference, ok := strings.Cut(value, ":")
	if !ok || scheme == "" || reference == "" {
		return false
	}
	for index, character := range scheme {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (index > 0 && character >= '0' && character <= '9') || (index > 0 && strings.ContainsRune("+.-", character)) {
			continue
		}
		return false
	}
	return true
}

func (strategy *Strategy) capabilities(endpoint string) agent.Capabilities {
	domain := ""
	if parsed, err := url.Parse(endpoint); err == nil {
		domain = parsed.Hostname()
	}
	domains := []string(nil)
	if domain != "" {
		domains = []string{domain}
	}
	return agent.Capabilities{
		Models: cloneOptions(strategy.config.models), Efforts: cloneOptions(strategy.config.efforts),
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle, Label: "Single agent", Default: true}},
		Resume:     false, Progress: agent.ProgressStructured,
		Network: agent.NetworkCapability{TransportRequired: true, ToolNetwork: false, ToolDomains: domains},
		Isolation: agent.RuntimeIsolation{
			Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true,
			TransportAuthIsolated: true, CrashContainment: true,
		},
	}
}

func (strategy *Strategy) Probe(ctx context.Context, profile agent.Profile) agent.ProbeResult {
	result := agent.ProbeResult{Runtime: RuntimeKind, State: agent.ProbeUnavailable}
	if strategy == nil || strategy.resolver == nil || strategy.config.client == nil {
		result.Diagnostic = "Responses API adapter is not configured"
		return result
	}
	if err := strategy.ValidateProfile(profile); err != nil {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = err.Error()
		return result
	}
	config, err := strategy.config.withProfile(profile)
	if err != nil {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = err.Error()
		return result
	}
	endpoint := strategy.config.endpoint
	result.Capabilities = strategy.capabilities(endpoint)
	secret, err := strategy.resolveSecret(ctx, profile.AuthenticationRef)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Diagnostic = "Responses API probe was canceled"
		} else {
			result.State = agent.ProbeUnauthenticated
			result.Diagnostic = authenticationRemediation(profile.AuthenticationRef)
		}
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsEndpoint(endpoint), nil)
	if err != nil {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API endpoint is invalid"
		return result
	}
	setHeaders(request, secret, false)
	response, err := strategy.config.client.Do(request)
	if err != nil {
		result.Diagnostic = "Responses API could not be reached"
		return result
	}
	defer response.Body.Close()
	body, tooLarge, readErr := readBounded(response.Body, config.maxProbeBytes)
	if readErr != nil || tooLarge {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API probe returned an invalid response"
		return result
	}
	if providerEchoesSecret(body, secret) {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API returned unsafe authentication material"
		return result
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		result.State = agent.ProbeUnauthenticated
		result.Diagnostic = "Responses API rejected authentication"
		return result
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Diagnostic = fmt.Sprintf("Responses API probe returned HTTP %d", response.StatusCode)
		return result
	}
	available, err := availableConfiguredModels(body, strategy.config.models)
	if err != nil {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API model catalog was invalid"
		return result
	}
	if len(available) == 0 {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "None of this profile's configured models are available to the account"
		result.Capabilities.Models = nil
		return result
	}
	result.Capabilities.Models = available
	result.State = agent.ProbeReady
	result.Version = "responses-v1"
	return result
}

func availableConfiguredModels(payload []byte, configured []agent.Option[agent.ModelID]) ([]agent.Option[agent.ModelID], error) {
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &catalog); err != nil || catalog.Data == nil {
		return nil, errors.New("invalid model catalog")
	}
	available := make(map[string]struct{}, len(catalog.Data))
	for _, model := range catalog.Data {
		if model.ID != "" {
			available[model.ID] = struct{}{}
		}
	}
	result := make([]agent.Option[agent.ModelID], 0, len(configured))
	for _, model := range configured {
		if _, ok := available[string(model.ID)]; ok {
			result = append(result, model)
		}
	}
	return result, nil
}

func authenticationRemediation(reference string) string {
	if name, ok := strings.CutPrefix(reference, "env:"); ok && environmentName.MatchString(name) {
		return "Set environment variable " + name + " before launching Slopwatch"
	}
	return "Responses API authentication reference is not available from this installation's secret resolver"
}

func (strategy *Strategy) Execute(parent context.Context, profile agent.Profile, request agent.Request, sink agent.EventSink) agent.Result {
	result := agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultFailed}
	if strategy == nil || strategy.resolver == nil || strategy.config.client == nil {
		result.Failure = agent.FailureUnavailable
		result.Diagnostic = "Responses API adapter is not configured"
		return result
	}
	if err := strategy.ValidateProfile(profile); err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	config, err := strategy.config.withProfile(profile)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	endpoint := strategy.config.endpoint
	capabilities := strategy.capabilities(endpoint)
	if !hasOption(capabilities.Models, request.Model) || !hasOption(capabilities.Efforts, request.Effort) || request.Delegation != agent.DelegationSingle || request.Resume.Reference != "" {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = "requested Responses API model, effort, delegation, or resume mode is unavailable"
		return result
	}
	ctx := parent
	secret, err := strategy.resolveSecret(ctx, profile.AuthenticationRef)
	if err != nil {
		return canceledOr(result, ctx, agent.FailureUnauthenticated, "Responses API authentication reference could not be resolved")
	}
	tools, err := newCandidateTools(request.Workspace, request.Write, config)
	if err != nil {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = err.Error()
		return result
	}
	defer tools.Close()
	emitter := newEventEmitter(request, sink)
	if err := emitter.emit(agent.EventStarted, "OpenAI Responses agent started", "", primaryActorID, nil, nil); err != nil {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "agent progress sink rejected the start event"
		return result
	}
	prompt := request.Task.Instructions.EffectiveBody()
	developer, err := inputMessage("developer", prompt)
	if err != nil {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "agent instructions could not be encoded"
		return result
	}
	user, _ := inputMessage("user", "Apply the requested remediation using only the supplied candidate tools. Finish with a concise summary.")
	history := []json.RawMessage{developer, user}
	if contextSize(history) > config.maxContextBytes {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "agent instructions exceed the configured context limit"
		return result
	}
	maximumResponseBytes := config.maxResponseBytes
	remainingResponseBytes := maximumResponseBytes
	toolCalls := 0
	var usage agent.Usage
	for turn := 1; ; turn++ {
		if config.maxTurns > 0 && turn > config.maxTurns {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "Responses API reached the configured model-turn budget"
			result.Usage = usage
			return result
		}
		if err := ctx.Err(); err != nil {
			return canceledOr(result, ctx, agent.FailureCancellation, "Responses API execution was canceled")
		}
		if err := emitter.emit(agent.EventActivity, fmt.Sprintf("Model turn %d", turn), "", primaryActorID, nil, nil); err != nil {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "agent progress sink rejected an activity event"
			return result
		}
		requestBody := apiRequest{
			Model: string(request.Model), Input: history, Tools: functionTools(), ToolChoice: "auto", ParallelToolCalls: false,
			Reasoning: reasoningRequest{Effort: string(request.Effort)}, MaxOutputTokens: config.maxOutputTokens,
			Store: false, Truncation: "disabled",
		}
		payload, err := json.Marshal(requestBody)
		if err != nil || int64(len(payload)) > config.maxRequestBytes {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "Responses API request exceeded the configured context limit"
			return result
		}
		// The authentication token belongs only in the Authorization header.
		// Enforce this at the adapter's final outbound boundary as well as at UI
		// admission, covering generated instructions and candidate tool output.
		if providerEchoesSecret(payload, secret) {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "Responses API context contains protected authentication material"
			return result
		}
		body, status, err := strategy.post(ctx, config, endpoint, secret, payload, remainingResponseBytes)
		if err != nil {
			if errors.Is(err, errResponseTooLarge) {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "Responses API exceeded the configured output limit"
				return result
			}
			if errors.Is(err, errSecretEcho) {
				if status == http.StatusUnauthorized || status == http.StatusForbidden {
					result.Failure = agent.FailureUnauthenticated
					result.Diagnostic = "Responses API rejected authentication"
				} else {
					result.Failure = agent.FailureProtocol
					result.Diagnostic = "Responses API returned unsafe authentication material"
				}
				return result
			}
			return canceledOr(result, ctx, agent.FailureProvider, "Responses API request failed")
		}
		remainingResponseBytes -= int64(len(body))
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			result.Failure = agent.FailureUnauthenticated
			result.Diagnostic = "Responses API rejected authentication"
			return result
		}
		if status < 200 || status >= 300 {
			result.Failure = agent.FailureProvider
			result.Diagnostic = fmt.Sprintf("Responses API returned HTTP %d", status)
			return result
		}
		response, output, err := decodeAPIResponse(body)
		if err != nil {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = err.Error()
			return result
		}
		if response.Status != "completed" {
			result.Failure = agent.FailureProvider
			result.Diagnostic = "Responses API did not complete the model turn"
			return result
		}
		if response.Usage != nil {
			usage.InputTokens += response.Usage.InputTokens
			usage.OutputTokens += response.Usage.OutputTokens
			if response.Usage.InputTokenDetails != nil {
				usage.CachedTokens += response.Usage.InputTokenDetails.CachedTokens
			}
			if response.Usage.OutputTokenDetails != nil {
				usage.ReasoningTokens += response.Usage.OutputTokenDetails.ReasoningTokens
			}
			usage.Cumulative = true
			if config.maxContextTokens > 0 && response.Usage.InputTokens > config.maxContextTokens {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "Responses API context exceeded the configured token limit"
				return result
			}
			copy := usage
			if err := emitter.emit(agent.EventUsage, "Token usage updated", "", primaryActorID, nil, &copy); err != nil {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "agent progress sink rejected a usage event"
				return result
			}
		}
		if output.refused {
			result.Failure = agent.FailureProvider
			result.Diagnostic = "Responses API refused the remediation request"
			result.Usage = usage
			return result
		}
		if len(output.calls) == 0 {
			if output.text == "" {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "Responses API completed without a summary or tool call"
				return result
			}
			summary := boundedText(output.text, config.maxSummaryBytes)
			if err := emitter.emit(agent.EventRuntimeMessage, summary, "", primaryActorID, nil, nil); err != nil {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "agent progress sink rejected the final message"
				return result
			}
			result.Status = agent.ResultCompleted
			result.Failure = agent.FailureNone
			result.Summary = summary
			result.Usage = usage
			return result
		}
		toolCalls += len(output.calls)
		if config.maxToolCalls > 0 && toolCalls > config.maxToolCalls {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "Responses API exceeded the configured tool-call limit"
			return result
		}
		// Validated output items are retained as model context. No provider-side
		// stored conversation or unbounded previous_response_id chain is used.
		history = append(history, response.Output...)
		for _, call := range output.calls {
			if err := emitter.emit(agent.EventCommandStarted, "Running "+call.Name, call.CallID, primaryActorID, nil, nil); err != nil {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "agent progress sink rejected a tool event"
				return result
			}
			toolOutput, changed, toolErr := tools.execute(ctx, call)
			if toolErr != nil {
				if errors.Is(toolErr, context.Canceled) || errors.Is(toolErr, context.DeadlineExceeded) {
					return canceledOr(result, ctx, agent.FailureCancellation, "agent tool execution was canceled")
				}
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "Responses API emitted an invalid tool call"
				return result
			}
			item, marshalErr := functionOutput(call.CallID, toolOutput)
			if marshalErr != nil {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "agent tool result could not be encoded"
				return result
			}
			history = append(history, item)
			if changed != nil {
				if err := emitter.emit(agent.EventFileChanged, "Candidate file changed", call.CallID, primaryActorID, changed, nil); err != nil {
					result.Failure = agent.FailureProtocol
					result.Diagnostic = "agent progress sink rejected a file event"
					return result
				}
			}
			if err := emitter.emit(agent.EventCommandFinished, "Finished "+call.Name, call.CallID, primaryActorID, nil, nil); err != nil {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "agent progress sink rejected a tool event"
				return result
			}
		}
		if contextSize(history) > config.maxContextBytes {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "agent tool loop exceeded the configured context limit"
			return result
		}
	}
}

func (strategy *Strategy) resolveSecret(ctx context.Context, reference string) (string, error) {
	secret, err := strategy.resolver.ResolveSecret(ctx, reference)
	if err != nil || secret == "" || strings.ContainsAny(secret, "\x00\r\n") {
		return "", errors.New("secret resolution failed")
	}
	return secret, nil
}

var (
	errResponseTooLarge = errors.New("response exceeds configured limit")
	errSecretEcho       = errors.New("provider response contained authentication material")
)

func (strategy *Strategy) post(ctx context.Context, config resolvedConfig, endpoint, secret string, payload []byte, maximum int64) ([]byte, int, error) {
	if maximum <= 0 {
		return nil, 0, errResponseTooLarge
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, errors.New("request construction failed")
	}
	setHeaders(request, secret, true)
	response, err := config.client.Do(request)
	if err != nil {
		return nil, 0, errors.New("transport failed")
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "application/json" {
			return nil, response.StatusCode, errors.New("provider returned a non-JSON response")
		}
	}
	body, tooLarge, err := readBounded(response.Body, maximum)
	if err != nil {
		return nil, response.StatusCode, errors.New("provider response could not be read")
	}
	if tooLarge {
		return nil, response.StatusCode, errResponseTooLarge
	}
	if providerEchoesSecret(body, secret) {
		return nil, response.StatusCode, errSecretEcho
	}
	return body, response.StatusCode, nil
}

func providerEchoesSecret(body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	if bytes.Contains(body, []byte(secret)) {
		return true
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(value any) bool {
		switch typed := value.(type) {
		case string:
			return strings.Contains(typed, secret)
		case []any:
			for _, item := range typed {
				if contains(item) {
					return true
				}
			}
		case map[string]any:
			for key, item := range typed {
				if strings.Contains(key, secret) || contains(item) {
					return true
				}
			}
		}
		return false
	}
	return contains(decoded)
}

func setHeaders(request *http.Request, secret string, jsonBody bool) {
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Accept", "application/json")
	if jsonBody {
		request.Header.Set("Content-Type", "application/json")
	}
}

func readBounded(reader io.Reader, maximum int64) ([]byte, bool, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) > maximum {
		return nil, true, nil
	}
	return payload, false, nil
}

func hasOption[T ~string](options []agent.Option[T], wanted T) bool {
	for _, option := range options {
		if option.ID == wanted {
			return true
		}
	}
	return false
}

func canceledOr(result agent.Result, ctx context.Context, fallback agent.FailureClass, diagnostic string) agent.Result {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Status = agent.ResultTimedOut
		result.Failure = agent.FailureTimeout
		result.Diagnostic = "Responses API execution timed out"
		return result
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		result.Status = agent.ResultCanceled
		result.Failure = agent.FailureCancellation
		result.Diagnostic = "Responses API execution was canceled"
		return result
	}
	result.Failure = fallback
	result.Diagnostic = diagnostic
	return result
}

func boundedText(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

type eventEmitter struct {
	request  agent.Request
	sink     agent.EventSink
	sequence uint64
	maximum  int
}

func newEventEmitter(request agent.Request, sink agent.EventSink) *eventEmitter {
	return &eventEmitter{request: request, sink: sink, maximum: request.Limits.MaxEvents}
}

func (emitter *eventEmitter) emit(kind agent.EventKind, summary, commandID, actorID string, path *fix.RepoPath, usage *agent.Usage) error {
	if emitter.maximum > 0 && emitter.sequence >= uint64(emitter.maximum) {
		return errors.New("event limit exceeded")
	}
	emitter.sequence++
	if emitter.sink == nil {
		return nil
	}
	event := agent.Event{
		JobID: emitter.request.JobID, AttemptID: emitter.request.AttemptID, Sequence: emitter.sequence,
		At: time.Now().UTC(), Kind: kind, Summary: summary, CommandID: commandID, ActorID: actorID, Usage: usage,
	}
	if path != nil {
		event.Path = *path
	}
	return emitter.sink.Emit(event)
}

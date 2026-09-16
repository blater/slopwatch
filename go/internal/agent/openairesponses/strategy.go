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
	"strconv"
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
			Description: "Environment variable containing an OpenAI API key; API usage is billed separately from ChatGPT",
			Required:    true, Default: "env:OPENAI_API_KEY", Pattern: `^env:[A-Za-z_][A-Za-z0-9_]*$`,
		},
	}
	fields = append(fields, strategy.config.profileFields()...)
	return agent.ProfileDescriptor{
		Runtime: RuntimeKind, Label: "OpenAI API",
		ConnectionInstructions: "Set the named environment variable before starting Slopwatch. The API key is read at runtime and is never stored in preferences.",
		DocumentationURL:       "https://platform.openai.com/api-keys",
		Fields:                 fields,
	}
}

func (strategy *Strategy) ValidateProfile(profile agent.Profile) error {
	if profile.ID == "" || profile.Runtime != RuntimeKind {
		return errors.New("Responses API profile ID and runtime are required")
	}
	if profile.Executable != "" || profile.RuntimeProfile != "" {
		return errors.New("Responses API profiles cannot configure a process runtime")
	}
	if name, ok := strings.CutPrefix(profile.AuthenticationRef, "env:"); !ok || !environmentName.MatchString(name) {
		return errors.New("Responses API authentication must be an env:VARIABLE reference supported by this installation")
	}
	_, err := strategy.config.withProfile(profile)
	return err
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
		Resume: false, Progress: agent.ProgressStructured,
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
	response, err := strategy.fetchProbe(ctx, config, endpoint, secret)
	if err != nil {
		if !errors.Is(err, errProbeUnavailable) {
			result.State = agent.ProbeIncompatible
		}
		result.Diagnostic = err.Error()
		return result
	}
	return finishProbe(result, response, secret, strategy.config.models)
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
	capabilities := strategy.capabilities(strategy.config.endpoint)
	model, modelOK := agent.ResolveOption(capabilities.Models, request.Model)
	effort, effortOK := agent.ResolveOption(capabilities.Efforts, request.Effort)
	if !modelOK || !effortOK || request.Resume.Reference != "" {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = "requested Responses API model, effort, or resume mode is unavailable"
		return result
	}
	request.Model, request.Effort = model, effort
	secret, err := strategy.resolveSecret(parent, profile.AuthenticationRef)
	if err != nil {
		return canceledOr(result, parent, agent.FailureUnauthenticated, "Responses API authentication reference could not be resolved")
	}
	tools, err := newCandidateTools(request.Workspace, request.Write, config, request.Task.Manifest)
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
	user, err := inputMessage("user", request.Task.EffectivePrompt())
	if err != nil {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "agent instructions could not be encoded"
		return result
	}
	history := []json.RawMessage{user}
	if contextSize(history) > config.maxContextBytes {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "agent instructions exceed the configured context limit"
		return result
	}
	loop := responseTurnLoop{strategy: strategy, ctx: parent, config: config, endpoint: strategy.config.endpoint, secret: secret, request: request, emitter: emitter, tools: tools, history: history, remainingResponseBytes: config.maxResponseBytes, result: result}
	return loop.run()
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

func (strategy *Strategy) post(ctx context.Context, config resolvedConfig, endpoint, secret string, payload []byte, maximum int64) ([]byte, int, string, error) {
	if maximum <= 0 {
		return nil, 0, "", errResponseTooLarge
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, "", errors.New("request construction failed")
	}
	setHeaders(request, secret, true)
	response, err := config.client.Do(request)
	if err != nil {
		return nil, 0, "", errors.New("transport failed")
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "application/json" {
			return nil, response.StatusCode, "", errors.New("provider returned a non-JSON response")
		}
	}
	body, tooLarge, err := readBounded(response.Body, maximum)
	if err != nil {
		return nil, response.StatusCode, "", errors.New("provider response could not be read")
	}
	if tooLarge {
		return nil, response.StatusCode, "", errResponseTooLarge
	}
	if providerEchoesSecret(body, secret) {
		return nil, response.StatusCode, "", errSecretEcho
	}
	return body, response.StatusCode, response.Header.Get("Retry-After"), nil
}

func rateLimitDiagnostic(retryAfter string) string {
	retryAfter = strings.TrimSpace(retryAfter)
	if seconds, err := strconv.ParseUint(retryAfter, 10, 31); err == nil {
		return fmt.Sprintf("Responses API rate limited this profile · retry after %s", (time.Duration(seconds) * time.Second).String())
	}
	if at, err := http.ParseTime(retryAfter); err == nil {
		return "Responses API rate limited this profile · retry after " + at.UTC().Format(time.RFC3339)
	}
	return "Responses API rate limited this profile"
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

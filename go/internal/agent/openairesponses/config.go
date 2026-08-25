// Package openairesponses implements a provider-neutral coding-agent loop on
// top of the OpenAI Responses API. The model never receives ambient process or
// filesystem access: all candidate access is mediated by the small Go tool set
// in this package.
package openairesponses

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
)

const RuntimeKind agent.RuntimeKind = "openai-responses"

const defaultEndpoint = "https://api.openai.com/v1/responses"

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SecretResolver resolves a trusted reference at execution time. References,
// rather than secret values, are the only authentication material profiles may
// persist. Resolver errors are deliberately not forwarded to user-visible
// diagnostics because an implementation may include secret material in them.
type SecretResolver interface {
	ResolveSecret(context.Context, string) (string, error)
}

type SecretResolverFunc func(context.Context, string) (string, error)

func (function SecretResolverFunc) ResolveSecret(ctx context.Context, reference string) (string, error) {
	return function(ctx, reference)
}

type SecretAdmission struct{ Resolver SecretResolver }

func (guard SecretAdmission) RejectKnownSecret(ctx context.Context, reference string, values ...string) error {
	if guard.Resolver == nil || !strings.HasPrefix(reference, "env:") {
		return nil
	}
	secret, err := guard.Resolver.ResolveSecret(ctx, reference)
	if err != nil {
		return nil
	}
	for _, value := range values {
		if strings.Contains(value, secret) {
			return errors.New("protected authentication material detected")
		}
	}
	return nil
}

// EnvironmentSecretResolver accepts references of the form env:NAME. Lookup
// is injected so callers can choose the environment boundary; New does not
// implicitly read process environment variables.
type EnvironmentSecretResolver struct {
	Lookup func(string) (string, bool)
}

func NewEnvironmentSecretResolver(lookup func(string) (string, bool)) EnvironmentSecretResolver {
	return EnvironmentSecretResolver{Lookup: lookup}
}

func DefaultEnvironmentSecretResolver() EnvironmentSecretResolver {
	return NewEnvironmentSecretResolver(os.LookupEnv)
}

func (resolver EnvironmentSecretResolver) ResolveSecret(ctx context.Context, reference string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name, ok := strings.CutPrefix(reference, "env:")
	if !ok || !environmentName.MatchString(name) || resolver.Lookup == nil {
		return "", errors.New("invalid environment secret reference")
	}
	value, present := resolver.Lookup(name)
	if !present || value == "" {
		return "", errors.New("environment secret is unavailable")
	}
	return value, nil
}

// Config supplies installation defaults for provider-specific settings.
// Each value is surfaced by ProfileDescriptor and may be overridden per
// profile; there are no separate, hidden product ceilings.
type Config struct {
	Endpoint           string
	Client             *http.Client
	Models             []agent.Option[agent.ModelID]
	Efforts            []agent.Option[agent.EffortID]
	MaxTurns           int
	MaxToolCalls       int
	MaxResponseBytes   int64
	MaxProbeBytes      int64
	MaxRequestBytes    int64
	MaxContextBytes    int64
	MaxContextTokens   int64
	MaxToolOutputBytes int64
	MaxReadBytes       int64
	MaxWriteBytes      int64
	MaxListEntries     int
	MaxSummaryBytes    int
	MaxOutputTokens    int
}

type resolvedConfig struct {
	endpoint           string
	client             *http.Client
	models             []agent.Option[agent.ModelID]
	efforts            []agent.Option[agent.EffortID]
	maxTurns           int
	maxToolCalls       int
	maxResponseBytes   int64
	maxProbeBytes      int64
	maxRequestBytes    int64
	maxContextBytes    int64
	maxContextTokens   int64
	maxToolOutputBytes int64
	maxReadBytes       int64
	maxWriteBytes      int64
	maxListEntries     int
	maxSummaryBytes    int
	maxOutputTokens    int
}

func resolveConfig(config Config) (resolvedConfig, error) {
	result := resolvedConfig{
		endpoint:           defaultEndpoint,
		maxTurns:           0,
		maxToolCalls:       0,
		maxResponseBytes:   4 << 20,
		maxProbeBytes:      1 << 20,
		maxRequestBytes:    8 << 20,
		maxContextBytes:    6 << 20,
		maxContextTokens:   0,
		maxToolOutputBytes: 256 << 10,
		maxReadBytes:       256 << 10,
		maxWriteBytes:      2 << 20,
		maxListEntries:     2_000,
		maxSummaryBytes:    16 << 10,
		maxOutputTokens:    0,
		models: []agent.Option[agent.ModelID]{
			{ID: "gpt-5.6", Label: "GPT-5.6", Description: "OpenAI GPT coding model", Default: true},
		},
		efforts: []agent.Option[agent.EffortID]{
			{ID: "low", Label: "Low"},
			{ID: "medium", Label: "Medium", Default: true},
			{ID: "high", Label: "High"},
			{ID: "xhigh", Label: "Extra high"},
		},
	}
	if config.Endpoint != "" {
		result.endpoint = config.Endpoint
	}
	if err := validateEndpoint(result.endpoint); err != nil {
		return resolvedConfig{}, err
	}
	if len(config.Models) > 0 {
		result.models = cloneOptions(config.Models)
	}
	if len(config.Efforts) > 0 {
		result.efforts = cloneOptions(config.Efforts)
	}
	if err := validateOptions("model", result.models); err != nil {
		return resolvedConfig{}, err
	}
	if err := validateOptions("effort", result.efforts); err != nil {
		return resolvedConfig{}, err
	}
	applyPositiveInt(&result.maxTurns, config.MaxTurns)
	applyPositiveInt(&result.maxToolCalls, config.MaxToolCalls)
	applyPositiveInt64(&result.maxResponseBytes, config.MaxResponseBytes)
	applyPositiveInt64(&result.maxProbeBytes, config.MaxProbeBytes)
	applyPositiveInt64(&result.maxRequestBytes, config.MaxRequestBytes)
	applyPositiveInt64(&result.maxContextBytes, config.MaxContextBytes)
	applyPositiveInt64(&result.maxContextTokens, config.MaxContextTokens)
	applyPositiveInt64(&result.maxToolOutputBytes, config.MaxToolOutputBytes)
	applyPositiveInt64(&result.maxReadBytes, config.MaxReadBytes)
	applyPositiveInt64(&result.maxWriteBytes, config.MaxWriteBytes)
	applyPositiveInt(&result.maxListEntries, config.MaxListEntries)
	applyPositiveInt(&result.maxSummaryBytes, config.MaxSummaryBytes)
	applyPositiveInt(&result.maxOutputTokens, config.MaxOutputTokens)
	if result.maxReadBytes > result.maxToolOutputBytes {
		return resolvedConfig{}, errors.New("max read bytes must not exceed max tool output bytes")
	}
	if result.maxResponseBytes > result.maxContextBytes || result.maxToolOutputBytes > result.maxContextBytes {
		return resolvedConfig{}, errors.New("response and tool output limits must not exceed context limit")
	}
	client := config.Client
	if client == nil {
		// Agent execution is governed by its cancelable job context. Do not add
		// an undisclosed wall-clock timeout at the HTTP transport layer.
		client = &http.Client{}
	}
	cloned := *client
	// Authentication must never follow a provider-controlled redirect.
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	result.client = &cloned
	return result, nil
}

var profileLimitFields = []struct {
	key         string
	label       string
	description string
	value       func(resolvedConfig) int64
}{
	{"max_turns", "Model turns", "0 means no Slopwatch turn budget; cancellation remains available.", func(v resolvedConfig) int64 { return int64(v.maxTurns) }},
	{"max_tool_calls", "Tool calls", "0 means no Slopwatch tool-call budget.", func(v resolvedConfig) int64 { return int64(v.maxToolCalls) }},
	{"max_output_tokens", "Output tokens / turn", "0 leaves the per-turn output budget to the provider/model.", func(v resolvedConfig) int64 { return int64(v.maxOutputTokens) }},
	{"max_context_tokens", "Input tokens / turn", "0 disables Slopwatch's usage-based token check.", func(v resolvedConfig) int64 { return v.maxContextTokens }},
	{"max_response_bytes", "Response bytes / job", "Cumulative provider response bytes accepted for one job.", func(v resolvedConfig) int64 { return v.maxResponseBytes }},
	{"max_probe_bytes", "Probe catalog bytes", "Maximum model-catalog response accepted by Test/Probe; this never times an active job.", func(v resolvedConfig) int64 { return v.maxProbeBytes }},
	{"max_request_bytes", "Request bytes / turn", "Maximum encoded request size sent on one model turn.", func(v resolvedConfig) int64 { return v.maxRequestBytes }},
	{"max_context_bytes", "Local context bytes", "Maximum locally retained prompt, response, and tool-result context.", func(v resolvedConfig) int64 { return v.maxContextBytes }},
	{"max_tool_output_bytes", "Tool result bytes", "Maximum bytes returned by one candidate tool call.", func(v resolvedConfig) int64 { return v.maxToolOutputBytes }},
	{"max_read_bytes", "File read bytes", "Maximum bytes read from one candidate file.", func(v resolvedConfig) int64 { return v.maxReadBytes }},
	{"max_write_bytes", "File write bytes", "Maximum bytes written to one candidate file.", func(v resolvedConfig) int64 { return v.maxWriteBytes }},
	{"max_list_entries", "Listed files", "Maximum entries returned by one candidate listing.", func(v resolvedConfig) int64 { return int64(v.maxListEntries) }},
	{"max_summary_bytes", "Summary bytes", "Maximum final summary retained and displayed.", func(v resolvedConfig) int64 { return int64(v.maxSummaryBytes) }},
}

func (config resolvedConfig) profileFields() []agent.ProfileField {
	fields := make([]agent.ProfileField, 0, len(profileLimitFields))
	for _, definition := range profileLimitFields {
		fields = append(fields, agent.ProfileField{
			Key: "options." + definition.key, OptionKey: definition.key,
			Label: definition.label, Description: definition.description,
			Kind:    agent.ProfileFieldText,
			Default: strconv.FormatInt(definition.value(config), 10), Pattern: `^[0-9]+$`,
		})
	}
	return fields
}

func (config resolvedConfig) withProfile(profile agent.Profile) (resolvedConfig, error) {
	result := config
	known := make(map[string]struct{}, len(profileLimitFields))
	for _, definition := range profileLimitFields {
		known[definition.key] = struct{}{}
		text, exists := profile.Options[definition.key]
		if !exists || text == "" {
			continue
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || value < 0 {
			return resolvedConfig{}, fmt.Errorf("%s must be a non-negative integer", definition.label)
		}
		if (definition.key == "max_turns" || definition.key == "max_tool_calls" || definition.key == "max_output_tokens" || definition.key == "max_list_entries" || definition.key == "max_summary_bytes") &&
			uint64(value) > uint64(^uint(0)>>1) {
			return resolvedConfig{}, fmt.Errorf("%s is too large for this platform", definition.label)
		}
		switch definition.key {
		case "max_turns":
			result.maxTurns = int(value)
		case "max_tool_calls":
			result.maxToolCalls = int(value)
		case "max_output_tokens":
			result.maxOutputTokens = int(value)
		case "max_context_tokens":
			result.maxContextTokens = value
		case "max_response_bytes":
			result.maxResponseBytes = value
		case "max_probe_bytes":
			result.maxProbeBytes = value
		case "max_request_bytes":
			result.maxRequestBytes = value
		case "max_context_bytes":
			result.maxContextBytes = value
		case "max_tool_output_bytes":
			result.maxToolOutputBytes = value
		case "max_read_bytes":
			result.maxReadBytes = value
		case "max_write_bytes":
			result.maxWriteBytes = value
		case "max_list_entries":
			result.maxListEntries = int(value)
		case "max_summary_bytes":
			result.maxSummaryBytes = int(value)
		}
	}
	for key := range profile.Options {
		if _, ok := known[key]; !ok {
			return resolvedConfig{}, fmt.Errorf("unsupported Responses API profile option %q", key)
		}
	}
	if result.maxResponseBytes <= 0 || result.maxProbeBytes <= 0 || result.maxRequestBytes <= 0 || result.maxContextBytes <= 0 || result.maxToolOutputBytes <= 0 ||
		result.maxReadBytes <= 0 || result.maxWriteBytes <= 0 || result.maxListEntries <= 0 || result.maxSummaryBytes <= 0 {
		return resolvedConfig{}, errors.New("byte, file-list, and summary limits must be positive")
	}
	if result.maxReadBytes > result.maxToolOutputBytes {
		return resolvedConfig{}, errors.New("file read bytes must not exceed tool result bytes")
	}
	if result.maxResponseBytes > result.maxContextBytes || result.maxToolOutputBytes > result.maxContextBytes {
		return resolvedConfig{}, errors.New("response and tool result bytes must not exceed local context bytes")
	}
	return result, nil
}

func applyPositiveInt(target *int, value int) {
	if value > 0 {
		*target = value
	}
}

func applyPositiveInt64(target *int64, value int64) {
	if value > 0 {
		*target = value
	}
}

func cloneOptions[T ~string](source []agent.Option[T]) []agent.Option[T] {
	return append([]agent.Option[T](nil), source...)
}

func validateOptions[T ~string](kind string, values []agent.Option[T]) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one %s is required", kind)
	}
	seen := make(map[T]struct{}, len(values))
	defaults := 0
	for _, value := range values {
		if value.ID == "" || value.Label == "" {
			return fmt.Errorf("%s IDs and labels are required", kind)
		}
		if _, exists := seen[value.ID]; exists {
			return fmt.Errorf("duplicate %s %q", kind, value.ID)
		}
		seen[value.ID] = struct{}{}
		if value.Default {
			defaults++
		}
	}
	if defaults > 1 {
		return fmt.Errorf("multiple default %ss", kind)
	}
	return nil
}

func validateEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || parsed.Path == "" {
		return errors.New("Responses endpoint must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.New("Responses endpoint must use HTTPS")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return errors.New("plain HTTP is permitted only for loopback endpoints")
		}
	}
	return nil
}

func modelsEndpoint(responsesEndpoint string) string {
	parsed, _ := url.Parse(responsesEndpoint)
	path := strings.TrimSuffix(parsed.Path, "/")
	if strings.HasSuffix(path, "/responses") {
		path = strings.TrimSuffix(path, "/responses")
	}
	parsed.Path = strings.TrimSuffix(path, "/") + "/models"
	return parsed.String()
}

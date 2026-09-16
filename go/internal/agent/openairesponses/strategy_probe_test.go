package openairesponses

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

func TestProbeNetworkFailureRemainsUnavailable(t *testing.T) {
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	probe := newTestStrategy(t, transport, Config{}).Probe(t.Context(), testProfile())
	if probe.State != agent.ProbeUnavailable || !strings.Contains(probe.Diagnostic, "could not be reached") {
		t.Fatalf("network probe = %#v", probe)
	}
}

func TestProbeRejectsProviderAuthenticationEcho(t *testing.T) {
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return testHTTPResponse(request, http.StatusOK, `{"data":[{"id":"`+testSecret+`"}]}`), nil
	})
	probe := newTestStrategy(t, transport, Config{}).Probe(t.Context(), testProfile())
	if probe.State != agent.ProbeIncompatible || strings.Contains(probe.Diagnostic, testSecret) {
		t.Fatalf("Probe()=%#v", probe)
	}
}

func TestProbeRejectsEmptyOrUnavailableModelCatalog(t *testing.T) {
	for _, body := range []string{`{"object":"list","data":[]}`, `{"object":"list","data":[{"id":"other-model"}]}`} {
		transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return testHTTPResponse(request, http.StatusOK, body), nil
		})
		probe := newTestStrategy(t, transport, Config{}).Probe(t.Context(), testProfile())
		if probe.State != agent.ProbeIncompatible || len(probe.Capabilities.Models) != 0 {
			t.Fatalf("unavailable model catalog was ready: %#v", probe)
		}
	}
}

func TestOfficialResponseEnvelopeCacheFieldsAreAccepted(t *testing.T) {
	root := canonicalTempDir(t)
	response := messageResponse("done", 3, 2, 1)
	response = strings.Replace(response, `"status":"completed"`, `"status":"completed","prompt_cache_options":{"mode":"implicit","ttl":"30m"},"conversation":{"id":"conversation"},"moderation":{"input":null,"output":null},"output_text":"done"`, 1)
	response = strings.Replace(response, `"role":"assistant"`, `"role":"assistant","phase":"final_answer"`, 1)
	response = strings.Replace(response, `"cached_tokens":0`, `"cached_tokens":0,"cache_write_tokens":2`, 1)
	provider := &scriptedProvider{t: t, responses: []string{response}}
	result := newTestStrategy(t, provider, Config{}).Execute(t.Context(), testProfile(), testRequest(t, root), nil)
	if result.Status != agent.ResultCompleted || result.Failure != agent.FailureNone {
		t.Fatalf("official-shaped response rejected: %#v", result)
	}
}

func TestProfileCapabilitiesAndProbeAreProviderOwned(t *testing.T) {
	provider := &scriptedProvider{t: t}
	strategy := newTestStrategy(t, provider, Config{})
	descriptor := strategy.ProfileDescriptor()
	if descriptor.Runtime != RuntimeKind || len(descriptor.Fields) != 1+len(profileLimitFields) || descriptor.Fields[0].Kind != agent.ProfileFieldAuthReference {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	preferenceLimits := make(map[string]agent.ProfileField, len(descriptor.Fields)-1)
	for _, field := range descriptor.Fields[1:] {
		preferenceLimits[field.OptionKey] = field
	}
	for _, definition := range profileLimitFields {
		field, ok := preferenceLimits[definition.key]
		if !ok || field.Key != "options."+definition.key || field.Kind != agent.ProfileFieldText || field.Pattern != `^[0-9]+$` || !field.PreferencesOnly {
			t.Fatalf("descriptor omitted, exposed, or malformed preference-only limit %q: %#v", definition.key, field)
		}
	}
	profile := testProfile()
	probe := strategy.Probe(t.Context(), profile)
	if probe.State != agent.ProbeReady || !probe.Capabilities.Isolation.EligibleForMutation() || probe.Capabilities.Network.ToolNetwork || !probe.Capabilities.Network.TransportRequired || probe.Capabilities.Resume {
		t.Fatalf("probe=%#v", probe)
	}
	if len(provider.authHeaders) != 1 || provider.authHeaders[0] != "Bearer "+testSecret {
		t.Fatalf("probe auth=%v", provider.authHeaders)
	}
	for _, invalid := range []agent.Profile{
		{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: testSecret},
		{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: "env:KEY", Executable: "sh"},
		{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: "env:KEY", Options: map[string]string{"api_key": testSecret}},
	} {
		if err := strategy.ValidateProfile(invalid); err == nil {
			t.Fatalf("invalid profile accepted: %#v", invalid)
		}
	}
}

func TestProbeGivesActionableEnvironmentAuthenticationRemediation(t *testing.T) {
	t.Parallel()
	strategy, err := New(Config{}, NewEnvironmentSecretResolver(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	profile := agent.Profile{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: "env:OPENAI_API_KEY"}
	probe := strategy.Probe(t.Context(), profile)
	if probe.State != agent.ProbeUnauthenticated || !strings.Contains(probe.Diagnostic, "Set environment variable OPENAI_API_KEY") {
		t.Fatalf("probe remediation = %#v", probe)
	}
}

func TestToolAndResponseLimitsFailClosed(t *testing.T) {
	t.Run("response bytes", func(t *testing.T) {
		provider := &scriptedProvider{t: t, responses: []string{messageResponse(strings.Repeat("x", 2_000), 1, 1, 0)}}
		strategy := newTestStrategy(t, provider, Config{MaxResponseBytes: 512})
		request := testRequest(t, canonicalTempDir(t))
		request.Limits.MaxOutputBytes = 512
		result := strategy.Execute(t.Context(), testProfile(), request, nil)
		if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, "output limit") {
			t.Fatalf("Execute()=%#v", result)
		}
	})
	t.Run("turns", func(t *testing.T) {
		provider := &scriptedProvider{t: t, responses: []string{
			functionResponse("c1", "list_files", `{"path":".","recursive":false}`, 1, 1),
		}}
		strategy := newTestStrategy(t, provider, Config{MaxTurns: 1})
		result := strategy.Execute(t.Context(), testProfile(), testRequest(t, canonicalTempDir(t)), nil)
		if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, "model-turn budget") {
			t.Fatalf("Execute()=%#v", result)
		}
	})
}

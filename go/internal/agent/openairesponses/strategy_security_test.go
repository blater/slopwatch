package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

func TestAuthenticationSecretNeverAppearsInResultsOrEvents(t *testing.T) {
	root := canonicalTempDir(t)
	profile := agent.Profile{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: "env:OPENAI_API_KEY"}
	strategy, err := New(Config{
		Models:  []agent.Option[agent.ModelID]{{ID: "gpt-test", Label: "GPT Test"}},
		Efforts: []agent.Option[agent.EffortID]{{ID: "high", Label: "High"}},
	}, SecretResolverFunc(func(context.Context, string) (string, error) {
		return "", fmt.Errorf("vault failed while handling %s", testSecret)
	}))
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.Event
	result := strategy.Execute(t.Context(), profile, testRequest(t, root), agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	encoded, _ := json.Marshal(struct {
		Result agent.Result
		Events []agent.Event
	}{result, events})
	if strings.Contains(string(encoded), testSecret) || result.Failure != agent.FailureUnauthenticated {
		t.Fatalf("authentication failure leaked: %s", encoded)
	}

	unauthorized := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return testHTTPResponse(request, http.StatusUnauthorized, `{"error":{"message":"`+testSecret+`"}}`), nil
	})
	strategy = newTestStrategy(t, unauthorized, Config{})
	result = strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
	if strings.Contains(result.Diagnostic, testSecret) || result.Failure != agent.FailureUnauthenticated {
		t.Fatalf("provider authentication failure leaked: %#v", result)
	}
}

func TestProfileRejectsAuthenticationReferencesTheInstalledResolverCannotUse(t *testing.T) {
	t.Parallel()
	strategy := newTestStrategy(t, &scriptedProvider{t: t}, Config{})
	for _, reference := range []string{"keychain:openai", "vault:openai", "sk-literal"} {
		profile := testProfile()
		profile.AuthenticationRef = reference
		if err := strategy.ValidateProfile(profile); err == nil || !strings.Contains(err.Error(), "env:VARIABLE") {
			t.Fatalf("ValidateProfile(%q) error = %v", reference, err)
		}
	}
}

func TestAuthenticationSecretNeverEntersOutboundProviderContext(t *testing.T) {
	provider := &scriptedProvider{t: t, responses: []string{messageResponse("unused", 1, 1, 0)}}
	strategy := newTestStrategy(t, provider, Config{})
	request := testRequest(t, canonicalTempDir(t))
	request.Task.Instructions.Objective = "Refactor without exposing " + testSecret
	result := strategy.Execute(t.Context(), testProfile(), request, nil)
	if result.Status != agent.ResultFailed || result.Failure != agent.FailureProtocol || len(provider.requests) != 0 || strings.Contains(result.Diagnostic, testSecret) {
		t.Fatalf("secret-bearing provider context was not rejected locally: result=%#v requests=%d", result, len(provider.requests))
	}
}

func TestProviderCannotEchoAuthenticationIntoMessagesOrToolArguments(t *testing.T) {
	root := canonicalTempDir(t)
	for _, response := range []string{
		messageResponse("provider repeated "+testSecret, 1, 1, 0),
		strings.Replace(functionResponse("c", "read_file", `{"path":"`+testSecret+`"}`, 1, 1), "test", `\u0074est`, 1),
	} {
		provider := &scriptedProvider{t: t, responses: []string{response}}
		strategy := newTestStrategy(t, provider, Config{})
		var events []agent.Event
		result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), agent.EventSinkFunc(func(event agent.Event) error {
			events = append(events, event)
			return nil
		}))
		encoded, _ := json.Marshal(struct {
			Result agent.Result
			Events []agent.Event
		}{result, events})
		if result.Failure != agent.FailureProtocol || strings.Contains(string(encoded), testSecret) {
			t.Fatalf("unsafe provider echo was not rejected safely: %s", encoded)
		}
	}
}

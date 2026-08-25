package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

const testSecret = "sk-test-auth-material-must-never-leak"
const testEndpoint = "http://127.0.0.1/v1/responses"

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type scriptedProvider struct {
	t           *testing.T
	mu          sync.Mutex
	responses   []string
	requests    [][]byte
	authHeaders []string
}

func (provider *scriptedProvider) RoundTrip(request *http.Request) (*http.Response, error) {
	provider.t.Helper()
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/models") {
		provider.authHeaders = append(provider.authHeaders, request.Header.Get("Authorization"))
		return testHTTPResponse(request, http.StatusOK, `{"object":"list","data":[{"id":"gpt-test"}]}`), nil
	}
	if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/responses") {
		return testHTTPResponse(request, http.StatusNotFound, `{"error":"unexpected request"}`), nil
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || !json.Valid(body) {
		provider.t.Errorf("decode request: %v", err)
		return testHTTPResponse(request, http.StatusBadRequest, `{"error":"bad request"}`), nil
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requests = append(provider.requests, append([]byte(nil), body...))
	provider.authHeaders = append(provider.authHeaders, request.Header.Get("Authorization"))
	if len(provider.responses) == 0 {
		provider.t.Error("unexpected provider turn")
		return testHTTPResponse(request, http.StatusInternalServerError, `{"error":"missing script"}`), nil
	}
	response := provider.responses[0]
	provider.responses = provider.responses[1:]
	return testHTTPResponse(request, http.StatusOK, response), nil
}

func testHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d test", status), Request: request,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
}

func TestStrategyImplementsControlledResponsesToolLoop(t *testing.T) {
	root := canonicalTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc old() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{t: t, responses: []string{
		functionResponse("call-read", "read_file", `{"path":"main.go"}`, 10, 3),
		functionResponse("call-write", "write_file", `{"path":"main.go","content":"package main\n\nfunc improved() {}\n"}`, 20, 5),
		messageResponse("Refactored main.go and preserved behavior.", 30, 7, 2),
	}}
	strategy := newTestStrategy(t, provider, Config{})
	var events []agent.Event
	result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	if result.Status != agent.ResultCompleted || result.Failure != agent.FailureNone || result.Summary != "Refactored main.go and preserved behavior." {
		t.Fatalf("Execute() = %#v", result)
	}
	if result.Usage.InputTokens != 60 || result.Usage.OutputTokens != 15 || result.Usage.ReasoningTokens != 2 || !result.Usage.Cumulative {
		t.Fatalf("usage = %#v", result.Usage)
	}
	contents, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || string(contents) != "package main\n\nfunc improved() {}\n" {
		t.Fatalf("candidate contents=%q err=%v", contents, err)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("provider requests=%d", len(provider.requests))
	}
	for index, body := range provider.requests {
		if bytesContainSecret(body) {
			t.Fatalf("request %d leaked authentication: %s", index, body)
		}
		if strings.Contains(string(body), "previous_response_id") || !strings.Contains(string(body), `"store":false`) || !strings.Contains(string(body), `"parallel_tool_calls":false`) {
			t.Fatalf("request %d does not use bounded local history: %s", index, body)
		}
	}
	for _, header := range provider.authHeaders {
		if header != "Bearer "+testSecret {
			t.Fatalf("authorization header=%q", header)
		}
	}
	if !strings.Contains(string(provider.requests[1]), `"type":"function_call_output"`) || !strings.Contains(string(provider.requests[1]), "func old") {
		t.Fatalf("read result was not returned through controlled tool output: %s", provider.requests[1])
	}
	var third struct {
		Input []struct {
			Type   string `json:"type"`
			Output string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(provider.requests[2], &third); err != nil {
		t.Fatal(err)
	}
	writeResultFound := false
	for _, item := range third.Input {
		if item.Type == "function_call_output" && strings.Contains(item.Output, `"bytes":33`) {
			writeResultFound = true
		}
	}
	if !writeResultFound {
		t.Fatalf("write result was not returned through controlled tool output: %s", provider.requests[2])
	}
	changed := false
	for _, event := range events {
		if event.ActorID != primaryActorID {
			t.Fatalf("event actor attribution is not stable: %#v", event)
		}
		if event.Kind == agent.EventFileChanged && event.Path == "main.go" {
			changed = true
		}
		if strings.Contains(event.Summary, testSecret) || strings.Contains(event.CommandID, testSecret) {
			t.Fatalf("event leaked secret: %#v", event)
		}
	}
	if !changed {
		t.Fatalf("missing normalized file event: %#v", events)
	}
}

func TestCrashLeftoverStagingCannotPoisonCandidateOrDeleteLookalikes(t *testing.T) {
	root := canonicalTempDir(t)
	request := testRequest(t, root)
	lookalike := filepath.Join(root, ".slopwatch-agent-unrelated.tmp")
	if err := os.WriteFile(lookalike, []byte("tracked lookalike"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.Workspace.StagingRoot, ".slopwatch-agent-crash.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools, err := newCandidateTools(request.Workspace, request.Write, resolvedConfig{maxWriteBytes: 1 << 20, maxReadBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	if _, err := tools.write(t.Context(), "main.go", "package main\n"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(lookalike)
	if err != nil || string(contents) != "tracked lookalike" {
		t.Fatalf("lookalike changed: %q %v", contents, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".slopwatch-agent-") && entry.Name() != filepath.Base(lookalike) {
			t.Fatalf("temporary artifact entered candidate: %s", entry.Name())
		}
	}
}

func TestToolBrokerRejectsTraversalAndDisallowedWrites(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments string
		code      string
	}{
		{name: "traversal", arguments: `{"path":"../outside.txt","content":"owned"}`, code: "invalid_path"},
		{name: "absolute", arguments: `{"path":"/tmp/outside.txt","content":"owned"}`, code: "invalid_path"},
		{name: "git", arguments: `{"path":".git/config","content":"owned"}`, code: "invalid_path"},
		{name: "disallowed", arguments: `{"path":"other.go","content":"owned"}`, code: "write_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			outside := filepath.Join(filepath.Dir(root), "outside.txt")
			_ = os.Remove(outside)
			provider := &scriptedProvider{t: t, responses: []string{
				functionResponse("call-write", "write_file", test.arguments, 1, 1),
				messageResponse("Handled the rejected operation.", 1, 1, 0),
			}}
			strategy := newTestStrategy(t, provider, Config{})
			result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
			if result.Status != agent.ResultCompleted {
				t.Fatalf("Execute() = %#v", result)
			}
			if len(provider.requests) != 2 || !strings.Contains(string(provider.requests[1]), test.code) {
				t.Fatalf("controlled error missing: %s", provider.requests[len(provider.requests)-1])
			}
			if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside path was touched: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "other.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disallowed path was touched: %v", err)
			}
		})
	}
}

func TestToolBrokerRejectsSymlinkAndDoesNotExposeTarget(t *testing.T) {
	root := canonicalTempDir(t)
	outsideRoot := canonicalTempDir(t)
	outside := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outside, []byte("candidate-external-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{t: t, responses: []string{
		functionResponse("call-read", "read_file", `{"path":"linked.go"}`, 1, 1),
		messageResponse("Skipped the unsupported link.", 1, 1, 0),
	}}
	strategy := newTestStrategy(t, provider, Config{})
	result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	returned := string(provider.requests[1])
	if !strings.Contains(returned, "symbolic links are not accessible") || strings.Contains(returned, "candidate-external-secret") {
		t.Fatalf("symlink result was unsafe: %s", returned)
	}
}

func TestExecutionCancellationStopsInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	root := canonicalTempDir(t)
	strategy := newTestStrategy(t, transport, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan agent.Result, 1)
	go func() { resultChannel <- strategy.Execute(ctx, testProfile(), testRequest(t, root), nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case result := <-resultChannel:
		if result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
			t.Fatalf("Execute() = %#v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled execution did not return")
	}
}

func TestMalformedProviderProtocolFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
	}{
		{name: "unknown envelope field", response: `{"status":"completed","output":[],"surprise":true}`},
		{name: "unknown output field", response: `{"status":"completed","output":[{"type":"function_call","call_id":"c","name":"read_file","arguments":"{}","surprise":true}]}`},
		{name: "unknown tool argument", response: functionResponse("c", "read_file", `{"path":"main.go","extra":true}`, 1, 1)},
		{name: "unknown tool", response: functionResponse("c", "run_shell", `{}`, 1, 1)},
		{name: "no action", response: `{"status":"completed","output":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedProvider{t: t, responses: []string{test.response}}
			strategy := newTestStrategy(t, provider, Config{})
			result := strategy.Execute(t.Context(), testProfile(), testRequest(t, canonicalTempDir(t)), nil)
			if result.Status != agent.ResultFailed || result.Failure != agent.FailureProtocol {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestAuthenticationSecretNeverAppearsInResultsOrEvents(t *testing.T) {
	root := canonicalTempDir(t)
	profile := agent.Profile{ID: "gpt", Runtime: RuntimeKind, AuthenticationRef: "vault:openai"}
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
	visibleLimits := make(map[string]agent.ProfileField, len(descriptor.Fields)-1)
	for _, field := range descriptor.Fields[1:] {
		visibleLimits[field.OptionKey] = field
	}
	for _, definition := range profileLimitFields {
		field, ok := visibleLimits[definition.key]
		if !ok || field.Key != "options."+definition.key || field.Kind != agent.ProfileFieldText || field.Pattern != `^[0-9]+$` {
			t.Fatalf("descriptor omitted or malformed visible limit %q: %#v", definition.key, field)
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

func newTestStrategy(t *testing.T, transport http.RoundTripper, overrides Config) *Strategy {
	t.Helper()
	overrides.Endpoint = testEndpoint
	overrides.Client = &http.Client{Transport: transport}
	overrides.Models = []agent.Option[agent.ModelID]{{ID: "gpt-test", Label: "GPT Test", Default: true}}
	overrides.Efforts = []agent.Option[agent.EffortID]{{ID: "high", Label: "High", Default: true}}
	strategy, err := New(overrides, SecretResolverFunc(func(_ context.Context, reference string) (string, error) {
		if reference != "env:OPENAI_API_KEY" {
			return "", errors.New("unknown reference containing " + testSecret)
		}
		return testSecret, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return strategy
}

func testProfile() agent.Profile {
	return agent.Profile{
		ID: "gpt", Label: "GPT", Runtime: RuntimeKind, AuthenticationRef: "env:OPENAI_API_KEY", Fingerprint: "profile-fingerprint",
	}
}

func testRequest(t *testing.T, root string) agent.Request {
	t.Helper()
	staging := filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+"-staging")
	if err := os.Mkdir(staging, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	allowed, err := fix.ParseRepoPath("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return agent.Request{
		JobID: "job-test", AttemptID: "attempt-test", Workspace: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: root, StagingRoot: staging},
		Task:  agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "Trusted remediation envelope", Objective: "Improve main.go"}},
		Model: "gpt-test", Effort: "high", Delegation: agent.DelegationSingle,
		Write:  agent.WritePolicy{Allowed: []fix.RepoPath{allowed}, Scope: "targets"},
		Limits: agent.Limits{MaxOutputBytes: 4 << 20, MaxEvents: 200},
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func functionResponse(callID, name, arguments string, input, output int64) string {
	encodedArguments, _ := json.Marshal(arguments)
	return fmt.Sprintf(`{"id":"response","status":"completed","output":[{"type":"function_call","id":"item","status":"completed","call_id":%q,"name":%q,"arguments":%s}],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":%d}}`, callID, name, encodedArguments, input, output, input+output)
}

func messageResponse(text string, input, output, reasoning int64) string {
	encodedText, _ := json.Marshal(text)
	return fmt.Sprintf(`{"id":"response","status":"completed","output":[{"type":"message","id":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":%s,"annotations":[],"logprobs":[]}]}],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":%d},"total_tokens":%d}}`, encodedText, input, output, reasoning, input+output)
}

func bytesContainSecret(value []byte) bool { return strings.Contains(string(value), testSecret) }

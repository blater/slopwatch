package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

type recordingExecutor struct {
	mu       sync.Mutex
	requests []isolation.Request
	results  []isolation.Result
	err      error
}

type liveExecutor struct {
	recordingExecutor
	started chan struct{}
	release chan struct{}
}

func TestProfileDescriptorAndValidationAreProviderOwned(t *testing.T) {
	descriptor := (&Strategy{}).ProfileDescriptor()
	if descriptor.Runtime != RuntimeKind || len(descriptor.Fields) < 3 || descriptor.Fields[0].Kind != agent.ProfileFieldExecutable || descriptor.Fields[1].Kind != agent.ProfileFieldAuthReference || descriptor.Fields[2].Kind != agent.ProfileFieldPathList {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	strategy := &Strategy{}
	if err := strategy.ValidateProfile(agent.Profile{ID: "codex", Runtime: RuntimeKind, Executable: "codex"}); err == nil || !strings.Contains(err.Error(), "provider-owned") {
		t.Fatalf("missing auth validation error = %v", err)
	}
	if err := strategy.ValidateProfile(agent.Profile{ID: "codex", Runtime: RuntimeKind, Executable: "codex", AuthenticationRef: "provider-owned"}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationStatusDistinguishesChatGPTAPIKeyAndSignedOut(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, output, method string
		signedIn             bool
	}{
		{name: "ChatGPT", output: "Logged in using ChatGPT", method: "chatgpt", signedIn: true},
		{name: "API key", output: "Logged in using API key", method: "api-key", signedIn: true},
		{name: "signed out substring", output: "Not logged in", signedIn: false},
		{name: "unknown", output: "authentication required", signedIn: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			authentication, signedIn := parseAuthenticationStatus(test.output)
			if signedIn != test.signedIn || authentication.Method != test.method {
				t.Fatalf("parseAuthenticationStatus(%q) = %#v, %t", test.output, authentication, signedIn)
			}
		})
	}
}

func (executor *liveExecutor) RunStreaming(_ context.Context, request isolation.Request, observe func([]byte)) (isolation.Result, error) {
	executor.mu.Lock()
	executor.requests = append(executor.requests, request)
	executor.mu.Unlock()
	first := []byte("{\"type\":\"thread.started\",\"thread_id\":\"live-thread\"}\n")
	observe(first)
	close(executor.started)
	<-executor.release
	last := []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"live complete\"}}\n")
	observe(last)
	return isolation.Result{ExitCode: 0, Stdout: append(first, last...)}, nil
}

func (executor *recordingExecutor) Run(_ context.Context, request isolation.Request) (isolation.Result, error) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	executor.requests = append(executor.requests, request)
	if executor.err != nil {
		return isolation.Result{}, executor.err
	}
	if len(executor.results) == 0 {
		return isolation.Result{}, errors.New("unexpected invocation")
	}
	result := executor.results[0]
	executor.results = executor.results[1:]
	return result, nil
}

func TestProbeFailsClosedWhenConfinementIsUnproven(t *testing.T) {
	t.Parallel()
	executable := fakeExecutable(t)
	executor := &recordingExecutor{results: probeResults()}
	strategy := New(executor, nil)
	strategy.workingDir = func() (string, error) { return t.TempDir(), nil }
	result := strategy.Probe(t.Context(), agent.Profile{Executable: executable})
	if result.State != agent.ProbeDegraded || result.Capabilities.Isolation.EligibleForMutation() {
		t.Fatalf("Probe() = %#v", result)
	}
	if result.Authentication.Method != "chatgpt" || result.Authentication.Label != "Signed in with ChatGPT" {
		t.Fatalf("Probe() authentication = %#v", result.Authentication)
	}
	if result.Capabilities.Resume {
		t.Fatal("ephemeral Codex adapter advertised resume")
	}
}

func TestExecutableCheckerDoesNotPromoteExactSandboxWithoutSharedCrashContainment(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(isolation.ProbeResult{CandidateWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{results: []isolation.Result{{ExitCode: 0, Stdout: payload}}}
	checker := ExecutableChecker{Runner: executor, HelperExecutable: os.Args[0], Confinement: isolation.UnsupportedConfinement{Reason: "no persistent descendant owner"}, Listen: func(string, string) (net.Listener, error) {
		return &testInertListener{closed: make(chan struct{})}, nil
	}}
	result := checker.Check(t.Context(), isolation.ConformanceRequest{
		Executable: "/codex", ProfileArguments: []string{"sandbox"}, CandidateRoot: "/candidate",
		GitCommonDir: "/git", OutsideRoot: t.TempDir(), SensitiveRoots: []string{"/secret"}, TransportAuthVerified: true,
		Limits: isolation.Limits{WallTime: time.Second, TerminateGrace: time.Second, MaxStdoutBytes: 1 << 20, MaxStderrBytes: 1 << 20},
	})
	if !result.CandidateWrite || !result.OutsideWriteDenied || !result.GitMetadataDenied || !result.SensitiveReadsDenied || !result.ToolNetworkPolicy || !result.TransportAuth {
		t.Fatalf("exact provider gates = %#v", result)
	}
	if result.CrashContainment || result.MutationEligible() || !strings.Contains(result.Diagnostic, "persistent descendant") {
		t.Fatalf("unproven shared confinement was promoted: %#v", result)
	}
}

func TestProbePolicyIsProfileOwnedAndDescriptorVisible(t *testing.T) {
	descriptor := (&Strategy{}).ProfileDescriptor()
	seen := map[string]bool{}
	for _, field := range descriptor.Fields {
		seen[field.OptionKey] = true
	}
	if !seen["probe_timeout"] || !seen["probe_output_bytes"] || !seen["termination_grace"] {
		t.Fatalf("probe settings absent from descriptor: %#v", descriptor.Fields)
	}
	profile := agent.Profile{ID: "codex", Runtime: RuntimeKind, Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{
		"probe_timeout": "37s", "probe_output_bytes": "123456", "termination_grace": "9s",
	}}
	if err := (&Strategy{}).ValidateProfile(profile); err != nil {
		t.Fatal(err)
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil || policy.timeout != 37*time.Second || policy.outputBytes != 123456 || policy.terminationGrace != 9*time.Second {
		t.Fatalf("probe policy=%+v err=%v", policy, err)
	}
}

type testInertListener struct{ closed chan struct{} }

func (listener *testInertListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, os.ErrClosed
}
func (listener *testInertListener) Close() error {
	select {
	case <-listener.closed:
	default:
		close(listener.closed)
	}
	return nil
}
func (*testInertListener) Addr() net.Addr { return testInertAddress("no-network") }

type testInertAddress string

func (address testInertAddress) Network() string { return "test" }
func (address testInertAddress) String() string  { return string(address) }

func TestExecuteBuildsConfinedEphemeralInvocationAndNormalizesEvents(t *testing.T) {
	t.Parallel()
	root := canonicalTestRoot(t)
	common := filepath.Join(root, "common.git")
	candidate := filepath.Join(root, "candidate")
	secret := filepath.Join(root, "secret")
	for _, path := range []string{common, candidate, secret, filepath.Join(candidate, ".git")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executor := &recordingExecutor{results: append(probeResults(), isolation.Result{ExitCode: 0, Stdout: []byte(strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-1"}`,
		`{"type":"item.started","item":{"id":"cmd-1","type":"command_execution","command":"go test ./..."}}`,
		`{"type":"future.event","new_field":true}`,
		`{"type":"item.completed","item":{"id":"change-1","type":"file_change","changes":[{"path":"main.go"}]}}`,
		`{"type":"item.completed","item":{"id":"message-1","type":"agent_message","text":"Fixed the target."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":5,"output_tokens":8,"reasoning_output_tokens":3}}`,
	}, "\n") + "\n")})}
	strategy := New(executor, isolation.CheckerFunc(func(context.Context, isolation.ConformanceRequest) isolation.Conformance {
		return passingConformance()
	}))
	strategy.workingDir = func() (string, error) { return root, nil }
	strategy.getenv = func(key string) string {
		switch key {
		case "PATH":
			return "/usr/bin:/bin"
		case "HOME":
			return root
		case "OPENAI_API_KEY":
			return "must-not-leak"
		default:
			return ""
		}
	}
	path, err := fix.ParseRepoPath("main.go")
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Request{
		JobID: "job-1", AttemptID: "attempt-1", Model: "gpt-5.6-sol", Effort: "high", Delegation: agent.DelegationSingle,
		Workspace: fix.CandidateIdentity{RepositoryRoot: candidate, GitCommonDir: common},
		Task:      agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "trusted envelope", Objective: "fix main.go"}},
		Limits:    agent.Limits{MaxOutputBytes: 1 << 20, MaxEvents: 100},
	}
	var events []agent.Event
	result := strategy.Execute(t.Context(), agent.Profile{Executable: fakeExecutable(t), Options: map[string]string{"denied_read_roots": secret}}, request, agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	if result.Status != agent.ResultCompleted || result.Summary != "Fixed the target." || result.SessionReference != "thread-1" {
		t.Fatalf("Execute() = %#v", result)
	}
	if result.Usage.InputTokens != 20 || result.Usage.ReasoningTokens != 3 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if len(events) != 5 || events[2].Path != path {
		t.Fatalf("events = %#v", events)
	}
	executor.mu.Lock()
	invocation := executor.requests[len(executor.requests)-1]
	executor.mu.Unlock()
	joined := strings.Join(invocation.Arguments, "\n")
	for _, wanted := range []string{"default_permissions=\"slopwatch\"", common, secret, "--ephemeral", "--ignore-user-config", "--ignore-rules", "--disable\nmulti_agent"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("invocation does not contain %q: %v", wanted, invocation.Arguments)
		}
	}
	if strings.Contains(joined, "trusted envelope") || string(invocation.Stdin) != "trusted envelope\n\nfix main.go" {
		t.Fatalf("prompt was not isolated to stdin: args=%v stdin=%q", invocation.Arguments, invocation.Stdin)
	}
	for _, value := range invocation.Environment {
		if strings.Contains(value, "OPENAI_API_KEY") || strings.Contains(value, "must-not-leak") {
			t.Fatalf("secret environment leaked: %q", value)
		}
	}
}

func TestExecuteRejectsMalformedAndExcessJSONL(t *testing.T) {
	t.Parallel()
	request := minimalRequest(t)
	for _, test := range []struct {
		name   string
		output string
		limit  int
	}{
		{name: "malformed", output: "not-json\n", limit: 10},
		{name: "too_many", output: "{}\n{}\n", limit: 1},
		{name: "too_many_actors", output: strings.Join([]string{
			`{"type":"item.started","item":{"type":"command_execution","agent_id":"one"}}`,
			`{"type":"item.started","item":{"type":"command_execution","agent_id":"two"}}`,
		}, "\n") + "\n", limit: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingExecutor{results: append(probeResults(), isolation.Result{ExitCode: 0, Stdout: []byte(test.output)})}
			strategy := readyStrategy(t, executor)
			request.Limits.MaxEvents = test.limit
			if test.name == "too_many_actors" {
				request.Limits.MaxActors = 1
			}
			result := strategy.Execute(t.Context(), agent.Profile{Executable: fakeExecutable(t)}, request, nil)
			if result.Failure != agent.FailureProtocol {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestExecuteEmitsStructuredProgressBeforeProcessExit(t *testing.T) {
	executor := &liveExecutor{recordingExecutor: recordingExecutor{results: probeResults()}, started: make(chan struct{}), release: make(chan struct{})}
	strategy := readyStrategy(t, executor)
	request := minimalRequest(t)
	events := make(chan agent.Event, 4)
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- strategy.Execute(t.Context(), agent.Profile{Executable: fakeExecutable(t)}, request, agent.EventSinkFunc(func(event agent.Event) error {
			events <- event
			return nil
		}))
	}()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("streaming execution did not start")
	}
	select {
	case event := <-events:
		if event.Kind != agent.EventStarted {
			t.Fatalf("first live event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("no structured event arrived while the runtime was still active")
	}
	select {
	case result := <-finished:
		t.Fatalf("runtime completed before release: %#v", result)
	default:
	}
	close(executor.release)
	if result := <-finished; result.Status != agent.ResultCompleted || result.Summary != "live complete" {
		t.Fatalf("streamed Execute() = %#v", result)
	}
}

func probeResults() []isolation.Result {
	return []isolation.Result{
		{ExitCode: 0, Stdout: []byte("codex-cli 0.149.1\n")},
		{ExitCode: 0, Stdout: []byte("Logged in using ChatGPT\n")},
		{ExitCode: 0, Stdout: []byte(`{"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"},{"effort":"ultra"}]}]}`)},
	}
}

func passingConformance() isolation.Conformance {
	return isolation.Conformance{
		CandidateWrite: true, OutsideWriteDenied: true, GitMetadataDenied: true,
		SensitiveReadsDenied: true, ToolNetworkPolicy: true, TransportAuth: true, CrashContainment: true,
	}
}

func readyStrategy(t *testing.T, executor isolation.Executor) *Strategy {
	t.Helper()
	strategy := New(executor, isolation.CheckerFunc(func(context.Context, isolation.ConformanceRequest) isolation.Conformance { return passingConformance() }))
	strategy.workingDir = func() (string, error) { return t.TempDir(), nil }
	return strategy
}

func minimalRequest(t *testing.T) agent.Request {
	t.Helper()
	root := canonicalTestRoot(t)
	common := filepath.Join(root, "common")
	candidate := filepath.Join(root, "candidate")
	if err := os.Mkdir(common, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	return agent.Request{
		JobID: "job", AttemptID: "attempt", Model: "gpt-5.6-sol", Effort: "high", Delegation: agent.DelegationSingle,
		Workspace: fix.CandidateIdentity{RepositoryRoot: candidate, GitCommonDir: common},
		Task:      agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "envelope", Objective: "objective"}},
		Limits:    agent.Limits{MaxOutputBytes: 1 << 20, MaxEvents: 10},
	}
}

func fakeExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func canonicalTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

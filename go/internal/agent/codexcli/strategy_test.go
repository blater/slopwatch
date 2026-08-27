package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/fix"
)

func TestProfileDescriptorKeepsOperationalDetailsInPreferences(t *testing.T) {
	descriptor := New().ProfileDescriptor()
	if descriptor.Runtime != RuntimeKind || descriptor.Label != "Codex" || descriptor.DocumentationURL != "https://developers.openai.com/codex/auth" || !strings.Contains(descriptor.ConnectionInstructions, "codex login") {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	for _, field := range descriptor.Fields {
		if !field.PreferencesOnly {
			t.Fatalf("operational field is visible in the Agents dialog: %#v", field)
		}
	}
}

func TestProbeUsesAppServerAccountAndModelCatalog(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	strategy := New()
	strategy.workingDir = func() (string, error) { return t.TempDir(), nil }
	result := strategy.Probe(t.Context(), testProfile(fakeAppServerExecutable(t, "complete", capture)))
	if result.State != agent.ProbeReady || result.Version != "0.149.1" {
		t.Fatalf("Probe() = %#v", result)
	}
	if result.Authentication.Method != "chatgpt" || !strings.Contains(result.Authentication.Label, "Plus") {
		t.Fatalf("authentication = %#v", result.Authentication)
	}
	if len(result.Capabilities.Models) != 1 || result.Capabilities.Models[0].ID != "gpt-5.6-sol" || len(result.Capabilities.Efforts) != 2 {
		t.Fatalf("capabilities = %#v", result.Capabilities)
	}
	isolation := result.Capabilities.Isolation
	if !isolation.ProviderManagedCancellation || isolation.CrashContainment || isolation.SensitiveReadsDenied || isolation.TransportAuthIsolated || !isolation.EligibleForMutation() {
		t.Fatalf("App Server lifecycle was misrepresented: %#v", isolation)
	}
	methods := capturedMethods(t, capture)
	for _, wanted := range []string{"initialize", "initialized", "account/read", "model/list"} {
		if !containsString(methods, wanted) {
			t.Fatalf("missing %q in %v", wanted, methods)
		}
	}
}

func TestExecuteStreamsEventsAndUsesWorkspaceWrite(t *testing.T) {
	root := canonicalTestRoot(t)
	common := filepath.Join(root, "common.git")
	candidate := filepath.Join(root, "candidate")
	staging := filepath.Join(root, "staging")
	for _, path := range []string{common, candidate, staging} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(staging, "targets.txt")
	if err := os.WriteFile(manifestPath, []byte("main.go\nother.go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "capture.jsonl")
	strategy := New()
	strategy.workingDir = func() (string, error) { return root, nil }
	var events []agent.Event
	request := testRequest(candidate, common)
	request.Workspace.StagingRoot = staging
	request.Task.Instructions.Objective += "\nManifest {target_manifest}; files {target_manifest_count}."
	request.Task.Manifest = &agent.TargetManifest{Path: manifestPath, Count: 2}
	result := strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "complete", capture)), request, agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	if result.Status != agent.ResultCompleted || result.Summary != "Fixed the target." || result.SessionReference != "thread-1" {
		t.Fatalf("Execute() = %#v", result)
	}
	if result.Usage.InputTokens != 20 || result.Usage.ReasoningTokens != 3 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	foundPath := false
	for _, event := range events {
		if event.Kind == agent.EventFileChanged && event.Path == "main.go" {
			foundPath = true
		}
	}
	if len(events) < 5 || events[0].Kind != agent.EventStarted || !foundPath {
		t.Fatalf("events = %#v", events)
	}
	requests := capturedMessages(t, capture)
	initializeCount := 0
	for _, message := range requests {
		if message.Method == "initialize" {
			initializeCount++
		}
	}
	if initializeCount != 1 {
		t.Fatalf("fix attempt launched %d App Server sessions, want 1", initializeCount)
	}
	thread := findCaptured(t, requests, "thread/start")
	turn := findCaptured(t, requests, "turn/start")
	if stringField(thread.Params, "sandbox") != "workspace-write" || stringField(thread.Params, "cwd") != candidate {
		t.Fatalf("thread/start params = %#v", thread.Params)
	}
	sandbox, _ := turn.Params["sandboxPolicy"].(map[string]any)
	if stringField(sandbox, "type") != "workspaceWrite" || sandbox["networkAccess"] != false {
		t.Fatalf("turn sandbox = %#v", sandbox)
	}
	if !strings.Contains(fmt.Sprint(sandbox["writableRoots"]), staging) {
		t.Fatalf("target manifest directory was not exposed to Codex: %#v", sandbox)
	}
	input, _ := turn.Params["input"].([]any)
	first, _ := input[0].(map[string]any)
	if !strings.Contains(stringField(first, "text"), "Manifest "+manifestPath+"; files 2.") {
		t.Fatalf("turn input = %#v", input)
	}
}

func TestExecutePrefersCompletionWhenServerExitsImmediatelyAfterIt(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		root := canonicalTestRoot(t)
		common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
		for _, path := range []string{common, candidate} {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "completeexit", filepath.Join(root, "capture"))), testRequest(candidate, common), nil)
		if result.Status != agent.ResultCompleted {
			t.Fatalf("attempt %d: Execute() = %#v", attempt, result)
		}
	}
}

func TestZeroOutputBudgetAllowsUnlimitedBoundedProtocolFrames(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	request.Limits.MaxOutputBytes = 0
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "cumulative", filepath.Join(root, "capture"))), request, nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("unlimited bounded frames failed: %#v", result)
	}
}

func TestZeroOutputBudgetStillRejectsOversizedProtocolFrame(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	request.Limits.MaxOutputBytes = 0
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "oversizeframe", filepath.Join(root, "capture"))), request, nil)
	if result.Status == agent.ResultCompleted || !strings.Contains(result.Diagnostic, "token too long") {
		t.Fatalf("oversized protocol frame was accepted: %#v", result)
	}
}

func TestExecuteReconcilesLostTurnCompletionFromAuthoritativeThreadState(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	strategy := New()
	strategy.reconcileEvery = 10 * time.Millisecond
	result := strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "lostcompletion", filepath.Join(root, "capture"))), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted || result.Summary != "Fixed the target." {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestCompletionReconciliationNeverStopsAnActiveCodexTurn(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	strategy := New()
	strategy.reconcileEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- strategy.Execute(ctx, testProfile(fakeAppServerExecutable(t, "lostactive", capture)), testRequest(candidate, common), nil)
	}()
	waitForCapturedMethod(t, capture, "thread/read")
	cancel()
	select {
	case result := <-finished:
		if result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
			t.Fatalf("active reconciliation result = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active Codex turn did not remain cancelable")
	}
}

func TestCompletionReconciliationRejectsForeignOrMissingThreadIdentity(t *testing.T) {
	for _, test := range []struct {
		mode string
		want string
	}{
		{mode: "lostforeign", want: "foreign thread"},
		{mode: "lostmissing", want: "no thread id"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			root := canonicalTestRoot(t)
			common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
			for _, path := range []string{common, candidate} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			strategy := New()
			strategy.reconcileEvery = 10 * time.Millisecond
			result := strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, test.mode, filepath.Join(root, "capture"))), testRequest(candidate, common), nil)
			if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, test.want) {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestOutstandingCompletionReadCannotMaskOwnedTurnCompletion(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	strategy := New()
	strategy.reconcileEvery = 10 * time.Millisecond
	result := strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "readblockedcomplete", filepath.Join(root, "capture"))), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted || result.Summary != "Fixed the target." {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestCancellationJoinsOutstandingCompletionRead(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	strategy := New()
	strategy.reconcileEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- strategy.Execute(ctx, testProfile(fakeAppServerExecutable(t, "readblockedcancel", capture)), testRequest(candidate, common), nil)
	}()
	waitForCapturedMethod(t, capture, "thread/read")
	cancel()
	select {
	case result := <-finished:
		if result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
			t.Fatalf("Execute() = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not join the outstanding completion read")
	}
}

func TestReconciliationCannotOutrunFailingFinalAnswerSink(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	strategy := New()
	strategy.reconcileEvery = 10 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "lostcompletion", capture)), testRequest(candidate, common), agent.EventSinkFunc(func(event agent.Event) error {
			if event.Kind == agent.EventRuntimeMessage && event.Summary == "Fixed the target." {
				close(entered)
				<-release
				return errors.New("sink rejected final answer")
			}
			return nil
		}))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("final-answer event did not reach the sink")
	}
	close(release)
	select {
	case result := <-finished:
		if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, "sink rejected final answer") {
			t.Fatalf("Execute() = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sink failure did not terminate the attempt")
	}
}

func TestExecuteIgnoresForeignThreadAndTurnEvents(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var events []agent.Event
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "foreign", filepath.Join(root, "capture"))), testRequest(candidate, common), agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	if result.Status != agent.ResultCompleted || result.Summary != "Fixed the target." {
		t.Fatalf("Execute() = %#v", result)
	}
	for _, event := range events {
		if strings.Contains(event.Summary, "foreign") {
			t.Fatalf("foreign event contaminated attempt: %#v", events)
		}
	}
}

func TestExecuteCancellationBeforeAndDuringInitializationIsCanceled(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result := New().Execute(ctx, testProfile("not-resolved"), request, nil); result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
		t.Fatalf("pre-canceled result = %#v", result)
	}

	capture := filepath.Join(root, "capture")
	ctx, cancel = context.WithCancel(t.Context())
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- New().Execute(ctx, testProfile(fakeAppServerExecutable(t, "blockinit", capture)), request, nil)
	}()
	waitForCapturedMethod(t, capture, "initialize")
	cancel()
	select {
	case result := <-finished:
		if result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
			t.Fatalf("initialize cancellation = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initialization cancellation did not finish")
	}
}

func TestExecuteEnforcesConfiguredProviderOutputAndEventBudgets(t *testing.T) {
	for _, test := range []struct {
		name      string
		mode      string
		bytes     int64
		maxEvents int
	}{
		{name: "oversized frame", mode: "oversize", bytes: 4096},
		{name: "event flood", mode: "flood", bytes: 1 << 20, maxEvents: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTestRoot(t)
			common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
			for _, path := range []string{common, candidate} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			request := testRequest(candidate, common)
			request.Limits.MaxOutputBytes, request.Limits.MaxEvents = test.bytes, test.maxEvents
			result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, test.mode, filepath.Join(root, "capture"))), request, nil)
			if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, "configured") && !strings.Contains(result.Diagnostic, "more than") {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestExecuteSuppressesNotificationsAfterTurnCompletion(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var summaries []string
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "trailing", filepath.Join(root, "capture"))), testRequest(candidate, common), agent.EventSinkFunc(func(event agent.Event) error {
		mu.Lock()
		summaries = append(summaries, event.Summary)
		mu.Unlock()
		return nil
	}))
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, summary := range summaries {
		if strings.Contains(summary, "late provider warning") {
			t.Fatalf("post-terminal notification reached sink: %v", summaries)
		}
	}
}

func TestOwnedCompletionSynchronouslySuppressesNextNotification(t *testing.T) {
	request := agent.Request{JobID: "job", AttemptID: "attempt", Limits: agent.Limits{MaxOutputBytes: 1 << 20}}
	var events []agent.Event
	run := newAppServerRun(request, agent.EventSinkFunc(func(event agent.Event) error {
		events = append(events, event)
		return nil
	}))
	run.setThread("thread-1")
	run.setTurn("turn-1")
	completedParams, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}})
	warningParams, _ := json.Marshal(map[string]any{"message": "must not be emitted"})
	run.handle(rpcMessage{Method: "turn/completed", Params: completedParams})
	run.handle(rpcMessage{Method: "warning", Params: warningParams})
	if len(events) != 0 {
		t.Fatalf("post-completion notification reached sink: %#v", events)
	}
	select {
	case completion := <-run.completed:
		if completion.Status != "completed" {
			t.Fatalf("completion = %#v", completion)
		}
	default:
		t.Fatal("owned completion was not published")
	}
}

func TestCompletionBeforeTurnBindingCannotClaimOwnedTurn(t *testing.T) {
	run := newAppServerRun(agent.Request{JobID: "job", AttemptID: "attempt"}, nil)
	run.setThread("thread-1")
	foreign, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "foreign-turn", "status": "completed"}})
	owned, _ := json.Marshal(map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}})
	run.handle(rpcMessage{Method: "turn/completed", Params: foreign})
	run.handle(rpcMessage{Method: "turn/completed", Params: owned})
	run.setTurn("turn-1")
	select {
	case completion := <-run.completed:
		if completion.Status != "completed" {
			t.Fatalf("completion = %#v", completion)
		}
	default:
		t.Fatal("owned buffered completion was not published")
	}
}

func TestActorLimitFailureWakesSilentAttempt(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	request.Limits.MaxActors = 1
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "actoroverflow", filepath.Join(root, "capture"))), request, nil)
	if result.Failure != agent.FailureProtocol || !strings.Contains(result.Diagnostic, "more than 1 actors") {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestCloseJoinsAppServerStdoutReader(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	client, err := startAppServer(fakeAppServerExecutable(t, "readerjoin", filepath.Join(t.TempDir(), "capture")), t.TempDir(), os.Environ(), 1<<20, func(message rpcMessage) {
		if message.Method == "warning" {
			close(started)
			<-release
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var initialized map[string]any
	if err := client.Request(t.Context(), "initialize", map[string]any{}, &initialized); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stdout handler did not start")
	}
	closed := make(chan error, 1)
	go func() {
		closed <- client.Close(500 * time.Millisecond)
	}()
	select {
	case err := <-closed:
		t.Errorf("Close returned before the stdout reader finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join the released stdout reader")
	}
}

func TestUnexpectedServerRequestIsRejected(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture")
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "serverrequest", capture)), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), `"id":99`) || !strings.Contains(string(data), `"code":-32601`) {
		t.Fatalf("unexpected server request was not explicitly rejected: %q err=%v", data, err)
	}
}

func TestUnsupportedServerRequestFailsFastWhenOutboundQueueIsFull(t *testing.T) {
	client := &appServerClient{
		pending:  make(map[int64]chan rpcResponse),
		outbound: make(chan outboundMessage, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		maximum:  1 << 20,
	}
	client.outbound <- outboundMessage{}
	finished := make(chan struct{})
	go func() {
		client.read(strings.NewReader("{\"id\":99,\"method\":\"item/permissions/requestApproval\"}\n"))
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("reader blocked while the outbound queue was full")
	}
	if err := client.protocolExitError(); err == nil || !strings.Contains(err.Error(), "more unsupported requests than could be rejected") {
		t.Fatalf("protocolExitError() = %v", err)
	}
}

func TestProbeReportsActionableEarlyAppServerExit(t *testing.T) {
	strategy := New()
	strategy.workingDir = func() (string, error) { return t.TempDir(), nil }
	result := strategy.Probe(t.Context(), testProfile(fakeAppServerExecutable(t, "fail", filepath.Join(t.TempDir(), "capture"))))
	if result.State != agent.ProbeIncompatible || !strings.Contains(result.Diagnostic, "exited before responding") ||
		!strings.Contains(result.Diagnostic, "install or update Codex") || !strings.Contains(result.Diagnostic, "Test again") {
		t.Fatalf("Probe() = %#v", result)
	}
}

func TestCancellationUsesTurnInterrupt(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture.jsonl")
	strategy := New()
	strategy.workingDir = func() (string, error) { return root, nil }
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	finished := make(chan agent.Result, 1)
	go func() {
		finished <- strategy.Execute(ctx, testProfile(fakeAppServerExecutable(t, "cancel", capture)), testRequest(candidate, common), agent.EventSinkFunc(func(event agent.Event) error {
			if event.Kind == agent.EventStarted {
				select {
				case <-started:
				default:
					close(started)
				}
			}
			return nil
		}))
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("Codex turn did not start")
	}
	cancel()
	select {
	case result := <-finished:
		if result.Status != agent.ResultCanceled || result.Failure != agent.FailureCancellation {
			t.Fatalf("canceled result = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled turn did not finish")
	}
	interrupt := findCaptured(t, capturedMessages(t, capture), "turn/interrupt")
	if stringField(interrupt.Params, "threadId") != "thread-1" || stringField(interrupt.Params, "turnId") != "turn-1" {
		t.Fatalf("interrupt params = %#v", interrupt.Params)
	}
}

func TestCodexAppServerHelper(t *testing.T) {
	if !containsString(os.Args, "app-server") {
		return
	}
	if containsString(os.Args, "--disable") || containsString(os.Args, "-c") {
		fmt.Fprintln(os.Stderr, "unexpected App Server launch constraint")
		os.Exit(2)
	}
	serveFakeAppServer(os.Getenv("SLOPMOCHI_FAKE_MODE"), os.Getenv("SLOPMOCHI_FAKE_CAPTURE"))
	os.Exit(0)
}

func serveFakeAppServer(mode, capture string) {
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fake app-server startup failure")
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(line, &request) != nil {
			continue
		}
		appendCapture(capture, line)
		respond := func(value any) {
			encoded, _ := json.Marshal(map[string]any{"id": json.RawMessage(request.ID), "result": value})
			fmt.Println(string(encoded))
		}
		respondError := func(message string) {
			encoded, _ := json.Marshal(map[string]any{"id": json.RawMessage(request.ID), "error": map[string]any{"code": -32602, "message": message}})
			fmt.Println(string(encoded))
		}
		notify := func(method string, params any) {
			encoded, _ := json.Marshal(map[string]any{"method": method, "params": params})
			fmt.Println(string(encoded))
		}
		switch request.Method {
		case "initialize":
			if mode == "blockinit" {
				continue
			}
			respond(map[string]any{"userAgent": "codex-cli/0.149.1 (test)"})
			if mode == "readerjoin" {
				notify("warning", map[string]any{"message": "reader is active"})
				return
			}
		case "initialized":
		case "account/read":
			respond(map[string]any{"account": map[string]any{"type": "chatgpt", "planType": "plus", "email": "test@example.com"}, "requiresOpenaiAuth": true})
		case "model/list":
			respond(map[string]any{"data": []any{map[string]any{
				"model": "gpt-5.6-sol", "displayName": "GPT-5.6-Sol", "isDefault": true,
				"defaultReasoningEffort": "high", "supportedReasoningEfforts": []any{
					map[string]any{"reasoningEffort": "low", "description": "Low"},
					map[string]any{"reasoningEffort": "high", "description": "High"},
				},
			}}, "nextCursor": nil})
		case "thread/start":
			if stringField(request.Params, "sandbox") != "workspace-write" {
				respondError("invalid thread sandbox")
				continue
			}
			respond(map[string]any{"thread": map[string]any{"id": "thread-1"}})
		case "turn/start":
			sandbox, _ := request.Params["sandboxPolicy"].(map[string]any)
			if stringField(sandbox, "type") != "workspaceWrite" {
				respondError("invalid turn sandbox")
				continue
			}
			respond(map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}}})
			notify("turn/started", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1"}})
			if mode == "serverrequest" {
				encoded, _ := json.Marshal(map[string]any{"id": 99, "method": "item/permissions/requestApproval", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"}})
				fmt.Println(string(encoded))
				// Give the client writer a chance to put the rejection on stdin
				// before this deliberately unrealistic peer completes the turn.
				time.Sleep(50 * time.Millisecond)
			}
			if mode == "descendant" {
				startFakeDescendant(capture)
			}
			if mode == "stubborn" {
				startTermIgnoringDescendant(capture)
			}
			if mode == "foreign" {
				notify("item/completed", map[string]any{"threadId": "foreign-thread", "turnId": "foreign-turn", "item": map[string]any{"id": "foreign", "type": "agentMessage", "text": "foreign result"}})
				notify("turn/completed", map[string]any{"threadId": "foreign-thread", "turn": map[string]any{"id": "foreign-turn", "status": "completed", "items": []any{}}})
			}
			if mode == "oversize" {
				notify("item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "large", "type": "agentMessage", "text": strings.Repeat("x", 8192)}})
			}
			if mode == "flood" {
				for index := 0; index < 4; index++ {
					notify("warning", map[string]any{"message": fmt.Sprintf("warning %d", index)})
				}
			}
			if mode == "actoroverflow" {
				identity := map[string]any{"threadId": "thread-1", "turnId": "turn-1"}
				for _, actor := range []string{"actor-1", "actor-2"} {
					notify("item/completed", mergeMap(identity, map[string]any{"item": map[string]any{"id": actor, "type": "collabAgentToolCall", "receiverThreadIds": []any{actor}}}))
				}
			}
			if mode == "cumulative" {
				for index := 0; index < 160; index++ {
					notify("warning", map[string]any{"message": strings.Repeat("x", 8192)})
				}
			}
			if mode == "oversizeframe" {
				notify("warning", map[string]any{"message": strings.Repeat("x", diagnosticCaptureLimit+1)})
				return
			}
			if mode == "complete" || mode == "completeexit" || mode == "cumulative" || mode == "foreign" || mode == "flood" || mode == "descendant" || mode == "stubborn" || mode == "serverrequest" || mode == "trailing" || mode == "lostcompletion" || mode == "lostactive" || mode == "lostforeign" || mode == "lostmissing" || mode == "readblockedcomplete" || mode == "readblockedcancel" {
				identity := map[string]any{"threadId": "thread-1", "turnId": "turn-1"}
				notify("item/started", mergeMap(identity, map[string]any{"item": map[string]any{"id": "cmd-1", "type": "commandExecution", "command": "go test ./..."}}))
				notify("item/completed", mergeMap(identity, map[string]any{"item": map[string]any{"id": "change-1", "type": "fileChange", "changes": []any{map[string]any{"path": "main.go"}}}}))
				notify("item/completed", mergeMap(identity, map[string]any{"item": map[string]any{"id": "message-1", "type": "agentMessage", "phase": "final_answer", "text": "Fixed the target."}}))
				notify("thread/tokenUsage/updated", mergeMap(identity, map[string]any{"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 20, "cachedInputTokens": 5, "outputTokens": 8, "reasoningOutputTokens": 3}}}))
				if mode != "lostcompletion" && mode != "lostactive" && mode != "lostforeign" && mode != "lostmissing" && mode != "readblockedcomplete" && mode != "readblockedcancel" {
					notify("turn/completed", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed", "items": []any{}}})
				}
				if mode == "trailing" {
					notify("warning", map[string]any{"message": "late provider warning"})
				}
				if mode == "completeexit" {
					return
				}
			}
		case "turn/interrupt":
			respond(map[string]any{})
			notify("turn/completed", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "interrupted", "items": []any{}}})
		case "thread/read":
			switch mode {
			case "readblockedcomplete":
				notify("turn/completed", map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed", "items": []any{}}})
			case "readblockedcancel":
			case "lostactive":
				respond(map[string]any{"thread": map[string]any{"id": "thread-1", "status": map[string]any{"type": "active"}, "turns": []any{}}})
			case "lostforeign":
				respond(map[string]any{"thread": map[string]any{"id": "foreign-thread", "status": map[string]any{"type": "idle"}, "turns": []any{}}})
			case "lostmissing":
				respond(map[string]any{"thread": map[string]any{"status": map[string]any{"type": "idle"}, "turns": []any{}}})
			default:
				respond(map[string]any{"thread": map[string]any{"id": "thread-1", "status": map[string]any{"type": "idle"}, "turns": []any{}}})
			}
		}
	}
}

func fakeAppServerExecutable(t *testing.T, mode, capture string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nSLOPMOCHI_FAKE_MODE=" + shellQuote(mode) + " SLOPMOCHI_FAKE_CAPTURE=" + shellQuote(capture) + " exec " + shellQuote(os.Args[0]) + " -test.run=TestCodexAppServerHelper -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendCapture(path string, line []byte) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(append(line, '\n'))
	_ = file.Close()
}

type capturedMessage struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func capturedMessages(t *testing.T, path string) []capturedMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result []capturedMessage
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var message capturedMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatal(err)
		}
		result = append(result, message)
	}
	return result
}

func capturedMethods(t *testing.T, path string) []string {
	messages := capturedMessages(t, path)
	result := make([]string, 0, len(messages))
	for _, message := range messages {
		result = append(result, message.Method)
	}
	return result
}

func waitForCapturedMethod(t *testing.T, path, method string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), `"method":"`+method+`"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("method %q was not captured", method)
}

func startFakeDescendant(capture string) {
	command := exec.Command("sh", "-c", `trap 'exit 0' TERM; printf '%s' "$$" > "$SLOPMOCHI_CHILD_PID"; printf ready > "$SLOPMOCHI_CHILD_READY"; while :; do sleep 1; done`)
	command.Env = append(os.Environ(), "SLOPMOCHI_CHILD_READY="+capture+".ready", "SLOPMOCHI_CHILD_PID="+capture+".pid")
	if command.Start() != nil {
		return
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(capture + ".ready"); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startTermIgnoringDescendant(capture string) {
	command := exec.Command("sh", "-c", `trap '' TERM; printf '%s' "$$" > "$SLOPMOCHI_CHILD_PID"; while :; do printf x >> "$SLOPMOCHI_CHILD_WRITES"; sleep 0.01; done`)
	command.Env = append(os.Environ(), "SLOPMOCHI_CHILD_PID="+capture+".pid", "SLOPMOCHI_CHILD_WRITES="+capture+".writes")
	if command.Start() != nil {
		return
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(capture + ".pid"); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func findCaptured(t *testing.T, values []capturedMessage, method string) capturedMessage {
	t.Helper()
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].Method == method {
			return values[i]
		}
	}
	t.Fatalf("method %q absent from %#v", method, values)
	return capturedMessage{}
}

func testProfile(executable string) agent.Profile {
	return agent.Profile{ID: "codex", Runtime: RuntimeKind, Executable: executable, AuthenticationRef: "provider-owned", Options: map[string]string{"termination_grace": "200ms"}}
}

func testRequest(candidate, common string) agent.Request {
	return agent.Request{
		JobID: "job-1", AttemptID: "attempt-1", Model: "gpt-5.6-sol", Effort: "high",
		Workspace: fix.CandidateIdentity{RepositoryRoot: candidate, GitCommonDir: common},
		Task:      agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "trusted envelope", Objective: "fix main.go"}},
		Limits:    agent.Limits{MaxOutputBytes: 1 << 20, MaxActors: 10},
	}
}

func canonicalTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func stringField(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func mergeMap(left, right map[string]any) map[string]any {
	result := make(map[string]any, len(left)+len(right))
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}

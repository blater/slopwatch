package codexcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

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
	client := newRequestTestClient()
	client.outbound = make(chan outboundMessage, 1)
	client.maximum = 1 << 20
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

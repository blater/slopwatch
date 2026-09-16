package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

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

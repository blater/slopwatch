package openairesponses

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

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

func TestExecutionDistinguishesAccessAndRateLimitFailures(t *testing.T) {
	t.Parallel()
	root := canonicalTempDir(t)
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		want       string
	}{
		{name: "account access", status: http.StatusForbidden, want: "request was forbidden"},
		{name: "rate limit", status: http.StatusTooManyRequests, retryAfter: "90", want: "retry after 1m30s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				response := testHTTPResponse(request, test.status, `{}`)
				if test.retryAfter != "" {
					response.Header.Set("Retry-After", test.retryAfter)
				}
				return response, nil
			})
			strategy := newTestStrategy(t, transport, Config{})
			result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
			if result.Failure != agent.FailureProvider || !strings.Contains(result.Diagnostic, test.want) {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

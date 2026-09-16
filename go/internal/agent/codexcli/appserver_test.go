package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRequestResponseBeforeEOF(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     string
		maximum   int64
		wantValue string
		wantError string
		provider  bool
		decode    bool
	}{
		{name: "result", input: `{"id":1,"result":{"value":"received"}}`, wantValue: "received"},
		{name: "provider error", input: `{"id":1,"error":{"code":-32602,"message":"invalid turn"}}`, wantError: "Codex App Server error -32602: invalid turn", provider: true},
		{name: "invalid result", input: `{"id":1,"result":{"value":42}}`, wantError: "decode turn/start response:", decode: true},
		{name: "null result", input: `{"id":1,"result":null}`},
		{name: "empty result", input: `{"id":1}`},
		{name: "missing response", wantError: "Codex App Server exited before responding: test server diagnostic; install or update Codex and run Test again"},
		{name: "unmatched response", input: `{"id":2,"result":{}}`, wantError: "Codex App Server exited before responding:"},
		{name: "malformed protocol", input: `{`, wantError: "decode Codex App Server message:"},
		{name: "frame budget", input: strings.Repeat("x", 64), maximum: 32, wantError: "read Codex App Server output within the configured 32-byte budget:"},
		{name: "cumulative budget", input: "{}\n{}\n{}", maximum: 8, wantError: "Codex App Server output exceeded the configured 8-byte budget"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, directExit := range []bool{false, true} {
				name := "request"
				if directExit {
					name = "exit branch"
				}
				t.Run(name, func(t *testing.T) {
					client := newRequestTestClient()
					client.maximum = test.maximum
					_, _ = client.stderr.Write([]byte("test server diagnostic"))
					var destination struct{ Value string }
					var err error
					if directExit {
						// Run the real reader first to force the EOF resolution path
						// without depending on which ready select case Request picks.
						pending := make(chan rpcResponse, 1)
						client.pending[1] = pending
						client.read(strings.NewReader(test.input))
						err = client.responseAfterExit(pending, "turn/start", &destination)
					} else {
						ctx, cancel := context.WithTimeout(t.Context(), time.Second)
						defer cancel()
						joined := make(chan struct{})
						go func() {
							defer close(joined)
							select {
							case outbound := <-client.outbound:
								// Request remains in write until the reader has queued
								// the outcome and closed done, so both are ready.
								client.read(strings.NewReader(test.input))
								outbound.result <- nil
							case <-ctx.Done():
							}
						}()
						err = client.Request(ctx, "turn/start", nil, &destination)
						joinRequestHelper(t, joined)
					}
					if test.wantError == "" {
						if err != nil || destination.Value != test.wantValue {
							t.Fatalf("response = %q, %v; want %q, nil", destination.Value, err, test.wantValue)
						}
					} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
						t.Fatalf("error = %v; want %q", err, test.wantError)
					}
					if test.provider {
						var provider *rpcError
						if !errors.As(err, &provider) || provider.Code != -32602 || provider.Message != "invalid turn" {
							t.Fatalf("provider error not preserved: %v", err)
						}
					}
					if test.decode {
						var decode *json.UnmarshalTypeError
						if !errors.As(err, &decode) {
							t.Fatalf("decode error not preserved: %v", err)
						}
					}
					assertNoPendingRequests(t, client)
				})
			}
		})
	}
}

func TestRequestExitWithoutQueuedOutcome(t *testing.T) {
	client := newRequestTestClient()
	client.read(strings.NewReader(""))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case outbound := <-client.outbound:
			outbound.result <- nil
		case <-ctx.Done():
		}
	}()
	err := client.Request(ctx, "turn/start", nil, nil)
	joinRequestHelper(t, joined)
	if err == nil || !strings.Contains(err.Error(), "Codex App Server exited before responding;") {
		t.Fatalf("Request() = %v", err)
	}
	assertNoPendingRequests(t, client)
}

func TestRequestCancellationRemovesPending(t *testing.T) {
	client := newRequestTestClient()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case outbound := <-client.outbound:
			// Acknowledge the write and cancel with no response available.
			outbound.result <- nil
			cancel()
		case <-ctx.Done():
		}
	}()
	err := client.Request(ctx, "turn/start", nil, nil)
	joinRequestHelper(t, joined)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Request() = %v; want context.Canceled", err)
	}
	assertNoPendingRequests(t, client)
}

func newRequestTestClient() *appServerClient {
	return &appServerClient{
		pending: make(map[int64]chan rpcResponse), outbound: make(chan outboundMessage),
		stop: make(chan struct{}), done: make(chan struct{}),
		stderr: boundedText{maximum: diagnosticCaptureLimit},
	}
}

func joinRequestHelper(t *testing.T, joined <-chan struct{}) {
	t.Helper()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("request helper did not finish")
	}
}

func assertNoPendingRequests(t *testing.T, client *appServerClient) {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.pending) != 0 {
		t.Fatalf("request left %d pending entries", len(client.pending))
	}
}

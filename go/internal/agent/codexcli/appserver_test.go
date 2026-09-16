package codexcli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
	protocol := &appServerProtocol{
		appServerResponse: &appServerResponse{pending: make(map[int64]chan rpcResponse), done: make(chan struct{}), stderr: boundedText{maximum: diagnosticCaptureLimit}},
		appServerWriter:   &appServerWriter{outbound: make(chan outboundMessage), stop: make(chan struct{}), writerDone: make(chan struct{})},
	}
	protocol.appServerReader = &appServerReader{responses: protocol.appServerResponse, writer: protocol.appServerWriter}
	protocol.appServerResponse.writer = protocol.appServerWriter
	return &appServerClient{appServerProtocol: protocol, appServerLifecycle: &appServerLifecycle{protocol: protocol}}
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

package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type responseCase struct {
	name      string
	input     string
	maximum   int64
	wantValue string
	wantError string
	provider  bool
	decode    bool
}

func TestRequestResponseBeforeEOF(t *testing.T) {
	for _, test := range responseCases() {
		t.Run(test.name, func(t *testing.T) {
			for _, directExit := range []bool{false, true} {
				name := "request"
				if directExit {
					name = "exit branch"
				}
				t.Run(name, func(t *testing.T) { runResponseCase(t, test, directExit) })
			}
		})
	}
}

func responseCases() []responseCase {
	return []responseCase{
		{name: "result", input: `{"id":1,"result":{"value":"received"}}`, wantValue: "received"},
		{name: "provider error", input: `{"id":1,"error":{"code":-32602,"message":"invalid turn"}}`, wantError: "Codex App Server error -32602: invalid turn", provider: true},
		{name: "invalid result", input: `{"id":1,"result":{"value":42}}`, wantError: "decode turn/start response:", decode: true},
		{name: "null result", input: `{"id":1,"result":null}`}, {name: "empty result", input: `{"id":1}`},
		{name: "missing response", wantError: "Codex App Server exited before responding: test server diagnostic; install or update Codex and run Test again"},
		{name: "unmatched response", input: `{"id":2,"result":{}}`, wantError: "Codex App Server exited before responding:"},
		{name: "malformed protocol", input: `{`, wantError: "decode Codex App Server message:"},
		{name: "frame budget", input: strings.Repeat("x", 64), maximum: 32, wantError: "read Codex App Server output within the configured 32-byte budget:"},
		{name: "cumulative budget", input: "{}\n{}\n{}", maximum: 8, wantError: "Codex App Server output exceeded the configured 8-byte budget"},
	}
}

func runResponseCase(t *testing.T, test responseCase, directExit bool) {
	client := newRequestTestClient()
	client.maximum = test.maximum
	_, _ = client.stderr.Write([]byte("test server diagnostic"))
	var destination struct{ Value string }
	err := responseRequest(t, client, test.input, directExit, &destination)
	assertResponse(t, test, destination.Value, err)
	assertNoPendingRequests(t, client)
}

func responseRequest(t *testing.T, client *appServerClient, input string, directExit bool, destination any) error {
	if directExit {
		pending := make(chan rpcResponse, 1)
		client.pending[1] = pending
		client.read(strings.NewReader(input))
		return client.responseAfterExit(pending, "turn/start", destination)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case outbound := <-client.outbound:
			client.read(strings.NewReader(input))
			outbound.result <- nil
		case <-ctx.Done():
		}
	}()
	err := client.Request(ctx, "turn/start", nil, destination)
	joinRequestHelper(t, joined)
	return err
}

func assertResponse(t *testing.T, test responseCase, value string, err error) {
	t.Helper()
	if test.wantError == "" {
		if err != nil || value != test.wantValue {
			t.Fatalf("response = %q, %v; want %q, nil", value, err, test.wantValue)
		}
	} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
		t.Fatalf("error = %v; want %q", err, test.wantError)
	}
	if test.provider {
		assertProviderError(t, err)
	}
	if test.decode {
		var decode *json.UnmarshalTypeError
		if !errors.As(err, &decode) {
			t.Fatalf("decode error not preserved: %v", err)
		}
	}
}

func assertProviderError(t *testing.T, err error) {
	var provider *rpcError
	if !errors.As(err, &provider) || provider.Code != -32602 || provider.Message != "invalid turn" {
		t.Fatalf("provider error not preserved: %v", err)
	}
}

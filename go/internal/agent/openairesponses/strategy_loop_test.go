package openairesponses

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

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
	assertLoopResult(t, result, root, provider)
	assertLoopRequests(t, provider)
	assertLoopEvents(t, events)
}

func assertLoopResult(t *testing.T, result agent.Result, root string, provider *scriptedProvider) {
	t.Helper()
	assertLoopStatus(t, result)
	assertLoopUsage(t, result)
	assertLoopContents(t, root)
	assertLoopRequestCount(t, provider)
}

func assertLoopStatus(t *testing.T, result agent.Result) {
	t.Helper()
	if result.Status != agent.ResultCompleted || result.Failure != agent.FailureNone || result.Summary != "Refactored main.go and preserved behavior." {
		t.Fatalf("Execute() = %#v", result)
	}
}

func assertLoopUsage(t *testing.T, result agent.Result) {
	t.Helper()
	if result.Usage.InputTokens != 60 || result.Usage.OutputTokens != 15 || result.Usage.ReasoningTokens != 2 || !result.Usage.Cumulative {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func assertLoopContents(t *testing.T, root string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || string(contents) != "package main\n\nfunc improved() {}\n" {
		t.Fatalf("candidate contents=%q err=%v", contents, err)
	}
}

func assertLoopRequestCount(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	if len(provider.requests) != 3 {
		t.Fatalf("provider requests=%d", len(provider.requests))
	}
}

func assertLoopRequests(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	assertLoopPrompt(t, provider)
	assertLoopBoundedRequests(t, provider)
	assertLoopAuthHeaders(t, provider)
	assertLoopReadOutput(t, provider)
	assertLoopWriteOutput(t, provider)
}

func assertLoopPrompt(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	firstRequest := string(provider.requests[0])
	if !strings.Contains(firstRequest, "Trusted remediation envelope\\n\\nImprove main.go") || strings.Contains(firstRequest, "Apply the requested remediation") {
		t.Fatalf("provider did not receive only the configured prompt: %s", provider.requests[0])
	}
}

func assertLoopBoundedRequests(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	for index, body := range provider.requests {
		if bytesContainSecret(body) {
			t.Fatalf("request %d leaked authentication: %s", index, body)
		}
		if strings.Contains(string(body), "previous_response_id") || !strings.Contains(string(body), `"store":false`) || !strings.Contains(string(body), `"parallel_tool_calls":false`) {
			t.Fatalf("request %d does not use bounded local history: %s", index, body)
		}
	}
}

func assertLoopAuthHeaders(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	for _, header := range provider.authHeaders {
		if header != "Bearer "+testSecret {
			t.Fatalf("authorization header=%q", header)
		}
	}
}

func assertLoopReadOutput(t *testing.T, provider *scriptedProvider) {
	t.Helper()
	if !strings.Contains(string(provider.requests[1]), `"type":"function_call_output"`) || !strings.Contains(string(provider.requests[1]), "func old") {
		t.Fatalf("read result was not returned through controlled tool output: %s", provider.requests[1])
	}
}

func assertLoopWriteOutput(t *testing.T, provider *scriptedProvider) {
	t.Helper()
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
}

func assertLoopEvents(t *testing.T, events []agent.Event) {
	t.Helper()
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

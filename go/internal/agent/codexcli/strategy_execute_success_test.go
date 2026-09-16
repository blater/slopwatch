package codexcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

func TestExecuteStreamsEventsAndUsesWorkspaceWrite(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate, staging := successCandidateDirs(t, root)
	capture := filepath.Join(root, "capture.jsonl")
	request := successRequest(candidate, common, staging)
	strategy := New()
	strategy.workingDir = func() (string, error) { return root, nil }
	var events []agent.Event
	result := strategy.Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "complete", capture)), request, agent.EventSinkFunc(func(event agent.Event) error { events = append(events, event); return nil }))
	assertSuccessResult(t, result, events)
	assertSuccessRequests(t, capture, candidate, staging)
}

func successCandidateDirs(t *testing.T, root string) (string, string, string) {
	t.Helper()
	common, candidate, staging := filepath.Join(root, "common.git"), filepath.Join(root, "candidate"), filepath.Join(root, "staging")
	for _, path := range []string{common, candidate, staging} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "targets.txt"), []byte("main.go\\nother.go\\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return common, candidate, staging
}

func successRequest(candidate, common, staging string) agent.Request {
	request := testRequest(candidate, common)
	request.Workspace.StagingRoot = staging
	request.Task.Instructions.Objective += "\\nManifest {target_manifest}; files {target_manifest_count}."
	request.Task.Manifest = &agent.TargetManifest{Path: filepath.Join(staging, "targets.txt"), Count: 2}
	return request
}

func assertSuccessResult(t *testing.T, result agent.Result, events []agent.Event) {
	t.Helper()
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
}

func assertSuccessRequests(t *testing.T, capture, candidate, staging string) {
	t.Helper()
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
	thread, turn := findCaptured(t, requests, "thread/start"), findCaptured(t, requests, "turn/start")
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
	if !strings.Contains(stringField(first, "text"), "Manifest "+filepath.Join(staging, "targets.txt")+"; files 2.") {
		t.Fatalf("turn input = %#v", input)
	}
}

package codexcli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func serveFakeAppServer(mode, capture string) {
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fake app-server startup failure")
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request fakeRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		appendCapture(capture, scanner.Bytes())
		if !handleFakeRequest(mode, capture, request) {
			return
		}
	}
}

func fakeAppServerExecutable(t *testing.T, mode, capture string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nSLOPWATCH_FAKE_MODE=" + shellQuote(mode) + " SLOPWATCH_FAKE_CAPTURE=" + shellQuote(capture) + " exec " + shellQuote(os.Args[0]) + " -test.run=TestCodexAppServerHelper -- \"$@\"\n"
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
	command := exec.Command("sh", "-c", `trap 'exit 0' TERM; printf '%s' "$$" > "$SLOPWATCH_CHILD_PID"; printf ready > "$SLOPWATCH_CHILD_READY"; while :; do sleep 1; done`)
	command.Env = append(os.Environ(), "SLOPWATCH_CHILD_READY="+capture+".ready", "SLOPWATCH_CHILD_PID="+capture+".pid")
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
	command := exec.Command("sh", "-c", `trap '' TERM; printf '%s' "$$" > "$SLOPWATCH_CHILD_PID"; while :; do printf x >> "$SLOPWATCH_CHILD_WRITES"; sleep 0.01; done`)
	command.Env = append(os.Environ(), "SLOPWATCH_CHILD_PID="+capture+".pid", "SLOPWATCH_CHILD_WRITES="+capture+".writes")
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

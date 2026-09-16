package openairesponses

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

func TestTargetManifestIsAvailableThroughDedicatedTool(t *testing.T) {
	root := canonicalTempDir(t)
	request := testRequest(t, root)
	directory := filepath.Join(request.Workspace.StagingRoot, "agent-input")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "targets.txt")
	contents := "main.go\nother.go\n"
	if err := os.WriteFile(manifestPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	request.Task.Manifest = &agent.TargetManifest{Path: manifestPath, Count: 2}
	provider := &scriptedProvider{t: t, responses: []string{
		functionResponse("call-manifest", "read_target_manifest", `{}`, 1, 1),
		messageResponse("Updated both targets.", 1, 1, 0),
	}}
	result := newTestStrategy(t, provider, Config{}).Execute(t.Context(), testProfile(), request, nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	if len(provider.requests) != 2 || !strings.Contains(string(provider.requests[0]), `"name":"read_target_manifest"`) {
		t.Fatalf("manifest tool was not offered: %s", provider.requests[0])
	}
	var sent apiRequest
	if err := json.Unmarshal(provider.requests[1], &sent); err != nil {
		t.Fatal(err)
	}
	returned := ""
	for _, item := range sent.Input {
		var output struct {
			Type   string `json:"type"`
			Output string `json:"output"`
		}
		if json.Unmarshal(item, &output) == nil && output.Type == "function_call_output" {
			var result readResult
			if json.Unmarshal([]byte(output.Output), &result) == nil {
				returned = result.Content
			}
		}
	}
	if returned != contents {
		t.Fatalf("manifest contents returned to the agent = %q", returned)
	}
}

func TestToolBrokerRejectsTraversalAndDisallowedWrites(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments string
		code      string
	}{
		{name: "traversal", arguments: `{"path":"../outside.txt","content":"owned"}`, code: "invalid_path"},
		{name: "absolute", arguments: `{"path":"/tmp/outside.txt","content":"owned"}`, code: "invalid_path"},
		{name: "git", arguments: `{"path":".git/config","content":"owned"}`, code: "invalid_path"},
		{name: "disallowed", arguments: `{"path":"other.go","content":"owned"}`, code: "write_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			outside := filepath.Join(filepath.Dir(root), "outside.txt")
			_ = os.Remove(outside)
			provider := &scriptedProvider{t: t, responses: []string{
				functionResponse("call-write", "write_file", test.arguments, 1, 1),
				messageResponse("Handled the rejected operation.", 1, 1, 0),
			}}
			strategy := newTestStrategy(t, provider, Config{})
			result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
			if result.Status != agent.ResultCompleted {
				t.Fatalf("Execute() = %#v", result)
			}
			if len(provider.requests) != 2 || !strings.Contains(string(provider.requests[1]), test.code) {
				t.Fatalf("controlled error missing: %s", provider.requests[len(provider.requests)-1])
			}
			if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside path was touched: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "other.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disallowed path was touched: %v", err)
			}
		})
	}
}

package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func newTestStrategy(t *testing.T, transport http.RoundTripper, overrides Config) *Strategy {
	t.Helper()
	overrides.Endpoint = testEndpoint
	overrides.Client = &http.Client{Transport: transport}
	overrides.Models = []agent.Option[agent.ModelID]{{ID: "gpt-test", Label: "GPT Test", Default: true}}
	overrides.Efforts = []agent.Option[agent.EffortID]{{ID: "high", Label: "High", Default: true}}
	strategy, err := New(overrides, SecretResolverFunc(func(_ context.Context, reference string) (string, error) {
		if reference != "env:OPENAI_API_KEY" {
			return "", errors.New("unknown reference containing " + testSecret)
		}
		return testSecret, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return strategy
}

func testProfile() agent.Profile {
	return agent.Profile{
		ID: "gpt", Label: "GPT", Runtime: RuntimeKind, AuthenticationRef: "env:OPENAI_API_KEY", Fingerprint: "profile-fingerprint",
	}
}

func testRequest(t *testing.T, root string) agent.Request {
	t.Helper()
	staging := filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+"-staging")
	if err := os.Mkdir(staging, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	allowed, err := fix.ParseRepoPath("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return agent.Request{
		JobID: "job-test", AttemptID: "attempt-test", Workspace: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: root, StagingRoot: staging},
		Task:  agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "Trusted remediation envelope", Objective: "Improve main.go"}},
		Model: "gpt-test", Effort: "high",
		Write:  agent.WritePolicy{Allowed: []fix.RepoPath{allowed}, Scope: "targets"},
		Limits: agent.Limits{MaxOutputBytes: 4 << 20, MaxEvents: 200},
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func functionResponse(callID, name, arguments string, input, output int64) string {
	encodedArguments, _ := json.Marshal(arguments)
	return fmt.Sprintf(`{"id":"response","status":"completed","output":[{"type":"function_call","id":"item","status":"completed","call_id":%q,"name":%q,"arguments":%s}],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":%d}}`, callID, name, encodedArguments, input, output, input+output)
}

func messageResponse(text string, input, output, reasoning int64) string {
	encodedText, _ := json.Marshal(text)
	return fmt.Sprintf(`{"id":"response","status":"completed","output":[{"type":"message","id":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":%s,"annotations":[],"logprobs":[]}]}],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":%d},"total_tokens":%d}}`, encodedText, input, output, reasoning, input+output)
}

func bytesContainSecret(value []byte) bool { return strings.Contains(string(value), testSecret) }

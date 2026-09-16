package codexcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

func TestProfileDescriptorKeepsOperationalDetailsInPreferences(t *testing.T) {
	descriptor := New().ProfileDescriptor()
	if descriptor.Runtime != RuntimeKind || descriptor.Label != "Codex" || descriptor.DocumentationURL != "https://developers.openai.com/codex/auth" || !strings.Contains(descriptor.ConnectionInstructions, "codex login") {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	for _, field := range descriptor.Fields {
		if !field.PreferencesOnly {
			t.Fatalf("operational field is visible in the Agents dialog: %#v", field)
		}
	}
}

func TestProbeUsesAppServerAccountAndModelCatalog(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	strategy := New()
	strategy.workingDir = func() (string, error) { return t.TempDir(), nil }
	result := strategy.Probe(t.Context(), testProfile(fakeAppServerExecutable(t, "complete", capture)))
	if result.State != agent.ProbeReady || result.Version != "0.149.1" {
		t.Fatalf("Probe() = %#v", result)
	}
	if result.Authentication.Method != "chatgpt" || !strings.Contains(result.Authentication.Label, "Plus") {
		t.Fatalf("authentication = %#v", result.Authentication)
	}
	if len(result.Capabilities.Models) != 1 || result.Capabilities.Models[0].ID != "gpt-5.6-sol" || len(result.Capabilities.Efforts) != 2 {
		t.Fatalf("capabilities = %#v", result.Capabilities)
	}
	isolation := result.Capabilities.Isolation
	if !isolation.ProviderManagedCancellation || isolation.CrashContainment || isolation.SensitiveReadsDenied || isolation.TransportAuthIsolated || !isolation.EligibleForMutation() {
		t.Fatalf("App Server lifecycle was misrepresented: %#v", isolation)
	}
	methods := capturedMethods(t, capture)
	for _, wanted := range []string{"initialize", "initialized", "account/read", "model/list"} {
		if !containsString(methods, wanted) {
			t.Fatalf("missing %q in %v", wanted, methods)
		}
	}
}

func TestExecutePrefersCompletionWhenServerExitsImmediatelyAfterIt(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := New().Execute(ctx, testProfile(fakeAppServerExecutable(t, "completeexit", filepath.Join(root, "capture"))), testRequest(candidate, common), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestZeroOutputBudgetAllowsUnlimitedBoundedProtocolFrames(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	request.Limits.MaxOutputBytes = 0
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "cumulative", filepath.Join(root, "capture"))), request, nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("unlimited bounded frames failed: %#v", result)
	}
}

func TestZeroOutputBudgetStillRejectsOversizedProtocolFrame(t *testing.T) {
	root := canonicalTestRoot(t)
	common, candidate := filepath.Join(root, "common.git"), filepath.Join(root, "candidate")
	for _, path := range []string{common, candidate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := testRequest(candidate, common)
	request.Limits.MaxOutputBytes = 0
	result := New().Execute(t.Context(), testProfile(fakeAppServerExecutable(t, "oversizeframe", filepath.Join(root, "capture"))), request, nil)
	if result.Status == agent.ResultCompleted || !strings.Contains(result.Diagnostic, "token too long") {
		t.Fatalf("oversized protocol frame was accepted: %#v", result)
	}
}

func TestCodexAppServerHelper(t *testing.T) {
	if !containsString(os.Args, "app-server") {
		return
	}
	if containsString(os.Args, "--disable") || containsString(os.Args, "-c") {
		fmt.Fprintln(os.Stderr, "unexpected App Server launch constraint")
		os.Exit(2)
	}
	serveFakeAppServer(os.Getenv("SLOPWATCH_FAKE_MODE"), os.Getenv("SLOPWATCH_FAKE_CAPTURE"))
	os.Exit(0)
}

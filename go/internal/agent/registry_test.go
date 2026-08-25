package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testStrategy struct{}

func (testStrategy) ProfileDescriptor() ProfileDescriptor {
	return ProfileDescriptor{Runtime: "codex-cli", Label: "Test"}
}
func (testStrategy) ValidateProfile(Profile) error { return nil }

func (testStrategy) Probe(_ context.Context, profile Profile) ProbeResult {
	return ProbeResult{Runtime: profile.Runtime, State: ProbeReady, Diagnostic: "test route"}
}
func (testStrategy) Execute(context.Context, Profile, Request, EventSink) Result {
	return Result{Status: ResultCompleted}
}

func TestRegistryRejectsDuplicateAndUnknownKinds(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registry.Register("codex-cli", testStrategy{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("codex-cli", testStrategy{}); err == nil {
		t.Fatal("duplicate registration succeeded")
	}
	if _, err := registry.Strategy("claude-cli"); !errors.Is(err, ErrUnknownRuntime) {
		t.Fatalf("unknown runtime error = %v", err)
	}
	if got := registry.Kinds(); len(got) != 1 || got[0] != "codex-cli" {
		t.Fatalf("Kinds() = %v", got)
	}
	probe := registry.Probe(context.Background(), Profile{Runtime: "codex-cli"})
	if probe.State != ProbeReady || probe.Runtime != "codex-cli" || probe.Diagnostic != "test route" {
		t.Fatalf("Probe() = %#v", probe)
	}
	unknown := registry.Probe(context.Background(), Profile{Runtime: "claude-cli"})
	if unknown.State != ProbeUnavailable || !strings.Contains(unknown.Diagnostic, ErrUnknownRuntime.Error()) {
		t.Fatalf("unknown Probe() = %#v", unknown)
	}
}

func TestRuntimeIsolationEligibility(t *testing.T) {
	t.Parallel()
	value := RuntimeIsolation{
		Writes:               CandidateTreeAndGitMetadataProtected,
		SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: true,
	}
	if !value.EligibleForMutation() {
		t.Fatal("complete isolation was not eligible")
	}
	value.CrashContainment = false
	if value.EligibleForMutation() {
		t.Fatal("missing crash containment was eligible")
	}
}

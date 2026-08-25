package isolation

import (
	"context"
	"strings"
	"testing"
)

func TestConformanceRequiresEveryGate(t *testing.T) {
	t.Parallel()
	value := Conformance{
		CandidateWrite: true, OutsideWriteDenied: true, GitMetadataDenied: true,
		SensitiveReadsDenied: true, ToolNetworkPolicy: true, TransportAuth: true,
		CrashContainment: true,
	}
	if !value.MutationEligible() {
		t.Fatal("complete conformance was not mutation eligible")
	}
	value.GitMetadataDenied = false
	if value.MutationEligible() {
		t.Fatal("incomplete conformance was mutation eligible")
	}
	failed := value.FailedGates()
	if len(failed) != 1 || failed[0] != GateGitMetadataDenied {
		t.Fatalf("FailedGates() = %v", failed)
	}
}

func TestDenyAllCheckerFailsClosed(t *testing.T) {
	t.Parallel()
	result := (DenyAllChecker{}).Check(t.Context(), ConformanceRequest{})
	if result.MutationEligible() || result.Diagnostic == "" {
		t.Fatalf("DenyAllChecker result = %#v", result)
	}
}

func TestProductionConfinementSelectorFailsClosedWithoutVerifiedBackend(t *testing.T) {
	confinement := SelectCandidateConfinement(Runner{}, ConfinementOptions{})
	capability := confinement.Capability(context.Background())
	if capability.Available || capability.CrashContainment || capability.Diagnostic == "" {
		t.Fatalf("Capability() = %#v", capability)
	}
	_, observed, err := confinement.RunCandidate(context.Background(), CandidatePolicy{}, Request{})
	if err == nil || observed.MutationEligible() || !strings.Contains(err.Error(), "confinement") {
		t.Fatalf("RunCandidate() conformance=%#v err=%v", observed, err)
	}
}

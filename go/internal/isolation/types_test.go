package isolation

import "testing"

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

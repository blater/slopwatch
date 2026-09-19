package native

import (
	"context"
	"testing"
)

func TestPersistentCacheRetriesTerminalFailureWithoutSourceEdit(t *testing.T) {
	_, analyzer := cacheHitFixture(t)
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		calls++
		if calls == 1 {
			return terminalHelperFailureInputs(request)
		}
		return fakeBatchInputs(t, request), nil
	}
	first := analyzeTestDocument(t, analyzer)
	if first.Files[0].Complete {
		t.Fatal("helper failure reported complete")
	}
	requireRecoveryDiagnostic(t, first.Diagnostics, "native.terminal_failure")
	recovered := analyzeTestDocument(t, analyzer)
	if calls != 2 || !recovered.Files[0].Complete {
		t.Fatalf("unchanged-source retry calls=%d complete=%v", calls, recovered.Files[0].Complete)
	}
	_ = analyzeTestDocument(t, analyzer)
	if calls != 2 {
		t.Fatalf("successful retry was not cached: calls=%d", calls)
	}
}

func terminalHelperFailureInputs(request analyzerRequest) (map[string]scoreInputs, error) {
	units := make(map[string]scoreInputs, len(request.Units))
	for _, unit := range request.Units {
		inputs := newScoreInputs()
		for _, path := range unit.Paths {
			inputs.languages[path] = unit.Language
			inputs.coverage[path] = failedComponents(request.Components)
			inputs.diagnostics = append(inputs.diagnostics, map[string]any{
				"path": path, "code": "STRUCTURAL_ANALYZER", "severity": "error", "message": "helper killed by OS",
			})
		}
		units[unit.ID] = inputs
	}
	return units, &analyzerTerminalFailure{status: "failure", message: "one structural unit failed"}
}

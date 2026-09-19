package depth

import (
	"fmt"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestResourceSummaryLimitCannotBecomeCompleteAfterCaching(t *testing.T) {
	var functions []facts.FlowFunction
	var calls []facts.Instruction
	for index := 0; index <= maxResourceWitnesses; index++ {
		id := fmt.Sprintf("helper%d", index)
		acquire, cleanup := resourceAcquire(), resourceCleanup()
		acquire.Roots[0].ID, cleanup.Roots[0].ID = id, id
		functions = append(functions, resourceSummaryFunction(id, acquire, cleanup, controlConstant("value", "7"), controlReturn("return", "value")))
		calls = append(calls, summaryCall(id, id, "arg0"))
	}
	calls = append(calls, controlReturn("return", calls[len(calls)-1].ID))
	functions = append(functions, resourceSummaryFunction("outer", calls...))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "limits", Functions: functions})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	for attempt := 0; attempt < 2; attempt++ {
		result := summaries.evaluate("outer")
		if result.Status == TransferOK {
			t.Fatalf("attempt %d: truncated resource summary became complete", attempt)
		}
		found := false
		for _, gap := range result.Gaps {
			found = found || gap.Reason == "resource_witness_limit"
		}
		if !found {
			t.Fatalf("attempt %d: resource limit reason lost: %+v", attempt, result.Gaps)
		}
	}
	proof := proveFunctionOutcomes(summaries, "outer", facts.BoundaryIdentity{Artifact: "limits", Symbol: "outer"})
	if len(proof.reasons) == 0 {
		t.Fatal("truncated responsibility set was accepted as a numeric measurement")
	}
}

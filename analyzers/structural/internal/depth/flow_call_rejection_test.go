package depth

import (
	"context"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func rejectionSummaryProbe(t *testing.T, literal string, options TransferOptions) (*functionSummaries, FlowEvaluation) {
	t.Helper()
	helper := facts.FlowFunction{ID: "check", Entry: "entry", Formals: []facts.Formal{{ID: "arg0", Type: "bool", ValueKind: facts.FlowKindBoolean}}, Blocks: []facts.FlowBlock{
		{ID: "entry", Edges: []facts.FlowEdge{{From: "entry", To: "reject", Kind: facts.EdgeTrue, Guard: "arg0", GuardPolarity: "true"}, {From: "entry", To: "accept", Kind: facts.EdgeFalse, Guard: "arg0", GuardPolarity: "false"}}},
		{ID: "reject", Instructions: []facts.Instruction{controlConstant("error", "1"), {ID: "throw", Opcode: facts.OpThrow, Operands: []string{"error"}}}},
		{ID: "accept", Instructions: []facts.Instruction{controlReturn("return")}},
	}}
	call := summaryCall("call", "check", "test")
	call.Type = "void"
	call.Results = nil
	root := summaryFixture("root", facts.Instruction{ID: "test", Opcode: facts.OpConstant, Type: "bool", ValueKind: facts.FlowKindBoolean, Results: []string{"test"}, Value: &facts.Value{Type: "bool", ValueKind: facts.FlowKindBoolean, Constant: literal}}, call,
		facts.Instruction{ID: "unreachable", Opcode: facts.OpUnknown, Type: "int", Results: []string{"bad"}}, controlReturn("return", "arg0"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{root, helper}})
	if err != nil {
		t.Fatal(err)
	}
	session := newFunctionSummaries(NewRecipeArena(), artifact, options)
	return session, session.evaluate("root")
}

func TestRejectionOnlyHelperStopsCallerInstructions(t *testing.T) {
	s, result := rejectionSummaryProbe(t, "true", TransferOptions{})
	if result.Status != TransferOK || len(result.Gaps) != 0 || len(result.Effects) != 0 || len(result.Completions) != 1 || result.Completions[0].Kind != facts.EdgeThrow {
		t.Fatalf("unreachable caller instructions executed: %+v", result)
	}
	if len(result.Dependencies) != 1 || result.Dependencies[0] != "check" {
		t.Fatal(result.Dependencies)
	}
	work := s.budget.state.work
	for limit := 1; limit < work; limit++ {
		limited, got := rejectionSummaryProbe(t, "true", TransferOptions{MaxWork: limit})
		if got.Status == TransferOK || limited.budget.state.work > limit {
			t.Fatalf("budget %d escaped: %+v", limit, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, got := rejectionSummaryProbe(t, "true", TransferOptions{Context: ctx})
	if got.Status == TransferOK {
		t.Fatal("cancelled call succeeded")
	}
	_, accepted := rejectionSummaryProbe(t, "false", TransferOptions{})
	if accepted.Status == TransferOK || len(accepted.Gaps) == 0 {
		t.Fatal("normal caller instructions were skipped")
	}
}

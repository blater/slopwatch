package depth

import (
	"context"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func errorChannelProbe(t *testing.T, options TransferOptions) (*functionSummaries, FlowEvaluation) {
	t.Helper()
	results := []facts.Formal{{ID: "result0", Type: "int", ValueKind: facts.FlowKindNumeric}, {ID: "result1", Type: "error", ValueKind: facts.FlowKindError}}
	helper := summaryFixture("checked")
	helper.Results = results
	helper.Blocks = []facts.FlowBlock{
		{ID: "entry", Instructions: []facts.Instruction{controlConstant("zero", "0"), {ID: "test", Opcode: facts.OpPrimitive, Results: []string{"test"}, Type: "bool", ValueKind: facts.FlowKindBoolean, ArithmeticMode: "integer", Operator: "<", Operands: []string{"arg0", "zero"}}}, Edges: []facts.FlowEdge{
			{From: "entry", To: "fail", Kind: facts.EdgeTrue, Guard: "test", GuardPolarity: "true"}, {From: "entry", To: "ok", Kind: facts.EdgeFalse, Guard: "test", GuardPolarity: "false"},
		}},
		{ID: "fail", Instructions: []facts.Instruction{{ID: "err", Opcode: facts.OpAllocate, Results: []string{"err"}, Type: "error", ValueKind: facts.FlowKindError, Roots: []facts.AliasRoot{{ID: "error-site", Kind: "allocation", Ownership: "owned"}}}, controlReturn("return-error", "arg0", "err")}},
		{ID: "ok", Instructions: []facts.Instruction{{ID: "nil", Opcode: facts.OpConstant, Results: []string{"nil"}, Type: "error", ValueKind: facts.FlowKindError, Value: &facts.Value{Type: "error", ValueKind: facts.FlowKindError, Constant: "nil"}}, controlReturn("return-ok", "arg0", "nil")}},
	}
	call := summaryCall("call", "checked", "arg0")
	call.Results = []string{"value", "error"}
	call.Call.ResultBindings = []facts.Binding{{Formal: "result0", Actual: "value"}, {Formal: "result1", Actual: "error"}}
	root := summaryFixture("root", call, controlReturn("return", "value", "error"))
	root.Results = results
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{helper, root}})
	if err != nil {
		t.Fatal(err)
	}
	s := newFunctionSummaries(NewRecipeArena(), artifact, options)
	return s, s.evaluate("root")
}

func TestErrorChannelEverySharedWorkCutoff(t *testing.T) {
	s, result := errorChannelProbe(t, TransferOptions{})
	if result.Status != TransferOK || len(result.Completions) != 2 {
		t.Fatalf("error correlation lost: %+v", result)
	}
	normal, failed := 0, 0
	for _, completion := range result.Completions {
		if len(completion.Values) != 2 || completion.Values[1].Kind != KindError {
			t.Fatal("physical tuple lost")
		}
		if completion.Kind == facts.EdgeNormal {
			normal++
		}
		if completion.Kind == facts.EdgeReturnError {
			failed++
		}
	}
	if normal != 1 || failed != 1 {
		t.Fatal("error became throw or unconditional normal")
	}
	for limit := 1; limit < s.budget.state.work; limit++ {
		limited, got := errorChannelProbe(t, TransferOptions{MaxWork: limit})
		if got.Status == TransferOK || limited.budget.state.work > limit {
			t.Fatalf("budget %d escaped", limit)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, got := errorChannelProbe(t, TransferOptions{Context: ctx})
	if got.Status == TransferOK {
		t.Fatal("cancelled error channel succeeded")
	}
}

func TestErrorPresenceRequiresTypedEvidence(t *testing.T) {
	a := NewRecipeArena()
	for _, recipe := range []RecipeID{a.Constant("error", "opaque"), mustPack(t, a, "error", []FieldRecipeBinding{{Field: "error.present", Recipe: a.Constant("int", "1")}})} {
		state := NewTransferState()
		state.Seed("error", NewValue(recipe, "error", KindError, nil, NewAliasSet()))
		result := Apply(a, state, facts.Instruction{ID: "present", Opcode: facts.OpErrorPresent, Operands: []string{"error"}, Results: []string{"present"}, Type: "bool", ValueKind: facts.FlowKindBoolean}, TransferOptions{})
		if result.Status == TransferOK {
			t.Fatal("unsupported presence became known")
		}
	}
}

package depth

import (
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func tupleCallProbe(t *testing.T, change func(*facts.FlowFunction, *facts.Instruction), options TransferOptions) (*TransferState, TransferResult) {
	t.Helper()
	function := summaryFixture("pair", controlConstant("one", "1"), controlReturn("return", "arg0", "one"))
	function.Results = []facts.Formal{{ID: "result0", Type: "int", ValueKind: facts.FlowKindNumeric}, {ID: "result1", Type: "int", ValueKind: facts.FlowKindNumeric}}
	call := summaryCall("call", "pair", "x")
	call.Results = []string{"a", "b"}
	call.Call.ResultBindings = []facts.Binding{{Formal: "result0", Actual: "a"}, {Formal: "result1", Actual: "b"}}
	if change != nil {
		change(&function, &call)
	}
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{function}})
	if err != nil {
		t.Fatal(err)
	}
	arena := NewRecipeArena()
	session := newFunctionSummaries(arena, artifact, options)
	state := NewTransferState()
	state.Seed("x", ScalarValue(mustFormal(t, arena, 0, "int"), "int", KindNumeric))
	return state, Apply(arena, state, call, session.options)
}

func TestTupleCallBindingsCommitAtomically(t *testing.T) {
	for _, change := range []func(*facts.FlowFunction, *facts.Instruction){
		func(f *facts.FlowFunction, c *facts.Instruction) {
			f.Results[1].Type = "bool"
			f.Results[1].ValueKind = facts.FlowKindBoolean
		},
		func(f *facts.FlowFunction, c *facts.Instruction) { c.Call.ResultBindings[1].Actual = "a" },
		func(f *facts.FlowFunction, c *facts.Instruction) { c.Call.ResultBindings[1].Formal = "result0" },
		func(f *facts.FlowFunction, c *facts.Instruction) { c.Call.ResultBindings[1].Value = "ambiguous" },
		func(f *facts.FlowFunction, c *facts.Instruction) { c.Call.ResultBindings = c.Call.ResultBindings[:1] },
	} {
		state, result := tupleCallProbe(t, change, TransferOptions{})
		if result.Status == TransferOK {
			t.Fatal("malformed binding accepted")
		}
		for _, id := range []string{"a", "b"} {
			value, ok := state.Value(id)
			if ok && !value.Unknown {
				t.Fatalf("partial tuple leaked %s: %+v", id, value)
			}
		}
	}
	state, result := tupleCallProbe(t, nil, TransferOptions{})
	a, _ := state.Value("a")
	b, _ := state.Value("b")
	if result.Status != TransferOK || a.Recipe == b.Recipe {
		t.Fatalf("tuple positions lost: %+v", result)
	}
	for limit := 1; limit < result.Work; limit++ {
		state, got := tupleCallProbe(t, nil, TransferOptions{MaxWork: limit})
		if got.Status == TransferOK {
			t.Fatalf("cutoff %d accepted tuple", limit)
		}
		if _, ok := state.Value("a"); ok {
			t.Fatal("budget failure committed tuple")
		}
		if _, ok := state.Value("b"); ok {
			t.Fatal("budget failure committed tuple")
		}
	}
}

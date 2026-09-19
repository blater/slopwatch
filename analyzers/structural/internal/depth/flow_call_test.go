package depth

import (
	"context"
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func summaryFixture(id string, instructions ...facts.Instruction) facts.FlowFunction {
	return facts.FlowFunction{ID: id, Entry: "entry", Formals: []facts.Formal{{ID: "arg0", Type: "int", ValueKind: facts.FlowKindNumeric}}, Blocks: []facts.FlowBlock{{ID: "entry", Instructions: instructions}}}
}
func summaryCall(id, target, actual string) facts.Instruction {
	return facts.Instruction{ID: id, Opcode: facts.OpCall, Type: "int", ValueKind: facts.FlowKindNumeric, Results: []string{id}, Operands: []string{actual}, Call: &facts.CallBinding{Targets: []string{target}, Bindings: []facts.Binding{{Formal: "arg0", Actual: actual}}}}
}
func summaryDiamond(t *testing.T, options TransferOptions) (*functionSummaries, FlowEvaluation) {
	t.Helper()
	functions := []facts.FlowFunction{
		summaryFixture("leaf", controlConstant("one", "1"), facts.Instruction{ID: "plus", Opcode: facts.OpPrimitive, Type: "int", ValueKind: facts.FlowKindNumeric, ArithmeticMode: "integer", Operator: "+", Operands: []string{"arg0", "one"}, Results: []string{"next"}}, controlReturn("return", "next")),
		summaryFixture("left", summaryCall("call", "leaf", "arg0"), controlReturn("return", "call")),
		summaryFixture("right", summaryCall("call", "leaf", "arg0"), controlReturn("return", "call")),
		summaryFixture("root", summaryCall("left", "left", "arg0"), summaryCall("right", "right", "arg0"), facts.Instruction{ID: "plus", Opcode: facts.OpPrimitive, Type: "int", ValueKind: facts.FlowKindNumeric, ArithmeticMode: "integer", Operator: "+", Operands: []string{"left", "right"}, Results: []string{"out"}}, controlReturn("return", "out")),
	}
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "a", Functions: functions})
	if err != nil {
		t.Fatal(err)
	}
	session := newFunctionSummaries(NewRecipeArena(), artifact, options)
	return session, session.evaluate("root")
}
func TestSummaryDiamondMemoizationAndSharedBudget(t *testing.T) {
	session, result := summaryDiamond(t, TransferOptions{})
	if result.Status != TransferOK || len(session.results) != 4 || len(result.Completions) != 1 {
		t.Fatalf("bad diamond: %+v", result)
	}
	value := result.Completions[0].Values[0]
	if session.arena.Classify(value.Recipe) != TransformationPrimitive {
		t.Fatal(value)
	}
	work := session.budget.state.work
	again := session.evaluate("root")
	if !reflect.DeepEqual(again, result) || session.budget.state.work != work+1 {
		t.Fatal("memoized body re-evaluated")
	}
	for limit := 1; limit < work; limit++ {
		s, r := summaryDiamond(t, TransferOptions{MaxWork: limit})
		if r.Status == TransferOK || s.budget.state.work > limit {
			t.Fatalf("cutoff %d/%d escaped: %s %+v", limit, work, s.budget.status, r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, r := summaryDiamond(t, TransferOptions{Context: ctx})
	if s.budget.status != TransferCancelled || r.Status == TransferOK {
		t.Fatalf("cancel escaped: %+v", r)
	}
}
func TestCallBindingValidationCannotAwardKnownResult(t *testing.T) {
	cases := []struct {
		name string
		edit func(*facts.Instruction)
	}{
		{"missing", func(in *facts.Instruction) { in.Call.Bindings = nil }},
		{"duplicate", func(in *facts.Instruction) { in.Call.Bindings = append(in.Call.Bindings, in.Call.Bindings[0]) }},
		{"wrongFormal", func(in *facts.Instruction) { in.Call.Bindings[0].Formal = "other" }},
		{"ambiguousActual", func(in *facts.Instruction) { in.Call.Bindings[0].Value = "arg0" }},
		{"resultType", func(in *facts.Instruction) { in.Type = "bool"; in.ValueKind = facts.FlowKindBoolean }},
		{"resultArity", func(in *facts.Instruction) { in.Results = []string{"call", "extra"} }},
		{"discardWrongArity", func(in *facts.Instruction) { in.Results = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			call := summaryCall("call", "identity", "arg0")
			test.edit(&call)
			root := summaryFixture("root", call, controlReturn("return", "arg0"))
			identity := summaryFixture("identity", controlReturn("return", "arg0"))
			artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{root, identity}})
			if err != nil {
				t.Fatal(err)
			}
			session := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
			r := session.evaluate("root")
			if r.Status == TransferOK || len(r.Gaps) == 0 || len(r.Effects) == 0 {
				t.Fatalf("bad binding lost effect: %+v", r)
			}
		})
	}
}

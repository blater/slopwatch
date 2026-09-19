package depth

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestReceiverStorageReadIsIdentityAndTransformationRemainsConnected(t *testing.T) {
	for _, transform := range []bool{false, true} {
		instructions := []facts.Instruction{{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"this"}, Results: []string{"value"}, FieldID: "Counter.n", Type: "int", ValueKind: facts.FlowKindNumeric}}
		result := "value"
		if transform {
			instructions = append(instructions, controlConstant("two", "2"), facts.Instruction{ID: "multiply", Opcode: facts.OpPrimitive, Type: "int", ValueKind: facts.FlowKindNumeric, ArithmeticMode: "integer", Operator: "*", Operands: []string{"value", "two"}, Results: []string{"product"}})
			result = "product"
		}
		instructions = append(instructions, controlReturn("return", result))
		function := facts.FlowFunction{ID: "read", Entry: "entry", ReceiverFormal: &facts.Formal{ID: "this", Type: "Counter", ValueKind: facts.FlowKindReference}, Blocks: []facts.FlowBlock{{ID: "entry", Instructions: instructions}}}
		artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "Counter", Functions: []facts.FlowFunction{function}})
		if err != nil {
			t.Fatal(err)
		}
		session := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
		proof := proveFunctionOutcomes(session, "read", facts.BoundaryIdentity{Artifact: "Counter", Symbol: "Counter"})
		want := 0
		if transform {
			want = 1
		}
		if len(proof.reasons) != 0 || len(proof.obligations) != want {
			t.Fatalf("transform=%v: %+v", transform, proof)
		}
		// The same receiver is not an ownership proof for writes.
		function.Blocks[0].Instructions = []facts.Instruction{controlConstant("one", "1"), {ID: "write", Opcode: facts.OpFieldWrite, Operands: []string{"this", "one"}, FieldID: "Counter.n", Type: "int", ValueKind: facts.FlowKindNumeric}, controlReturn("return", "one")}
		artifact, err = CompileFlowArtifact(facts.FlowArtifact{Artifact: "Counter", Functions: []facts.FlowFunction{function}})
		if err != nil {
			t.Fatal(err)
		}
		session = newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
		if proof := proveFunctionOutcomes(session, "read", facts.BoundaryIdentity{Artifact: "Counter", Symbol: "Counter"}); len(proof.reasons) == 0 {
			t.Fatal("receiver write was accepted without state proof")
		}
	}
}

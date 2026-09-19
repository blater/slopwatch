package depth

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func resourceSummaryFunction(id string, body ...facts.Instruction) facts.FlowFunction {
	return facts.FlowFunction{ID: id, Entry: "entry", Formals: []facts.Formal{{ID: "arg0", Type: "int", ValueKind: facts.FlowKindNumeric}}, Blocks: []facts.FlowBlock{{ID: "entry", Instructions: body}}}
}

func TestResourceLifecycleHelperWitness(t *testing.T) {
	call := summaryCall("call", "helper", "arg0")
	functions := []facts.FlowFunction{
		resourceSummaryFunction("helper", resourceAcquire(), resourceCleanup(), controlConstant("value", "7"), controlReturn("return", "value")),
		resourceSummaryFunction("outer", call, controlReturn("return", "call")),
	}
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "helper", Functions: functions})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	evaluation := summaries.evaluate("outer")
	if evaluation.Status != TransferOK || len(evaluation.Gaps) != 0 {
		t.Fatalf("helper evaluation partial: %+v", evaluation)
	}
	if len(evaluation.ResourceWitnesses) != 1 || len(evaluation.Dependencies) != 1 {
		t.Fatalf("helper witness/dependency missing: %+v", evaluation)
	}
	proof := proveFunctionOutcomes(summaries, "outer", facts.BoundaryIdentity{Artifact: "helper", Symbol: "outer"})
	if len(proof.reasons) != 0 || len(proof.obligations) != 1 || proof.obligations[0].Category != facts.ObligationResource {
		t.Fatalf("helper resource not measured: %+v", proof)
	}
}

func TestResourceLifecycleNestedHelperAndMissingCleanup(t *testing.T) {
	leaf := resourceSummaryFunction("leaf", resourceAcquire(), resourceCleanup(), controlConstant("value", "7"), controlReturn("return", "value"))
	middle := resourceSummaryFunction("middle", summaryCall("leafCall", "leaf", "arg0"), controlReturn("return", "leafCall"))
	outer := resourceSummaryFunction("outer", summaryCall("middleCall", "middle", "arg0"), controlReturn("return", "middleCall"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "nested", Functions: []facts.FlowFunction{leaf, middle, outer}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	if got := summaries.evaluate("outer"); got.Status != TransferOK || len(got.ResourceWitnesses) != 1 {
		t.Fatalf("nested helper witness missing: %+v", got)
	}
	proof := proveFunctionOutcomes(summaries, "outer", facts.BoundaryIdentity{Artifact: "nested", Symbol: "outer"})
	if len(proof.reasons) != 0 || len(proof.obligations) != 1 {
		t.Fatalf("nested helper resource not measured: %+v", proof)
	}

	missing := resourceSummaryFunction("missing", resourceAcquire(), controlConstant("value", "7"), controlReturn("return", "value"))
	artifact, err = CompileFlowArtifact(facts.FlowArtifact{Artifact: "missing", Functions: []facts.FlowFunction{missing, resourceSummaryFunction("outer", summaryCall("call", "missing", "arg0"), controlReturn("return", "call"))}})
	if err != nil {
		t.Fatal(err)
	}
	summaries = newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	got := summaries.evaluate("outer")
	if got.Status == TransferOK || len(got.Effects) == 0 {
		t.Fatalf("missing helper cleanup was credited: %+v", got)
	}
}

func TestResourceLifecycleConstantFalseHelperHasNoWitness(t *testing.T) {
	condition := facts.Instruction{ID: "condition", Opcode: facts.OpConstant, Results: []string{"condition"}, Type: "bool", ValueKind: facts.FlowKindBoolean, Value: &facts.Value{Type: "bool", ValueKind: facts.FlowKindBoolean, Constant: "false"}}
	branch := facts.Instruction{ID: "branch", Opcode: facts.OpBranch, Operands: []string{"condition"}}
	entry := facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{condition, branch}, Edges: []facts.FlowEdge{{From: "entry", To: "acquired", Kind: facts.EdgeTrue, Guard: "condition", GuardPolarity: string(facts.EdgeTrue)}, {From: "entry", To: "safe", Kind: facts.EdgeFalse, Guard: "condition", GuardPolarity: string(facts.EdgeFalse)}}}
	helper := facts.FlowFunction{ID: "helper", Entry: "entry", Formals: []facts.Formal{{ID: "arg0", Type: "int", ValueKind: facts.FlowKindNumeric}}, Blocks: []facts.FlowBlock{
		entry,
		{ID: "acquired", Instructions: []facts.Instruction{resourceAcquire(), resourceCleanup(), controlConstant("value", "7"), controlReturn("return", "value")}},
		{ID: "safe", Instructions: []facts.Instruction{controlConstant("safeValue", "8"), controlReturn("safeReturn", "safeValue")}},
	}}
	outer := resourceSummaryFunction("outer", summaryCall("call", "helper", "arg0"), controlReturn("return", "call"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "false", Functions: []facts.FlowFunction{helper, outer}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	got := summaries.evaluate("outer")
	if got.Status != TransferOK || len(got.ResourceWitnesses) != 0 || len(got.Effects) != 0 {
		t.Fatalf("constant-false helper acquired resource: %+v", got)
	}
}

func TestResourceLifecycleHelperWithPolicyStaysPartial(t *testing.T) {
	helper := resourceSummaryFunction("helper", resourceAcquire(), resourceCleanup(), controlConstant("value", "7"), controlReturn("return", "value"))
	helper.Formals[0].Path = "policy"
	outer := resourceSummaryFunction("outer", summaryCall("call", "helper", "arg0"), controlReturn("return", "call"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "policy-helper", Functions: []facts.FlowFunction{helper, outer}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	got := summaries.evaluate("outer")
	if got.Status == TransferOK || len(got.Effects) == 0 {
		t.Fatalf("policy helper resource was credited: %+v", got)
	}
}

func TestResourceLifecycleHelperExceptionalExitRetainsWitness(t *testing.T) {
	panicHelper := resourceSummaryFunction("panic", resourceAcquire(), resourceCleanup(), facts.Instruction{ID: "panic", Opcode: facts.OpThrow})
	outer := resourceSummaryFunction("outer", summaryCall("call", "panic", "arg0"), controlReturn("return", "call"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "panic-helper", Functions: []facts.FlowFunction{panicHelper, outer}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	got := summaries.evaluate("outer")
	if len(got.ResourceWitnesses) != 1 || len(got.Completions) != 1 || got.Completions[0].Kind != facts.EdgeThrow {
		t.Fatalf("exceptional helper lost lifecycle witness: %+v", got)
	}
}

func TestPurePolicyHelperWithoutResourcesRemainsSupported(t *testing.T) {
	helper := summaryFixture("policy", controlReturn("return", "arg0"))
	helper.Formals[0].Path = "policy"
	outer := summaryFixture("outer", summaryCall("call", "policy", "arg0"), controlReturn("return", "call"))
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "pure-policy", Functions: []facts.FlowFunction{helper, outer}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	got := summaries.evaluate("outer")
	if got.Status != TransferOK || len(got.Gaps) != 0 || len(got.Effects) != 0 {
		t.Fatalf("pure policy helper regressed: %+v", got)
	}
}

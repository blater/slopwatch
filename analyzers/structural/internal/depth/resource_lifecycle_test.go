package depth

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func resourceRootFact() facts.AliasRoot {
	return facts.AliasRoot{ID: "resource-site", Kind: "allocation", Ownership: "owned", Mutable: true}
}

func resourceAcquire() facts.Instruction {
	return facts.Instruction{ID: "acquire", Opcode: facts.OpAcquire, Results: []string{"handle"}, Type: "Handle", ValueKind: facts.FlowKindReference, Roots: []facts.AliasRoot{resourceRootFact()}}
}

func resourceAcquireResult(result string) facts.Instruction {
	in := resourceAcquire()
	in.Results = []string{result}
	return in
}

func resourceCleanup() facts.Instruction {
	return facts.Instruction{ID: "cleanup", Opcode: facts.OpCleanupAttempt, Operands: []string{"handle"}, Roots: []facts.AliasRoot{resourceRootFact()}}
}

func resourceFunction(t *testing.T, blocks ...facts.FlowBlock) *CompiledFunction {
	t.Helper()
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "resource-test", Functions: []facts.FlowFunction{{ID: "resource", Entry: "entry", Blocks: blocks}}})
	if err != nil {
		t.Fatal(err)
	}
	function, ok := artifact.Function("resource")
	if !ok {
		t.Fatal("missing compiled resource function")
	}
	return function
}

func resourceEvaluation(arena *RecipeArena, function *CompiledFunction, events ...GuardedEffect) FlowEvaluation {
	trueGuard := arena.Constant("bool", "true")
	for index := range events {
		if events[index].Guard == "" {
			events[index].Guard = trueGuard
		}
	}
	evaluation := FlowEvaluation{Status: TransferOK, Completions: []FlowCompletion{{Guard: trueGuard, Block: function.function.Entry, Kind: facts.EdgeNormal}}, Effects: events}
	for _, blockID := range function.reachable {
		for _, edge := range function.Successors(blockID) {
			evaluation.Edges = append(evaluation.Edges, GuardedEdge{Guard: trueGuard, From: edge.From, To: edge.To, Kind: edge.Kind})
		}
	}
	return evaluation
}

func resourceEffect(block, instruction, kind string, ownership OwnershipState) GuardedEffect {
	root := resourceRootFact()
	return GuardedEffect{Block: block, Effect: TransferEffect{Kind: kind, RootID: root.ID, RootKind: root.Kind, Ownership: ownership, Mutable: root.Mutable, Instruction: instruction}}
}

func TestResourceLifecycleProofRequiresEveryExit(t *testing.T) {
	function := resourceFunction(t,
		facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{resourceAcquire()}, Edges: []facts.FlowEdge{{From: "entry", To: "cleanup", Kind: facts.EdgeCleanup}, {From: "entry", To: "bare", Kind: facts.EdgeNormal}}},
		facts.FlowBlock{ID: "cleanup", Instructions: []facts.Instruction{resourceCleanup()}},
		facts.FlowBlock{ID: "bare"},
	)
	arena := NewRecipeArena()
	artifact := &CompiledArtifact{artifact: facts.FlowArtifact{Artifact: "resource-test"}, functions: map[string]*CompiledFunction{"resource": function}}
	summaries := newFunctionSummaries(arena, artifact, TransferOptions{})
	evaluation := resourceEvaluation(arena, function,
		resourceEffect("entry", "acquire", string(facts.OpAcquire), OwnershipOwned),
		resourceEffect("cleanup", "cleanup", string(facts.OpCleanupAttempt), OwnershipOwned),
	)
	evaluation.Completions[0].Block = "bare"
	proof := proveResourceLifecycles(summaries, "resource", evaluation, facts.BoundaryIdentity{Artifact: "resource-test", Audience: "public", View: "function", Symbol: "resource"})
	if proof.allEffectsProven || len(proof.obligations) != 0 {
		t.Fatalf("missing cleanup branch was credited: %+v", proof)
	}
}

func TestResourceLifecycleProofRejectsOrderingAndEscape(t *testing.T) {
	function := resourceFunction(t,
		facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "handle", Opcode: facts.OpConstant, Results: []string{"handle"}, Type: "Handle", ValueKind: facts.FlowKindReference}, resourceCleanup(), resourceAcquireResult("newhandle")}},
	)
	arena := NewRecipeArena()
	artifact := &CompiledArtifact{artifact: facts.FlowArtifact{Artifact: "resource-test"}, functions: map[string]*CompiledFunction{"resource": function}}
	summaries := newFunctionSummaries(arena, artifact, TransferOptions{})
	ordered := resourceEvaluation(arena, function,
		resourceEffect("entry", "cleanup", string(facts.OpCleanupAttempt), OwnershipOwned),
		resourceEffect("entry", "acquire", string(facts.OpAcquire), OwnershipOwned),
	)
	proof := proveResourceLifecycles(summaries, "resource", ordered, facts.BoundaryIdentity{Artifact: "resource-test", Symbol: "resource"})
	if proof.allEffectsProven || len(proof.obligations) != 0 {
		t.Fatalf("cleanup before acquire was credited: %+v", proof)
	}

	function = resourceFunction(t,
		facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{resourceAcquire(), {ID: "escape", Opcode: facts.OpEscape, Operands: []string{"handle"}}, resourceCleanup()}},
	)
	artifact.functions["resource"] = function
	evaluation := resourceEvaluation(arena, function,
		resourceEffect("entry", "acquire", string(facts.OpAcquire), OwnershipOwned),
		resourceEffect("entry", "escape", string(facts.OpEscape), OwnershipEscaped),
		resourceEffect("entry", "cleanup", string(facts.OpCleanupAttempt), OwnershipEscaped),
	)
	proof = proveResourceLifecycles(summaries, "resource", evaluation, facts.BoundaryIdentity{Artifact: "resource-test", Symbol: "resource"})
	if proof.allEffectsProven || len(proof.obligations) != 0 {
		t.Fatalf("escaped resource was credited: %+v", proof)
	}
}

func TestResourceLifecycleProofPositiveAndReturnedHandleNegative(t *testing.T) {
	function := resourceFunction(t,
		facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{resourceAcquire(), resourceCleanup()}},
	)
	arena := NewRecipeArena()
	artifact := &CompiledArtifact{artifact: facts.FlowArtifact{Artifact: "resource-test"}, functions: map[string]*CompiledFunction{"resource": function}}
	summaries := newFunctionSummaries(arena, artifact, TransferOptions{})
	boundary := facts.BoundaryIdentity{Artifact: "resource-test", Symbol: "resource"}
	evaluation := resourceEvaluation(arena, function,
		resourceEffect("entry", "acquire", string(facts.OpAcquire), OwnershipOwned),
		resourceEffect("entry", "cleanup", string(facts.OpCleanupAttempt), OwnershipOwned),
	)
	proof := proveResourceLifecycles(summaries, "resource", evaluation, boundary)
	if !proof.allEffectsProven || len(proof.obligations) != 1 || proof.obligations[0].Category != facts.ObligationResource {
		t.Fatalf("valid lifecycle was not credited: %+v", proof)
	}
	root, ok := rootFromFact(resourceRootFact())
	if !ok {
		t.Fatal("invalid test resource root")
	}
	evaluation.Completions[0].Values = []DomainValue{NewValue(arena.Constant("Handle", "returned"), "Handle", KindReference, nil, NewAliasSet(root))}
	proof = proveResourceLifecycles(summaries, "resource", evaluation, boundary)
	if proof.allEffectsProven || len(proof.obligations) != 0 {
		t.Fatalf("returned resource was credited: %+v", proof)
	}

	ambiguous := resourceEffect("entry", "cleanup", string(facts.OpCleanupAttempt), OwnershipOwned)
	ambiguous.Effect.RootID = "another-resource-site"
	evaluation = resourceEvaluation(arena, function,
		resourceEffect("entry", "acquire", string(facts.OpAcquire), OwnershipOwned),
		resourceEffect("entry", "cleanup", string(facts.OpCleanupAttempt), OwnershipOwned),
		ambiguous,
	)
	proof = proveResourceLifecycles(summaries, "resource", evaluation, boundary)
	if proof.allEffectsProven || len(proof.obligations) != 0 {
		t.Fatalf("ambiguous cleanup target was credited: %+v", proof)
	}
}

func TestResourceLifecycleFlowsThroughOutcomeProof(t *testing.T) {
	function := resourceFunction(t,
		facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{
			resourceAcquire(),
			resourceCleanup(),
			{ID: "value", Opcode: facts.OpConstant, Results: []string{"value"}, Type: "int", ValueKind: facts.FlowKindNumeric, Value: &facts.Value{Type: "int", ValueKind: facts.FlowKindNumeric, Constant: "7"}},
			{ID: "return", Opcode: facts.OpReturn, Operands: []string{"value"}},
		}},
	)
	arena := NewRecipeArena()
	artifact := &CompiledArtifact{artifact: facts.FlowArtifact{Artifact: "resource-test"}, functions: map[string]*CompiledFunction{"resource": function}}
	summaries := newFunctionSummaries(arena, artifact, TransferOptions{})
	proof := proveFunctionOutcomes(summaries, "resource", facts.BoundaryIdentity{Artifact: "resource-test", Symbol: "resource"})
	if len(proof.reasons) != 0 || len(proof.alternatives) != 1 || len(proof.obligations) != 1 || proof.obligations[0].Category != facts.ObligationResource {
		t.Fatalf("resource outcome proof was not measured: %+v", proof)
	}
	if len(proof.alternatives[0]) != 1 || proof.alternatives[0][0] != proof.obligations[0].ID {
		t.Fatalf("resource obligation missing from alternative: %+v", proof.alternatives)
	}
}

func TestResourceLifecycleUsesResolvedReachableEdges(t *testing.T) {
	artifact, err := CompileFlowArtifact(facts.FlowArtifact{Artifact: "resource-test", Functions: []facts.FlowFunction{{
		ID: "resource", Entry: "entry", Blocks: []facts.FlowBlock{
			{ID: "entry", Instructions: []facts.Instruction{{ID: "condition", Opcode: facts.OpConstant, Results: []string{"condition"}, Type: "bool", ValueKind: facts.FlowKindBoolean, Value: &facts.Value{Type: "bool", ValueKind: facts.FlowKindBoolean, Constant: "true"}}, {ID: "branch", Opcode: facts.OpBranch, Operands: []string{"condition"}}}, Edges: []facts.FlowEdge{{From: "entry", To: "clean", Kind: facts.EdgeTrue, Guard: "condition", GuardPolarity: string(facts.EdgeTrue)}, {From: "entry", To: "bare", Kind: facts.EdgeFalse, Guard: "condition", GuardPolarity: string(facts.EdgeFalse)}}},
			{ID: "clean", Instructions: []facts.Instruction{resourceAcquire(), resourceCleanup(), {ID: "value", Opcode: facts.OpConstant, Results: []string{"value"}, Type: "int", ValueKind: facts.FlowKindNumeric, Value: &facts.Value{Type: "int", ValueKind: facts.FlowKindNumeric, Constant: "1"}}, {ID: "return", Opcode: facts.OpReturn, Operands: []string{"value"}}}},
			{ID: "bare", Instructions: []facts.Instruction{{ID: "other", Opcode: facts.OpConstant, Results: []string{"other"}, Type: "int", ValueKind: facts.FlowKindNumeric, Value: &facts.Value{Type: "int", ValueKind: facts.FlowKindNumeric, Constant: "2"}}, {ID: "returnOther", Opcode: facts.OpReturn, Operands: []string{"other"}}}},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	proof := proveFunctionOutcomes(summaries, "resource", facts.BoundaryIdentity{Artifact: "resource-test", Symbol: "resource"})
	if len(proof.reasons) != 0 || len(proof.obligations) != 1 {
		t.Fatalf("unreachable cleanup alternative affected R proof: %+v", proof)
	}
}

func TestResourceLifecycleRequiresFullTerminalCoverage(t *testing.T) {
	arena := NewRecipeArena()
	guards := newControlGuards(arena, func(int) bool { return true })
	predicate, _ := arena.Formal(0, "bool")
	for _, coverage := range []guardID{guardFalse, guards.atom(predicate)} {
		walker := resourceWalker{guards: guards, exits: map[string]guardID{"exit": coverage}, memo: map[resourceWalkKey]bool{}, visiting: map[resourceWalkKey]bool{}}
		ok, reason := walker.finishTerminal(resourceWalkKey{block: "exit"}, "exit", guardTrue, guardTrue)
		if ok || reason != "resource_exit_coverage_unknown" {
			t.Fatalf("incomplete terminal coverage accepted: ok=%v reason=%s", ok, reason)
		}
	}
}

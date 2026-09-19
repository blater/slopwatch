package depth

import (
	"context"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func compileProbe(blocks ...facts.FlowBlock) facts.FlowFunction {
	return facts.FlowFunction{ID: "probe", Entry: "entry", Formals: []facts.Formal{{ID: "x", Type: "int"}}, Blocks: blocks}
}

func TestCompileDefensiveSnapshotAndCanonicalBlockOrder(t *testing.T) {
	input := compileProbe(facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "ret", Opcode: facts.OpReturn, Operands: []string{"x"}}}}, facts.FlowBlock{ID: "entry"})
	compiled, err := CompileFunction(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Blocks[0].Instructions[0].Operands[0] = "changed"
	blocks := compiled.BlocksSnapshot()
	if len(blocks) != 2 || blocks[0].ID != "entry" || blocks[1].ID != "join" {
		t.Fatalf("noncanonical block snapshot: %+v", blocks)
	}
	blocks[1].Instructions[0].Operands[0] = "changed"
	again, _ := compiled.Block("join")
	if again.Instructions[0].Operands[0] != "x" {
		t.Fatal("compiled graph exposed mutable instruction data")
	}
}

func TestCompileUsesEffectiveCleanupEdges(t *testing.T) {
	fn := compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "ret", Opcode: facts.OpReturn}}, Edges: []facts.FlowEdge{{From: "entry", To: "cleanup", Kind: facts.EdgeCleanup}, {From: "entry", To: "dead", Kind: facts.EdgeNormal}}}, facts.FlowBlock{ID: "cleanup"}, facts.FlowBlock{ID: "dead"})
	compiled, err := CompileFunction(fn)
	if err != nil {
		t.Fatal(err)
	}
	got := compiled.ReachableBlocks()
	if len(got) != 2 || got[0] != "cleanup" || got[1] != "entry" {
		t.Fatalf("ordinary post-return edge remained reachable: %v", got)
	}
}

func TestCompilePhiRequiresExactlyNamedIncomingEdges(t *testing.T) {
	fn := compileProbe(facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{{From: "entry", To: "left", Kind: facts.EdgeTrue}, {From: "entry", To: "right", Kind: facts.EdgeFalse}}}, facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{{ID: "leftv", Opcode: facts.OpBind, Operands: []string{"x"}, Results: []string{"left"}}}, Edges: []facts.FlowEdge{{From: "left", To: "join", Kind: facts.EdgeNormal}}}, facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{{ID: "rightv", Opcode: facts.OpBind, Operands: []string{"x"}, Results: []string{"right"}}}, Edges: []facts.FlowEdge{{From: "right", To: "join", Kind: facts.EdgeNormal}}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Results: []string{"out"}, PhiInputs: []facts.PhiInput{{Predecessor: "left", Value: "left"}, {Predecessor: "right", Value: "right"}}}, {ID: "ret", Opcode: facts.OpReturn, Operands: []string{"out"}}}})
	if _, err := CompileFunction(fn); err != nil {
		t.Fatalf("explicit predecessor-keyed phi rejected: %v", err)
	}
	fn.Blocks[len(fn.Blocks)-1].Instructions[0].PhiInputs = fn.Blocks[len(fn.Blocks)-1].Instructions[0].PhiInputs[:1]
	if _, err := CompileFunction(fn); err == nil {
		t.Fatal("accepted incomplete phi inputs")
	}
}

func TestCompileArtifactKeepsUnknownCallLocal(t *testing.T) {
	artifact := facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{
		compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "call", Opcode: facts.OpCall, Results: []string{"out"}, Call: &facts.CallBinding{Targets: []string{"missing"}}}, {ID: "ret", Opcode: facts.OpReturn, Operands: []string{"out"}}}}),
		compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "ret", Opcode: facts.OpReturn, Operands: []string{"x"}}}}),
	}}
	artifact.Functions[1].ID = "other"
	compiled, err := CompileFlowArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.FunctionsSnapshot(); len(got) != 2 || got[0] != "other" || got[1] != "probe" {
		t.Fatalf("unexpected artifact functions: %v", got)
	}
}

func TestCompilePreflightCountsNestedElements(t *testing.T) {
	fn := compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "pack", Opcode: facts.OpPack, Results: []string{"out"}, FieldBindings: []facts.FieldBinding{{Field: "A.x", Value: "x"}}}}})
	_, err := CompileFunctionWithOptions(fn, CompileOptions{MaxElements: 1})
	if err == nil || !strings.Contains(err.Error(), "graph_limit") {
		t.Fatalf("expected bounded preflight error, got %v", err)
	}
}

func TestCompileHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CompileFunctionWithOptions(compileProbe(facts.FlowBlock{ID: "entry"}), CompileOptions{Context: ctx})
	if err == nil || !strings.Contains(err.Error(), "compile_cancelled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

func TestCompileArtifactAdmitsCanonicallyAndBoundsMetadata(t *testing.T) {
	validA := compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{{ID: "ret", Opcode: facts.OpReturn, Operands: []string{"x"}}}})
	validA.ID = "a"
	validZ := validA
	validZ.ID = "z"
	bad := validA
	bad.ID = "bad"
	bad.Entry = "missing"
	compiled, diagnostics, err := CompileFlowArtifactPartialWithOptions(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{validZ, bad, validA}}, CompileOptions{MaxFunctions: 3})
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("unexpected artifact admission: artifact=%v diagnostics=%v", err, diagnostics)
	}
	if got := compiled.FunctionsSnapshot(); len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("functions were not canonically admitted: %v", got)
	}
}

func TestCompileArtifactRejectsCyclicTypeShape(t *testing.T) {
	shape := &facts.TypeShape{}
	shape.Children = []*facts.TypeShape{shape}
	_, _, err := CompileFlowArtifactPartial(facts.FlowArtifact{Artifact: "a", Types: []facts.FlowType{{ID: "T", Fields: []facts.Field{{Name: "x", Type: shape}}}}})
	if err == nil || !strings.Contains(err.Error(), "cyclic_type_shape") {
		t.Fatalf("cyclic type shape was not rejected: %v", err)
	}
}

func TestCompileArtifactBoundsSharedTypeShapeExpansion(t *testing.T) {
	leaf := &facts.TypeShape{StableID: "leaf"}
	typ := facts.FlowType{ID: "T", Fields: []facts.Field{{Name: "a", Type: leaf}, {Name: "b", Type: leaf}}}
	compiled, _, err := CompileFlowArtifactPartialWithOptions(facts.FlowArtifact{Artifact: "a", Types: []facts.FlowType{typ}}, CompileOptions{MaxElements: 5})
	if err != nil {
		t.Fatalf("shared type shape rejected: %v", err)
	}
	if len(compiled.FlowArtifact().Types) != 1 {
		t.Fatal("metadata snapshot omitted admitted type")
	}
}

func TestCompilerRejectsInvalidCompletionKinds(t *testing.T) {
	for _, in := range []facts.Instruction{
		{ID: "bad", Opcode: facts.OpReturn, CompletionKind: facts.EdgePanic},
		{ID: "bad", Opcode: facts.OpConstant, Results: []string{"x"}, CompletionKind: facts.EdgeReturnError},
	} {
		if err := validateInstruction("f", "entry", in); err == nil {
			t.Fatalf("accepted invalid completion metadata: %+v", in)
		}
	}
}

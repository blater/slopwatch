package depth

import (
	"context"
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"strings"
	"testing"
)

func recurrenceProbe() facts.FlowFunction {
	phi := facts.Instruction{ID: "indexPhi", Opcode: facts.OpPhi, Results: []string{"index"}, Type: "int", ValueKind: facts.FlowKindNumeric, PhiInputs: []facts.PhiInput{{Predecessor: "entry", Value: "zero"}, {Predecessor: "body", Value: "next"}}}
	return facts.FlowFunction{ID: "loop", Entry: "entry", Formals: []facts.Formal{{ID: "limit", Type: "int", ValueKind: facts.FlowKindNumeric}}, Blocks: []facts.FlowBlock{
		{ID: "entry", Instructions: []facts.Instruction{controlConstant("zero", "0"), controlConstant("one", "1")}, Edges: []facts.FlowEdge{controlEdge("entry", "header", facts.EdgeNormal)}},
		{ID: "header", Instructions: []facts.Instruction{phi, {ID: "condition", Opcode: facts.OpPrimitive, Operator: "<", ArithmeticMode: "integer", Operands: []string{"index", "limit"}, Results: []string{"test"}, Type: "bool", ValueKind: facts.FlowKindBoolean}}, Edges: []facts.FlowEdge{{From: "header", To: "body", Kind: facts.EdgeTrue, Guard: "test", GuardPolarity: "true"}, {From: "header", To: "exit", Kind: facts.EdgeFalse, Guard: "test", GuardPolarity: "false"}}},
		{ID: "body", Instructions: []facts.Instruction{{ID: "increment", Opcode: facts.OpPrimitive, Operator: "+", ArithmeticMode: "integer", Operands: []string{"index", "one"}, Results: []string{"next"}, Type: "int", ValueKind: facts.FlowKindNumeric}}, Edges: []facts.FlowEdge{controlEdge("body", "header", facts.EdgeNormal)}},
		{ID: "exit", Instructions: []facts.Instruction{controlReturn("return", "index")}},
	}, Recurrences: []facts.Recurrence{{ID: "loop0/slot0", LoopHeader: "header", PhiSlot: "index", Definition: "next", References: []string{"loop0/slot0"}}}}
}
func TestCompileRecurrenceUsesActualPhiBindings(t *testing.T) {
	source := recurrenceProbe()
	compiled, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	got := compiled.RecurrencesSnapshot()
	if len(got) != 1 || got[0].Initial["entry"] != "zero" || got[0].Updates["body"] != "next" || got[0].PhiID != "indexPhi" || got[0].Kind != facts.FlowKindNumeric {
		t.Fatalf("wrong recurrence: %+v", got)
	}
	got[0].Initial["entry"] = "changed"
	got[0].Updates["body"] = "changed"
	got[0].References[0] = "changed"
	source.Recurrences[0].References[0] = "changed"
	again := compiled.RecurrencesSnapshot()
	if again[0].Initial["entry"] != "zero" || again[0].Updates["body"] != "next" || again[0].References[0] != "loop0/slot0" {
		t.Fatal("recurrence snapshot aliases input or compiled state")
	}
}
func TestCompileRecurrenceRejectsMisleadingMetadata(t *testing.T) {
	cases := []struct {
		name, code string
		edit       func(*facts.FlowFunction)
	}{
		{"id", "invalid_recurrence_id", func(f *facts.FlowFunction) { f.Recurrences[0].ID = "" }},
		{"duplicate", "invalid_recurrence_id", func(f *facts.FlowFunction) { f.Recurrences = append(f.Recurrences, f.Recurrences[0]) }},
		{"header", "missing_recurrence_header", func(f *facts.FlowFunction) { f.Recurrences[0].LoopHeader = "missing" }},
		{"notPhi", "invalid_recurrence_phi", func(f *facts.FlowFunction) { f.Recurrences[0].PhiSlot = "next" }},
		{"instructionNotValue", "invalid_recurrence_phi", func(f *facts.FlowFunction) { f.Recurrences[0].PhiSlot = "indexPhi" }},
		{"wrongHeader", "invalid_recurrence_phi", func(f *facts.FlowFunction) { f.Recurrences[0].LoopHeader = "body" }},
		{"unrelatedUpdate", "recurrence_update_mismatch", func(f *facts.FlowFunction) { f.Recurrences[0].Definition = "one" }},
		{"missingReference", "unbound_recurrence_reference", func(f *facts.FlowFunction) { f.Recurrences[0].References = []string{"other"} }},
		{"duplicateReference", "duplicate_recurrence_reference", func(f *facts.FlowFunction) { f.Recurrences[0].References = []string{"loop0/slot0", "loop0/slot0"} }},
		{"duplicateSlot", "duplicate_recurrence_slot", func(f *facts.FlowFunction) {
			r := f.Recurrences[0]
			r.ID = "loop0/slot1"
			f.Recurrences = append(f.Recurrences, r)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := recurrenceProbe()
			test.edit(&source)
			_, err := CompileFunction(source)
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("want %s, got %v", test.code, err)
			}
		})
	}
}
func TestCompileRecurrenceMultipleLatchesPreserveUpdates(t *testing.T) {
	source := recurrenceProbe()
	source.Blocks[2].Edges = []facts.FlowEdge{{From: "body", To: "latch1", Kind: facts.EdgeTrue, Guard: "test", GuardPolarity: "true"}, {From: "body", To: "latch2", Kind: facts.EdgeFalse, Guard: "test", GuardPolarity: "false"}}
	source.Blocks = append(source.Blocks, facts.FlowBlock{ID: "latch1", Edges: []facts.FlowEdge{controlEdge("latch1", "header", facts.EdgeNormal)}}, facts.FlowBlock{ID: "latch2", Edges: []facts.FlowEdge{controlEdge("latch2", "header", facts.EdgeNormal)}})
	source.Blocks[1].Instructions[0].PhiInputs = []facts.PhiInput{{Predecessor: "entry", Value: "zero"}, {Predecessor: "latch1", Value: "next"}, {Predecessor: "latch2", Value: "index"}}
	source.Recurrences[0].Definition = ""
	compiled, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.RecurrencesSnapshot()[0].Updates; !reflect.DeepEqual(got, map[string]string{"latch1": "next", "latch2": "index"}) {
		t.Fatalf("lost latch correlation: %v", got)
	}
	source.Recurrences[0].Definition = "next"
	if _, err := CompileFunction(source); err == nil {
		t.Fatal("allowed common update assertion to hide a distinct latch value")
	}
}
func TestCompileRecurrenceWorkCutoffsAndLocalFailure(t *testing.T) {
	source := recurrenceProbe()
	full, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	for limit := 1; limit < full.CompilerWork(); limit++ {
		_, err := CompileFunctionWithOptions(source, CompileOptions{MaxWork: limit})
		if err == nil || !strings.Contains(err.Error(), "graph_limit") {
			t.Fatalf("work cutoff %d: %v", limit, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileFunctionWithOptions(source, CompileOptions{Context: ctx}); err == nil || !strings.Contains(err.Error(), "compile_cancelled") {
		t.Fatal(err)
	}
	source.Recurrences[0].Definition = "wrong"
	peer := compileProbe(facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlReturn("ret", "x")}})
	artifact, diagnostics, err := CompileFlowArtifactPartial(facts.FlowArtifact{Artifact: "a", Functions: []facts.FlowFunction{source, peer}})
	if err != nil || len(diagnostics) != 1 || !reflect.DeepEqual(artifact.FunctionsSnapshot(), []string{"probe"}) {
		t.Fatalf("recurrence failure discarded unrelated peer: %v %v", diagnostics, err)
	}
}

func TestCompileRecurrenceCanonicalOrderAndAcyclicRejection(t *testing.T) {
	source := recurrenceProbe()
	second := source.Blocks[1].Instructions[0]
	second.ID = "otherPhi"
	second.Results = []string{"other"}
	source.Blocks[1].Instructions = append([]facts.Instruction{second}, source.Blocks[1].Instructions...)
	other := source.Recurrences[0]
	other.ID = "loop0/slot1"
	other.PhiSlot = "other"
	source.Recurrences = append(source.Recurrences, other)
	first, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	source.Recurrences[0], source.Recurrences[1] = source.Recurrences[1], source.Recurrences[0]
	secondCompiled, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.RecurrencesSnapshot(), secondCompiled.RecurrencesSnapshot()) || first.CompilerWork() != secondCompiled.CompilerWork() {
		t.Fatal("descriptor order changed recurrence indexing or cost")
	}
	source = recurrenceProbe()
	source.Blocks[2].Edges = nil
	source.Blocks[1].Instructions[0].PhiInputs = source.Blocks[1].Instructions[0].PhiInputs[:1]
	_, err = CompileFunction(source)
	if err == nil || !strings.Contains(err.Error(), "incomplete_recurrence_edges") {
		t.Fatalf("acyclic phi accepted as recurrence: %v", err)
	}
}

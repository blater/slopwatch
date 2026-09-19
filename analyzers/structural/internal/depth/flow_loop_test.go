package depth

import (
	"context"
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func loopAccumulator(identity bool) facts.FlowFunction {
	f := recurrenceProbe()
	f.Formals = append(f.Formals, facts.Formal{ID: "seed", Type: "int", ValueKind: facts.FlowKindNumeric})
	phi := facts.Instruction{ID: "sumPhi", Opcode: facts.OpPhi, Results: []string{"sum"}, Type: "int", ValueKind: facts.FlowKindNumeric, PhiInputs: []facts.PhiInput{{Predecessor: "entry", Value: "seed"}, {Predecessor: "body", Value: "sumNext"}}}
	update := facts.Instruction{ID: "add", Opcode: facts.OpPrimitive, Operator: "+", ArithmeticMode: "integer", Operands: []string{"sum", "index"}, Results: []string{"sumNext"}, Type: "int", ValueKind: facts.FlowKindNumeric}
	if identity {
		update.Opcode = facts.OpBind
		update.Operands = []string{"sum"}
	}
	f.Blocks[1].Instructions = append([]facts.Instruction{phi}, f.Blocks[1].Instructions...)
	f.Blocks[2].Instructions = append(f.Blocks[2].Instructions, update)
	f.Blocks[3].Instructions = []facts.Instruction{controlReturn("return", "sum")}
	f.Recurrences = append(f.Recurrences, facts.Recurrence{ID: "loop0/slot1", LoopHeader: "header", PhiSlot: "sum", Definition: "sumNext"})
	return f
}
func runLoop(t *testing.T, f facts.FlowFunction, zero bool, options TransferOptions) (*RecipeArena, FlowEvaluation) {
	t.Helper()
	c, err := CompileFunction(f)
	if err != nil {
		t.Fatal(err)
	}
	a := NewRecipeArena()
	s := NewTransferState()
	for i, formal := range f.Formals {
		r, err := a.Formal(i, formal.Type)
		if err != nil {
			t.Fatal(err)
		}
		s.Seed(formal.ID, ScalarValue(r, formal.Type, suppliedKind(formal.ValueKind)))
	}
	if zero {
		s.Seed("limit", ScalarValue(a.Constant("int", "0"), "int", KindNumeric))
	}
	return a, EvaluateFlow(a, c, s, options)
}
func TestFlowNumericLoopClassifiesReturnedResponsibility(t *testing.T) {
	for _, identity := range []bool{false, true} {
		a, r := runLoop(t, loopAccumulator(identity), false, TransferOptions{})
		if r.Status != TransferOK || len(r.Gaps) != 0 || len(r.Completions) != 1 {
			t.Fatalf("identity %v: %+v", identity, r)
		}
		v := r.Completions[0].Values[0]
		want := TransformationPrimitive
		if identity {
			want = TransformationIdentity
		}
		if v.Unknown || a.Classify(v.Recipe) != want {
			t.Fatalf("identity %v: value %+v class %s", identity, v, a.Classify(v.Recipe))
		}
		if a.NodeCount() > 80 || r.Blocks != 4 {
			t.Fatalf("loop expanded: nodes %d blocks %d", a.NodeCount(), r.Blocks)
		}
	}
}
func TestFlowZeroTripPrunesUnknownBody(t *testing.T) {
	f := recurrenceProbe()
	f.Blocks[2].Instructions[0].Opcode = facts.OpUnknown
	a, r := runLoop(t, f, true, TransferOptions{})
	if r.Status != TransferOK || len(r.Gaps) != 0 || len(r.Effects) != 0 || r.Blocks != 3 || len(r.Completions) != 1 {
		t.Fatalf("zero trip: %+v", r)
	}
	if got := r.Completions[0].Values[0]; got.Unknown || got.Recipe != a.Constant("int", "0") {
		t.Fatalf("wrong initial result: %+v", got)
	}
}
func TestFlowLoopUnknownEffectsRemainUnknown(t *testing.T) {
	f := recurrenceProbe()
	f.Blocks[2].Instructions[0].Opcode = facts.OpUnknown
	_, r := runLoop(t, f, false, TransferOptions{})
	if r.Status != TransferPartial || len(r.Completions) != 1 || !r.Completions[0].Values[0].Unknown || len(r.Effects) != 1 || !r.Effects[0].Effect.Unknown {
		t.Fatalf("unknown loop laundered: %+v", r)
	}
}
func TestFlowLoopOrderAndBudget(t *testing.T) {
	f := loopAccumulator(false)
	_, full := runLoop(t, f, false, TransferOptions{})
	for i, j := 0, len(f.Blocks)-1; i < j; i, j = i+1, j-1 {
		f.Blocks[i], f.Blocks[j] = f.Blocks[j], f.Blocks[i]
	}
	_, reversed := runLoop(t, f, false, TransferOptions{})
	if !reflect.DeepEqual(full, reversed) {
		t.Fatalf("source order changed result: %+v / %+v", full, reversed)
	}
	for limit := 1; limit < full.Work; limit++ {
		_, r := runLoop(t, f, false, TransferOptions{MaxWork: limit})
		if r.Status != TransferLimit {
			t.Fatalf("cutoff %d/%d: %+v", limit, full.Work, r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, r := runLoop(t, f, false, TransferOptions{Context: ctx})
	if r.Status != TransferCancelled {
		t.Fatalf("cancel: %+v", r)
	}
}

func TestFlowProvenInfiniteLoopHasNoNormalCompletion(t *testing.T) {
	f := recurrenceProbe()
	f.Blocks[1].Instructions[1] = facts.Instruction{ID: "condition", Opcode: facts.OpConstant, Results: []string{"test"}, Type: "bool", ValueKind: facts.FlowKindBoolean, Value: &facts.Value{Type: "bool", ValueKind: facts.FlowKindBoolean, Constant: "true"}}
	_, r := runLoop(t, f, false, TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 0 {
		t.Fatalf("fabricated infinite-loop exit: %+v", r)
	}
}
func TestFlowLoopRejectsCompletionBearingBackedge(t *testing.T) {
	for _, kind := range []facts.EdgeKind{facts.EdgeReturnError, facts.EdgeThrow, facts.EdgeCleanup} {
		f := recurrenceProbe()
		f.Blocks[2].Edges[0].Kind = kind
		_, r := runLoop(t, f, false, TransferOptions{})
		if r.Status != TransferPartial || len(r.Completions) != 0 {
			t.Fatalf("discarded %s completion: %+v", kind, r)
		}
	}
}
func TestFlowLoopPreservesUpdateDependencies(t *testing.T) {
	f := loopAccumulator(false)
	f.Formals = append(f.Formals, facts.Formal{ID: "delta", Type: "int", ValueKind: facts.FlowKindNumeric})
	f.Blocks[2].Instructions[1].Operands = []string{"sum", "delta"}
	c, err := CompileFunction(f)
	if err != nil {
		t.Fatal(err)
	}
	a := NewRecipeArena()
	s := NewTransferState()
	for i, formal := range f.Formals {
		recipe, _ := a.Formal(i, "int")
		v := ScalarValue(recipe, "int", KindNumeric)
		v.Dependencies = []string{formal.ID}
		s.Seed(formal.ID, v)
	}
	r := EvaluateFlow(a, c, s, TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 {
		t.Fatal(r)
	}
	value := r.Completions[0].Values[0]
	if !reflect.DeepEqual(value.Dependencies, []string{"delta", "limit", "seed"}) || len(value.Aliases.Roots()) != 0 {
		t.Fatalf("lost loop metadata: %+v", value)
	}
}

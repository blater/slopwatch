package depth

import (
	"context"
	"fmt"
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func controlEdge(from, to string, kind facts.EdgeKind) facts.FlowEdge {
	e := facts.FlowEdge{From: from, To: to, Kind: kind}
	if kind == facts.EdgeTrue || kind == facts.EdgeFalse {
		e.Guard = "p"
		e.GuardPolarity = string(kind)
	}
	return e
}
func controlConstant(id, value string) facts.Instruction {
	return facts.Instruction{ID: id, Opcode: facts.OpConstant, Results: []string{id}, Type: "int", ValueKind: facts.FlowKindNumeric, Value: &facts.Value{Type: "int", ValueKind: facts.FlowKindNumeric, Constant: value}}
}
func controlReturn(id string, values ...string) facts.Instruction {
	return facts.Instruction{ID: id, Opcode: facts.OpReturn, Operands: values}
}
func controlCompile(t *testing.T, blocks ...facts.FlowBlock) *CompiledFunction {
	t.Helper()
	fn, err := CompileFunction(facts.FlowFunction{ID: "fn", Entry: "entry", Formals: []facts.Formal{{ID: "p", Type: "bool", ValueKind: facts.FlowKindBoolean}, {ID: "box", Type: "Box", ValueKind: facts.FlowKindReference}}, Blocks: blocks})
	if err != nil {
		t.Fatal(err)
	}
	return fn
}
func controlInitial(t *testing.T, a *RecipeArena) *TransferState {
	t.Helper()
	s := NewTransferState()
	r, err := a.Formal(0, "bool")
	if err != nil {
		t.Fatal(err)
	}
	s.Seed("p", ScalarValue(r, "bool", KindBoolean))
	reviewSeed(t, a, s, "box", reviewRoot("A"))
	return s
}
func controlDiamond(t *testing.T) *CompiledFunction {
	return controlCompile(t,
		facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}},
		facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{controlConstant("one", "1")}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{controlConstant("two", "2")}, Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Type: "int", ValueKind: facts.FlowKindNumeric, Results: []string{"out"}, PhiInputs: []facts.PhiInput{{Predecessor: "left", Value: "one"}, {Predecessor: "right", Value: "two"}}}, controlReturn("return", "out")}})
}
func TestControlDiamondPhi(t *testing.T) {
	a := NewRecipeArena()
	r := EvaluateDAG(a, controlDiamond(t), controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 || r.Completions[0].Values[0].Unknown {
		t.Fatalf("bad phi: %+v", r)
	}
	for _, choice := range []string{"true", "false"} {
		recipe, err := a.Substitute(r.Completions[0].Values[0].Recipe, map[int]RecipeID{0: a.Constant("bool", choice)})
		expected := "1"
		if choice == "false" {
			expected = "2"
		}
		if err != nil || recipe != a.Constant("int", expected) {
			t.Fatalf("choice %s: %s %v", choice, recipe, err)
		}
	}
}
func TestControlConstantPrunesUnsupported(t *testing.T) {
	a := NewRecipeArena()
	s := controlInitial(t, a)
	s.Seed("p", ScalarValue(a.Constant("bool", "true"), "bool", KindBoolean))
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{controlReturn("good")}}, facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{{ID: "bad", Opcode: facts.OpUnknown}, controlReturn("badreturn")}})
	r := EvaluateDAG(a, fn, s, TransferOptions{})
	if r.Status != TransferOK || r.Blocks != 2 || len(r.Gaps) != 0 {
		t.Fatalf("unreachable gap leaked: %+v", r)
	}
}
func TestControlPendingCompletion(t *testing.T) {
	for _, kind := range []facts.Opcode{facts.OpReturn, facts.OpThrow} {
		for _, override := range []bool{false, true} {
			a := NewRecipeArena()
			terminal := controlReturn("initial", "one")
			terminal.Opcode = kind
			cleanup := []facts.Instruction{{ID: "cleanup", Opcode: facts.OpCleanupAttempt, Operands: []string{"box"}, Roots: []facts.AliasRoot{{ID: "A", Kind: "allocation", Ownership: "owned", Mutable: true}}}}
			if override {
				cleanup = append(cleanup, controlConstant("two", "2"), controlReturn("override", "two"))
			}
			fn := controlCompile(t, facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlConstant("one", "1"), terminal}, Edges: []facts.FlowEdge{controlEdge("entry", "cleanup", facts.EdgeCleanup)}}, facts.FlowBlock{ID: "cleanup", Instructions: cleanup})
			r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
			if r.Status != TransferOK || len(r.Completions) != 1 || len(r.Effects) != 1 {
				t.Fatalf("lost cleanup: %+v", r)
			}
			expected := "1"
			completion := facts.EdgeNormal
			if kind == facts.OpThrow {
				completion = facts.EdgeThrow
			}
			if override {
				expected = "2"
				completion = facts.EdgeNormal
			}
			if r.Completions[0].Values[0].Recipe != a.Constant("int", expected) || r.Completions[0].Kind != completion {
				t.Fatalf("wrong pending result: %+v", r.Completions)
			}
		}
	}
}
func TestControlBranchMemoryPrior(t *testing.T) {
	a := NewRecipeArena()
	s := controlInitial(t, a)
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}},
		facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{controlConstant("one", "1"), {ID: "write", Opcode: facts.OpFieldWrite, Operands: []string{"box", "one"}, FieldID: "count", Type: "int", ValueKind: facts.FlowKindNumeric}}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "right", Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"box"}, Results: []string{"out"}, FieldID: "count", Type: "int", ValueKind: facts.FlowKindNumeric}, controlReturn("return", "out")}})
	r := EvaluateDAG(a, fn, s, TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 {
		t.Fatalf("bad memory join %+v", r)
	}
	value := r.Completions[0].Values[0]
	if value.Unknown {
		t.Fatalf("exact branch memory widened: %+v", value)
	}
	yes, _ := a.Substitute(value.Recipe, map[int]RecipeID{0: a.Constant("bool", "true")})
	no, _ := a.Substitute(value.Recipe, map[int]RecipeID{0: a.Constant("bool", "false")})
	if yes != a.Constant("int", "1") {
		t.Fatal("lost branch write")
	}
	node, _ := a.Lookup(no)
	if node.Kind != KindStorage {
		t.Fatalf("missing prior storage: %+v", node)
	}
}
func TestControlLimitsAndDeterminism(t *testing.T) {
	fn := controlDiamond(t)
	a := NewRecipeArena()
	s := controlInitial(t, a)
	full := EvaluateDAG(a, fn, s, TransferOptions{})
	for limit := 1; limit < full.Work; limit++ {
		r := EvaluateDAG(a, fn, s, TransferOptions{MaxWork: limit})
		if r.Status != TransferLimit || r.Work > limit {
			t.Fatalf("cutoff %d: %+v", limit, r)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if r := EvaluateDAG(a, fn, s, TransferOptions{Context: cancelled}); r.Status != TransferCancelled {
		t.Fatalf("cancellation %+v", r)
	}
	source := fn.FlowFunction()
	for i, j := 0, len(source.Blocks)-1; i < j; i, j = i+1, j-1 {
		source.Blocks[i], source.Blocks[j] = source.Blocks[j], source.Blocks[i]
	}
	reordered, err := CompileFunction(source)
	if err != nil {
		t.Fatal(err)
	}
	r := EvaluateDAG(a, reordered, s, TransferOptions{})
	if !reflect.DeepEqual(full, r) {
		t.Fatalf("order changed result\n%+v\n%+v", full, r)
	}
}
func TestControlDiamondWork(t *testing.T) {
	for _, layers := range []int{1, 6, 12} {
		a := NewRecipeArena()
		blocks := []facts.FlowBlock{}
		id := "entry"
		for i := 0; i < layers; i++ {
			left, right, next := fmt.Sprintf("l%d", i), fmt.Sprintf("r%d", i), fmt.Sprintf("j%d", i)
			blocks = append(blocks, facts.FlowBlock{ID: id, Edges: []facts.FlowEdge{controlEdge(id, left, facts.EdgeTrue), controlEdge(id, right, facts.EdgeFalse)}}, facts.FlowBlock{ID: left, Edges: []facts.FlowEdge{controlEdge(left, next, facts.EdgeNormal)}}, facts.FlowBlock{ID: right, Edges: []facts.FlowEdge{controlEdge(right, next, facts.EdgeNormal)}})
			id = next
		}
		blocks = append(blocks, facts.FlowBlock{ID: id, Instructions: []facts.Instruction{controlReturn("return", "p")}})
		r := EvaluateDAG(a, controlCompile(t, blocks...), controlInitial(t, a), TransferOptions{})
		if r.Status != TransferOK || r.Blocks != 3*layers+1 || r.Work > 400*layers+100 {
			t.Fatalf("nonlinear diamond %d: %+v", layers, r)
		}
	}
}

func TestControlNestedCorrelation(t *testing.T) {
	a := NewRecipeArena()
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Edges: []facts.FlowEdge{controlEdge("left", "good", facts.EdgeTrue), controlEdge("left", "impossible", facts.EdgeFalse)}}, facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{controlReturn("rightReturn")}}, facts.FlowBlock{ID: "good", Instructions: []facts.Instruction{controlReturn("goodReturn")}}, facts.FlowBlock{ID: "impossible", Instructions: []facts.Instruction{{ID: "bad", Opcode: facts.OpUnknown}}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || r.Blocks != 4 || len(r.Completions) != 2 {
		t.Fatalf("lost predicate correlation: %+v", r)
	}
}
func TestControlParallelPhiEdges(t *testing.T) {
	a := NewRecipeArena()
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlConstant("one", "1")}, Edges: []facts.FlowEdge{controlEdge("entry", "join", facts.EdgeTrue), controlEdge("entry", "join", facts.EdgeFalse)}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Results: []string{"out"}, Type: "int", ValueKind: facts.FlowKindNumeric, PhiInputs: []facts.PhiInput{{Predecessor: "entry", Value: "one"}}}, controlReturn("ret", "out")}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 || r.Completions[0].Values[0].Recipe != a.Constant("int", "1") {
		t.Fatalf("parallel edges lost predecessor phi: %+v", r)
	}
}
func TestControlErrorEdgeAndNestedCleanup(t *testing.T) {
	a := NewRecipeArena()
	edge := controlEdge("entry", "cleanup1", facts.EdgeReturnError)
	edge.ErrorTag = "rejected"
	edge.Payload = "one"
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlConstant("one", "1"), controlReturn("ret", "one")}, Edges: []facts.FlowEdge{edge}}, facts.FlowBlock{ID: "cleanup1", Instructions: []facts.Instruction{{ID: "first", Opcode: facts.OpUseResource, Operands: []string{"box"}}}, Edges: []facts.FlowEdge{controlEdge("cleanup1", "cleanup2", facts.EdgeCleanup)}}, facts.FlowBlock{ID: "cleanup2", Instructions: []facts.Instruction{{ID: "second", Opcode: facts.OpUseResource, Operands: []string{"box"}}}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if len(r.Completions) != 1 || r.Completions[0].Kind != facts.EdgeReturnError || r.Completions[0].ErrorTag != "rejected" || len(r.Effects) != 2 {
		t.Fatalf("lost exceptional completion: %+v", r)
	}
	if r.Effects[0].Effect.Instruction != "first" || r.Effects[1].Effect.Instruction != "second" {
		t.Fatal("cleanup reordered")
	}
}
func TestControlLoopIsExplicitlyUnsupported(t *testing.T) {
	a := NewRecipeArena()
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "entry", facts.EdgeNormal)}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferUnsupported || len(r.Completions) != 0 || r.Gaps[0].Reason != "unsupported_loop" {
		t.Fatalf("fabricated loop completion: %+v", r)
	}
}
func TestControlPendingAndNormalJoin(t *testing.T) {
	a := NewRecipeArena()
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{controlConstant("one", "1"), controlReturn("ret", "one")}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeCleanup)}}, facts.FlowBlock{ID: "right", Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "join"})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 2 {
		t.Fatalf("normal completion lost beside pending return: %+v", r)
	}
}

func TestControlOwnershipAndMayAliases(t *testing.T) {
	a := NewRecipeArena()
	s := controlInitial(t, a)
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}},
		facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"box"}}, {ID: "leftBind", Opcode: facts.OpBind, Operands: []string{"box"}, Results: []string{"l"}}}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{{ID: "rightBind", Opcode: facts.OpBind, Operands: []string{"box"}, Results: []string{"r"}}}, Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}},
		facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Results: []string{"out"}, PhiInputs: []facts.PhiInput{{Predecessor: "left", Value: "l"}, {Predecessor: "right", Value: "r"}}}, controlReturn("ret", "out")}})
	r := EvaluateDAG(a, fn, s, TransferOptions{})
	if len(r.Completions) != 1 {
		t.Fatalf("lost completion: %+v", r)
	}
	roots := r.Completions[0].Values[0].Aliases.Roots()
	if len(roots) != 1 || roots[0].Ownership == OwnershipOwned {
		t.Fatalf("may escape retained must ownership: %+v", roots)
	}
	original, _ := s.Value("box")
	if original.Aliases.Roots()[0].Ownership != OwnershipOwned {
		t.Fatal("mutated caller state")
	}
}
func TestControlMemoryWildcardAfterJoin(t *testing.T) {
	a := NewRecipeArena()
	s := controlInitial(t, a)
	path := []facts.AliasPathSegment{{Kind: "index", Name: "0"}}
	dynamic := []facts.AliasPathSegment{{Kind: "index", Dynamic: true}}
	write := func(id, value string, p []facts.AliasPathSegment) facts.Instruction {
		return facts.Instruction{ID: id, Opcode: facts.OpFieldWrite, Operands: []string{"box", value}, Type: "int", ValueKind: facts.FlowKindNumeric, AccessPath: p}
	}
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlConstant("one", "1"), controlConstant("two", "2")}, Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{write("exact", "one", path)}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{write("other", "one", path)}, Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{write("wild", "two", dynamic), {ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"box"}, Results: []string{"out"}, Type: "int", ValueKind: facts.FlowKindNumeric, AccessPath: path}, controlReturn("ret", "out")}})
	r := EvaluateDAG(a, fn, s, TransferOptions{})
	if len(r.Completions) != 1 || !r.Completions[0].Values[0].Unknown {
		t.Fatalf("later wildcard ignored: %+v", r)
	}
}
func TestControlMissingFormalPhiRemainsUnknown(t *testing.T) {
	a := NewRecipeArena()
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Results: []string{"out"}, Type: "Box", ValueKind: facts.FlowKindReference, PhiInputs: []facts.PhiInput{{Predecessor: "entry", Value: "box"}}}, controlReturn("ret", "out")}})
	r := EvaluateDAG(a, fn, NewTransferState(), TransferOptions{})
	if r.Status != TransferPartial || !r.Completions[0].Values[0].Unknown {
		t.Fatalf("missing input invented: %+v", r)
	}
}

func TestControlBooleanRecipeCorrelation(t *testing.T) {
	a := NewRecipeArena()
	work := 0
	g := newControlGuards(a, func(n int) bool { work += n; return true })
	p, _ := a.Formal(0, "bool")
	q, _ := a.Formal(1, "bool")
	neg, _ := a.Primitive("bool", ModeBoolean, "!", []RecipeID{p})
	both, _ := a.Primitive("bool", ModeBoolean, "&&", []RecipeID{p, q})
	selection, _ := a.Select("bool", p, q, a.Constant("bool", "false"))
	if g.atom(both) != g.atom(selection) {
		t.Fatal("equivalent boolean recipe lost correlation")
	}
	if g.and(g.atom(p), g.atom(neg)) != guardFalse {
		t.Fatal("negated branch remained reachable")
	}
	if g.or(g.atom(p), g.atom(neg)) != guardTrue {
		t.Fatal("complementary branches did not reconverge")
	}
	if work > 100 {
		t.Fatalf("boolean DAG revisits: %d", work)
	}
}

func TestControlSimultaneousPhis(t *testing.T) {
	a := NewRecipeArena()
	phi := func(id, left, right string) facts.Instruction {
		return facts.Instruction{ID: id, Opcode: facts.OpPhi, Results: []string{id}, Type: "int", ValueKind: facts.FlowKindNumeric, PhiInputs: []facts.PhiInput{{Predecessor: "left", Value: left}, {Predecessor: "right", Value: right}}}
	}
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Instructions: []facts.Instruction{controlConstant("one", "1"), controlConstant("two", "2")}, Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "right", Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{phi("x", "one", "two"), phi("y", "two", "one"), controlReturn("ret", "x", "y")}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 {
		t.Fatalf("phi tuple: %+v", r)
	}
	for i, v := range r.Completions[0].Values {
		result, err := a.Substitute(v.Recipe, map[int]RecipeID{0: a.Constant("bool", "true")})
		if err != nil || result != a.Constant("int", fmt.Sprint(i+1)) {
			t.Fatalf("phi read another phi assignment: %+v", v)
		}
	}
}
func TestControlErrorEdgePermutation(t *testing.T) {
	a := NewRecipeArena()
	edges := []facts.FlowEdge{}
	for _, tag := range []string{"a", "b", "c"} {
		edge := controlEdge("entry", "cleanup", facts.EdgeReturnError)
		edge.ErrorTag = tag
		edges = append(edges, edge)
	}
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: edges}, facts.FlowBlock{ID: "cleanup"})
	first := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if len(first.Completions) != 3 {
		t.Fatalf("exception alternatives disappeared: %+v", first)
	}
	edges[0], edges[2] = edges[2], edges[0]
	second := EvaluateDAG(a, controlCompile(t, facts.FlowBlock{ID: "entry", Edges: edges}, facts.FlowBlock{ID: "cleanup"}), controlInitial(t, a), TransferOptions{})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("parallel exception order changed result")
	}
}

func TestControlRecordPhiRetainsFieldRecipes(t *testing.T) {
	a := NewRecipeArena()
	pack := func(id, value string) facts.Instruction {
		return facts.Instruction{ID: id, Opcode: facts.OpPack, Type: "Pair", ValueKind: facts.FlowKindRecord, Results: []string{id}, FieldBindings: []facts.FieldBinding{{Field: "number", Value: value}, {Field: "reference", Value: "box"}}}
	}
	fn := controlCompile(t, facts.FlowBlock{ID: "entry", Edges: []facts.FlowEdge{controlEdge("entry", "left", facts.EdgeTrue), controlEdge("entry", "right", facts.EdgeFalse)}}, facts.FlowBlock{ID: "left", Instructions: []facts.Instruction{controlConstant("one", "1"), pack("l", "one")}, Edges: []facts.FlowEdge{controlEdge("left", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "right", Instructions: []facts.Instruction{controlConstant("two", "2"), pack("r", "two")}, Edges: []facts.FlowEdge{controlEdge("right", "join", facts.EdgeNormal)}}, facts.FlowBlock{ID: "join", Instructions: []facts.Instruction{{ID: "phi", Opcode: facts.OpPhi, Type: "Pair", ValueKind: facts.FlowKindRecord, Results: []string{"pair"}, PhiInputs: []facts.PhiInput{{Predecessor: "left", Value: "l"}, {Predecessor: "right", Value: "r"}}}, {ID: "number", Opcode: facts.OpFieldRead, Operands: []string{"pair"}, Results: []string{"out"}, FieldID: "number", Type: "int", ValueKind: facts.FlowKindNumeric}, {ID: "reference", Opcode: facts.OpFieldRead, Operands: []string{"pair"}, Results: []string{"ref"}, FieldID: "reference", Type: "Box", ValueKind: facts.FlowKindReference}, controlReturn("ret", "out", "ref")}})
	r := EvaluateDAG(a, fn, controlInitial(t, a), TransferOptions{})
	if r.Status != TransferOK || len(r.Completions) != 1 {
		t.Fatalf("record phi widened: %+v", r)
	}
	values := r.Completions[0].Values
	for _, choice := range []string{"true", "false"} {
		recipe, err := a.Substitute(values[0].Recipe, map[int]RecipeID{0: a.Constant("bool", choice)})
		want := "1"
		if choice == "false" {
			want = "2"
		}
		if err != nil || recipe != a.Constant("int", want) {
			t.Fatalf("field phi: %s %v", recipe, err)
		}
	}
	if len(values[0].Aliases.Roots()) != 0 || len(values[1].Aliases.Roots()) != 1 || values[1].Aliases.Roots()[0].ID != "A" {
		t.Fatalf("field aliases mixed: %+v", values)
	}
}

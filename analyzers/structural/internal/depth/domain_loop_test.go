package depth

import "testing"

func TestNumericLoopReductionUsesBoundedDefinition(t *testing.T) {
	a := NewRecipeArena()
	n, _ := a.Formal(0, "int")
	zero, one := a.Constant("int", "0"), a.Constant("int", "1")
	index, _ := a.RecurrenceRef("int", "loop0/index", nil)
	sum, _ := a.RecurrenceRef("int", "loop0/sum", nil)
	nextIndex, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{index, one})
	nextSum, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{sum, index})
	condition, _ := a.Primitive("bool", ModeInteger, "<", []RecipeID{index, n})
	bindings := []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop0/index", Type: "int", Initial: zero, Update: nextIndex}, {Kind: KindNumeric, Slot: "loop0/sum", Type: "int", Initial: zero, Update: nextSum}}
	recipe, err := a.NumericLoop("int", "loop0/sum", condition, bindings)
	if err != nil {
		t.Fatal(err)
	}
	size := a.NodeCount()
	for i := 0; i < 100; i++ {
		if a.Classify(recipe) != TransformationPrimitive {
			t.Fatal("numeric reduction not recognized")
		}
	}
	if a.NodeCount() != size || a.BuildOutcome(recipe).UniqueNodes > 16 {
		t.Fatal("recurrence expanded during analysis")
	}
	bindings[0], bindings[1] = bindings[1], bindings[0]
	reordered, err := a.NumericLoop("int", "loop0/sum", condition, bindings)
	if err != nil || reordered != recipe {
		t.Fatal("binding order changed recurrence identity")
	}
	substituted, err := a.Substitute(recipe, map[int]RecipeID{0: a.Constant("int", "10")})
	if err != nil || a.Classify(substituted) != TransformationConstant {
		t.Fatalf("constant actual did not remove input dependency: %v", err)
	}
}
func TestNumericLoopDoesNotGrantCreditForIdentity(t *testing.T) {
	a := NewRecipeArena()
	input, _ := a.Formal(0, "int")
	condition, _ := a.Formal(1, "bool")
	self, _ := a.RecurrenceRef("int", "loop0/value", nil)
	recipe, err := a.NumericLoop("int", "loop0/value", condition, []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop0/value", Type: "int", Initial: input, Update: self}})
	if err != nil || a.Classify(recipe) != TransformationIdentity {
		t.Fatalf("identity loop gained X: %v", err)
	}
}
func TestNumericLoopUnknownReferenceRemainsUnknown(t *testing.T) {
	a := NewRecipeArena()
	input, _ := a.Formal(0, "int")
	foreign, _ := a.RecurrenceRef("int", "other/slot", nil)
	step, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{input, foreign})
	recipe, err := a.NumericLoop("int", "loop0/value", a.Constant("bool", "true"), []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop0/value", Type: "int", Initial: input, Update: step}})
	if err != nil || a.Classify(recipe) != TransformationUnknown {
		t.Fatalf("unbound recurrence gained precision: %v", err)
	}
}
func TestNumericLoopKnownZeroIterationsPrunesUpdate(t *testing.T) {
	a := NewRecipeArena()
	input, _ := a.Formal(0, "int")
	foreign, _ := a.RecurrenceRef("int", "other/slot", nil)
	recipe, err := a.NumericLoop("int", "loop0/value", a.Constant("bool", "false"), []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop0/value", Type: "int", Initial: input, Update: foreign}})
	if err != nil || a.Classify(recipe) != TransformationIdentity {
		t.Fatalf("dead update affected zero-iteration result: %v", err)
	}
}

func TestNumericLoopRejectsCircularInitialKnowledge(t *testing.T) {
	a := NewRecipeArena()
	self, _ := a.RecurrenceRef("int", "loop0/value", nil)
	step, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{self, a.Constant("int", "1")})
	recipe, err := a.NumericLoop("int", "loop0/value", a.Constant("bool", "true"), []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop0/value", Type: "int", Initial: self, Update: step}})
	if err != nil || a.Classify(recipe) != TransformationUnknown {
		t.Fatalf("circular initial value became known: %v", err)
	}
}

func TestNumericLoopSubstitutionPrunesZeroIterationBody(t *testing.T) {
	a := NewRecipeArena()
	initial, _ := a.Formal(0, "int")
	limit, _ := a.Formal(1, "int")
	unused, _ := a.Formal(2, "int")
	index, _ := a.RecurrenceRef("int", "loop/index", nil)
	sum, _ := a.RecurrenceRef("int", "loop/sum", nil)
	nextIndex, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{index, a.Constant("int", "1")})
	nextSum, _ := a.Primitive("int", ModeInteger, "+", []RecipeID{sum, unused})
	condition, _ := a.Primitive("bool", ModeInteger, "<", []RecipeID{index, limit})
	recipe, err := a.NumericLoop("int", "loop/sum", condition, []NumericLoopBinding{{Kind: KindNumeric, Slot: "loop/index", Type: "int", Initial: a.Constant("int", "0"), Update: nextIndex}, {Kind: KindNumeric, Slot: "loop/sum", Type: "int", Initial: initial, Update: nextSum}})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := a.Formal(7, "int")
	out, err := a.Substitute(recipe, map[int]RecipeID{0: actual, 1: a.Constant("int", "0")})
	if err != nil || out != actual {
		t.Fatalf("zero-iteration loop demanded unused body input: %s %v", out, err)
	}
}

func TestIntegerComparisonFoldingIsExactAndModeSpecific(t *testing.T) {
	a := NewRecipeArena()
	for _, test := range []struct{ left, right, op, want string }{{"0", "0", "<", "false"}, {"-1", "0", "<", "true"}, {"18446744073709551615", "18446744073709551614", ">", "true"}} {
		value, err := a.Primitive("bool", ModeInteger, test.op, []RecipeID{a.Constant("int", test.left), a.Constant("int", test.right)})
		if err != nil || value != a.Constant("bool", test.want) {
			t.Fatalf("comparison %v: %s %v", test, value, err)
		}
	}
	for _, test := range []struct {
		mode         ArithmeticMode
		typ, literal string
	}{{ModeInteger, "int", "010"}, {ModeFloat, "float64", "NaN"}, {ModeJSNumber, "number", "NaN"}} {
		operand := a.Constant(test.typ, test.literal)
		value, err := a.Primitive("bool", test.mode, "==", []RecipeID{operand, operand})
		if err != nil {
			t.Fatal(err)
		}
		node, _ := a.Lookup(value)
		if node.Kind == KindConstant {
			t.Fatalf("unsafe folding for %v", test)
		}
	}
}

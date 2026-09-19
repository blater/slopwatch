package depth

import "testing"

func TestTextPrimitiveComposition(t *testing.T) {
	a := NewRecipeArena()
	left := mustFormal(t, a, 0, "string")
	right := mustFormal(t, a, 1, "string")
	if got := mustPrimitive(t, a, "string", ModeText, "+", []RecipeID{left, a.Constant("string", `""`)}); got != left {
		t.Fatalf("left text identity = %s", got)
	}
	if got := mustPrimitive(t, a, "string", ModeText, "+", []RecipeID{a.Constant("string", `""`), right}); got != right {
		t.Fatalf("right text identity = %s", got)
	}
	literal := mustPrimitive(t, a, "string", ModeText, "+", []RecipeID{a.Constant("string", `"a"`), a.Constant("string", `"b"`)})
	if literal != a.Constant("string", `"ab"`) || a.Classify(literal) != TransformationConstant {
		t.Fatalf("text literal fold = %s", literal)
	}
	joined := mustPrimitive(t, a, "string", ModeText, "+", []RecipeID{left, right})
	if a.Classify(joined) != TransformationPrimitive || !textConcatenation(a, joined) {
		t.Fatalf("text transformation = %s", joined)
	}
	if unknown, _ := a.Primitive("string", ModeText, "-", []RecipeID{left, right}); a.Classify(unknown) != TransformationUnknown {
		t.Fatalf("unsupported text operator was accepted: %s", unknown)
	}
}

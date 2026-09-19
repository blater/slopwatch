package depth

import "testing"

func mustRecipe(t *testing.T, id RecipeID, err error) RecipeID {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected recipe error: %v", err)
	}
	return id
}

func mustFormal(t *testing.T, a *RecipeArena, index int, typ string) RecipeID {
	id, err := a.Formal(index, typ)
	return mustRecipe(t, id, err)
}
func mustPrimitive(t *testing.T, a *RecipeArena, typ string, mode ArithmeticMode, op string, operands []RecipeID) RecipeID {
	id, err := a.Primitive(typ, mode, op, operands)
	return mustRecipe(t, id, err)
}
func mustPack(t *testing.T, a *RecipeArena, typ string, fields []FieldRecipeBinding) RecipeID {
	id, err := a.Pack(typ, fields)
	return mustRecipe(t, id, err)
}
func mustSelect(t *testing.T, a *RecipeArena, typ string, predicate, whenTrue, whenFalse RecipeID) RecipeID {
	id, err := a.Select(typ, predicate, whenTrue, whenFalse)
	return mustRecipe(t, id, err)
}
func mustSubstitute(t *testing.T, a *RecipeArena, root RecipeID, bindings map[int]RecipeID) RecipeID {
	id, err := a.Substitute(root, bindings)
	return mustRecipe(t, id, err)
}

func TestRecipesSubstituteAndClassify(t *testing.T) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "int")
	one := a.Constant("int", "1")
	actual := mustFormal(t, a, 7, "int")
	direct := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{actual, one})
	hformal := mustFormal(t, a, 0, "int")
	helper := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{hformal, one})
	inst, err := a.Substitute(helper, map[int]RecipeID{0: actual})
	if err != nil || inst != direct {
		t.Fatalf("helper recipe did not canonicalize to direct recipe: %s %v", inst, err)
	}
	if got := a.Classify(x); got != TransformationIdentity {
		t.Fatalf("identity class = %s", got)
	}
	if got := a.Classify(direct); got != TransformationPrimitive {
		t.Fatalf("primitive class = %s", got)
	}
	formal1 := mustFormal(t, a, 1, "int")
	formal10 := mustFormal(t, a, 10, "int")
	pair := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{formal1, formal10})
	leftActual := mustFormal(t, a, 20, "int")
	rightActual := mustFormal(t, a, 21, "int")
	firstBinding := mustSubstitute(t, a, pair, map[int]RecipeID{1: leftActual, 10: rightActual})
	secondBinding := mustSubstitute(t, a, pair, map[int]RecipeID{10: rightActual, 1: leftActual})
	if firstBinding != secondBinding {
		t.Fatal("binding map order changed recipe identity")
	}
	zero := a.Constant("int", "0")
	identity := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{x, zero})
	if identity != x || a.Classify(identity) != TransformationIdentity {
		t.Fatal("integer x+0 was not an identity")
	}
	floatX := mustFormal(t, a, 1, "float64")
	floatZero := a.Constant("float64", "0")
	floatAdd := mustPrimitive(t, a, "float64", ModeFloat, "+", []RecipeID{floatX, floatZero})
	if floatAdd == floatX || a.Classify(floatAdd) != TransformationPrimitive {
		t.Fatal("float x+0 was incorrectly simplified")
	}
	jsAdd := mustPrimitive(t, a, "number", ModeJSNumber, "|", []RecipeID{x, zero})
	if jsAdd == x || a.Classify(jsAdd) != TransformationPrimitive {
		t.Fatal("JS x|0 was incorrectly simplified")
	}
}

func TestRecipeCanonicalPackSelectAndDefensiveSnapshots(t *testing.T) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "int")
	one := a.Constant("int", "1")
	p1 := mustPack(t, a, "record", []FieldRecipeBinding{{Field: "b", Recipe: one}, {Field: "a", Recipe: x}})
	p2 := mustPack(t, a, "record", []FieldRecipeBinding{{Field: "a", Recipe: x}, {Field: "b", Recipe: one}})
	if p1 != p2 {
		t.Fatal("pack binding order changed identity")
	}
	n, ok := a.Lookup(p1)
	if !ok {
		t.Fatal("missing pack")
	}
	n.Fields[0].Field = "changed"
	n2, _ := a.Lookup(p1)
	if n2.Fields[0].Field == "changed" {
		t.Fatal("lookup exposed mutable arena storage")
	}
	pred := a.Constant("bool", "true")
	selected := mustSelect(t, a, "int", pred, x, one)
	if selected != x {
		t.Fatal("constant predicate was not pruned")
	}
	if _, err := a.Pack("record", []FieldRecipeBinding{{Field: "a", Recipe: x}, {Field: "a", Recipe: one}}); err == nil {
		t.Fatal("duplicate pack field was accepted")
	}
	boolFormal := mustFormal(t, a, 30, "bool")
	unknown, _ := a.Primitive("int", ArithmeticMode("opaque"), "+", []RecipeID{x, one})
	route := mustSelect(t, a, "int", boolFormal, one, unknown)
	knownRoute := mustSubstitute(t, a, route, map[int]RecipeID{30: pred})
	if knownRoute != one {
		t.Fatal("constant true selection traversed an unreachable unknown arm")
	}
}

func TestTypedPrimitiveIdentitiesAndUnknowns(t *testing.T) {
	a := NewRecipeArena()
	b := mustFormal(t, a, 0, "bool")
	trueValue := a.Constant("bool", "true")
	falseValue := a.Constant("bool", "false")
	if got := mustPrimitive(t, a, "bool", ModeBoolean, "&&", []RecipeID{b, trueValue}); got != b {
		t.Fatal("boolean x&&true was not an identity")
	}
	if got := mustPrimitive(t, a, "bool", ModeBoolean, "||", []RecipeID{b, falseValue}); got != b {
		t.Fatal("boolean x||false was not an identity")
	}
	if got := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{trueValue}); a.Classify(got) != TransformationConstant {
		t.Fatal("boolean inversion of a constant was not constant")
	}
	x := mustFormal(t, a, 1, "int")
	zero := a.Constant("int", "0")
	comparison := mustPrimitive(t, a, "bool", ModeInteger, "<", []RecipeID{x, zero})
	if a.Classify(comparison) != TransformationPrimitive {
		t.Fatal("numeric comparison was not classified as a transformation")
	}
	floatX := mustFormal(t, a, 2, "float64")
	if _, err := a.Primitive("int", ModeInteger, "+", []RecipeID{floatX, zero}); err == nil {
		t.Fatal("integer operation accepted float operand")
	}
	missing, err := a.Substitute(x, map[int]RecipeID{})
	if err == nil || a.Classify(missing) != TransformationUnknown {
		t.Fatal("missing formal became an exact fact")
	}
	if _, err := a.Primitive("int", ArithmeticMode("overloaded"), "+", []RecipeID{x, zero}); err == nil {
		t.Fatal("unknown arithmetic mode became exact")
	}
}

func TestDiamondRecipeWorkIsBounded(t *testing.T) {
	a := NewRecipeArena()
	current := mustFormal(t, a, 0, "int")
	predicate := mustFormal(t, a, 1, "bool")
	for layer := 0; layer < 25; layer++ {
		constant := a.Constant("int", string(rune('2'+layer%7)))
		left := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{current, constant})
		right := mustPrimitive(t, a, "int", ModeInteger, "-", []RecipeID{current, constant})
		current = mustSelect(t, a, "int", predicate, left, right)
	}
	out := a.BuildOutcome(current)
	if out.Unknown || out.UniqueNodes < 25 || out.ElementVisits > out.UniqueNodes*4 {
		t.Fatalf("diamond outcome was not bounded: %+v", out)
	}
	if a.Classify(current) != TransformationPrimitive {
		t.Fatal("diamond transformation lost classification")
	}
	actual := mustFormal(t, a, 2, "int")
	inst := mustSubstitute(t, a, current, map[int]RecipeID{0: actual, 1: predicate})
	if a.Classify(inst) != TransformationPrimitive {
		t.Fatal("diamond substitution lost classification")
	}
}

func TestEmptyResolvedRecipeFieldsAreUnknown(t *testing.T) {
	a := NewRecipeArena()
	if id, err := a.Formal(0, ""); err == nil || a.Classify(id) != TransformationUnknown {
		t.Fatal("empty formal type became exact")
	}
	x := mustFormal(t, a, 1, "int")
	if id, err := a.FieldRead("int", "", x); err == nil || a.Classify(id) != TransformationUnknown {
		t.Fatal("empty field identity became exact")
	}
}

func TestAliasBoundsJoinAndUpdates(t *testing.T) {
	root := NewAliasRoot("field:State", true, OwnershipOwned)
	known := NewAliasSet(root)
	if !known.CanStrongUpdate() {
		t.Fatal("singleton owned root should permit strong update")
	}
	wildcard := NewAliasSet(NewAliasRoot("array:items", true, OwnershipOwned, DynamicIndexSegment()))
	exactWildcard := NewAliasSet(NewAliasRoot("array:items", true, OwnershipOwned, IndexSegment("*")))
	if wildcard.CanStrongUpdate() || !exactWildcard.CanStrongUpdate() || aliasKey(wildcard.Roots()[0]) == aliasKey(exactWildcard.Roots()[0]) {
		t.Fatal("dynamic and literal wildcard paths were conflated for updates")
	}
	wide := make([]AliasRoot, 17)
	for i := range wide {
		wide[i] = NewAliasRoot("alloc:"+string(rune('a'+i)), true, OwnershipOwned)
	}
	w := NewAliasSet(wide...)
	if w.IsKnown() || len(w.SupportingRoots()) != 17 || len(w.Reasons()) == 0 || w.Reasons()[0].Code != AliasLimitReason {
		t.Fatal("wide alias set did not widen deterministically")
	}
	deep := known.Extend(FieldSegment("a")).Extend(FieldSegment("b")).Extend(FieldSegment("c")).Extend(FieldSegment("d"))
	if !deep.IsKnown() || len(deep.Roots()[0].Path) != 4 {
		t.Fatal("four path steps were not retained")
	}
	if five := deep.Extend(DynamicIndexSegment()); five.IsKnown() || five.Reasons()[0].Code != AccessPathLimitReason {
		t.Fatal("fifth path step did not widen")
	}
	borrowed := NewAliasSet(NewAliasRoot("field:State", true, OwnershipBorrowed))
	joined := known.Join(borrowed)
	if joined.Roots()[0].Ownership != OwnershipUnknown || joined.CanStrongUpdate() {
		t.Fatal("ownership disagreement remained strong-update eligible")
	}
	unknown := UnknownAliasSet(UnknownInputReason)
	if unknown.Join(known).IsKnown() || unknown.Join(known).Complete() {
		t.Fatal("unknown input recovered precision after join")
	}
	store := NewMemoryStore()
	store.Write(root, nil, NewValue("known", "Box", KindReference, nil, known), true)
	store.Write(borrowed.Roots()[0], nil, NewValue("borrowed", "Box", KindReference, nil, borrowed), false)
	stored, _ := store.Read(root, nil)
	got := stored.Aliases
	if len(got.Roots()) != 1 || got.Roots()[0].Ownership != OwnershipUnknown {
		t.Fatal("weak update did not preserve prior alias")
	}
	formal := NewAliasSet(FormalRoot(0, true, OwnershipBorrowed, FieldSegment("next")))
	actual := NewAliasSet(NewAliasRoot("alloc:obj", true, OwnershipOwned))
	bound := formal.SubstituteRoots(map[int]AliasSet{0: actual})
	if !bound.IsKnown() || len(bound.Roots()) != 1 || len(bound.Roots()[0].Path) != 1 || bound.Roots()[0].Path[0].Name != "next" {
		t.Fatal("formal path was not preserved during root substitution")
	}
	if got := bound.Extend(FieldSegment("deep")).Extend(FieldSegment("more")).Extend(FieldSegment("last")).Extend(FieldSegment("overflow")); got.IsKnown() {
		t.Fatal("substituted path did not hit depth cap")
	}
	scalar := NewValue("scalar", "int", KindNumeric, nil, known)
	if len(scalar.Aliases.Roots()) != 0 {
		t.Fatal("numeric value retained mutable aliases")
	}
	ref := NewValue("ref", "Buffer", KindReference, nil, known)
	packed := PackValues("pack", "Record", []DomainValue{scalar, ref})
	if len(packed.Aliases.Roots()) != 1 || packed.Aliases.Roots()[0].ID != root.ID {
		t.Fatal("nested reference pack did not retain aliases")
	}
	joinedValue := scalar.Join(ref)
	if !joinedValue.Unknown || joinedValue.Recipe != UnknownRecipeID || len(joinedValue.Dependencies) != 0 {
		t.Fatal("incompatible value join recovered exact recipe")
	}
	other := PrimitiveResult("other", "int", KindNumeric, []string{"dep"})
	if left, right := scalar.Join(other).Join(scalar), scalar.Join(other.Join(scalar)); left.Recipe != right.Recipe || !left.Unknown || left.Recipe != UnknownRecipeID {
		t.Fatal("joined recipe identity was not associative")
	}
	wideValues := make([]DomainValue, 17)
	for i := range wideValues {
		wideValues[i] = NewValue("ref", "Buffer", KindReference, nil, NewAliasSet(wide[i]))
	}
	widePack := PackValues("wide-pack", "Record", wideValues)
	if !widePack.Unknown || len(widePack.Reasons) == 0 {
		t.Fatal("packed alias widening did not propagate uncertainty")
	}
}

func TestOutcomeLimitAndDiamondAccounting(t *testing.T) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "int")
	one := a.Constant("int", "1")
	left := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{x, one})
	right := mustPrimitive(t, a, "int", ModeInteger, "*", []RecipeID{left, one})
	pack := mustPack(t, a, "record", []FieldRecipeBinding{{Field: "a", Recipe: right}, {Field: "b", Recipe: right}})
	out := a.BuildOutcome(pack)
	if out.UniqueNodes != 4 || out.ElementVisits <= out.UniqueNodes {
		t.Fatalf("diamond accounting = %+v", out)
	}
	plus := mustPrimitive(t, a, "int", ModeInteger, "+", []RecipeID{x, x})
	if got := a.BuildOutcome(plus); got.ElementVisits != 4 {
		t.Fatalf("operand position accounting = %+v", got)
	}
	limited := NewRecipeArena(RecipeArenaOptions{MaxNodes: 2})
	lx := mustFormal(t, limited, 0, "int")
	lo := limited.Constant("int", "1")
	lr := mustPrimitive(t, limited, "int", ModeInteger, "+", []RecipeID{lx, lo})
	if result := limited.BuildOutcome(lr); !result.Unknown || result.Reason != ReasonRecipeLimit {
		t.Fatalf("limit result = %+v", result)
	}
}

func TestOutcomeStopsDuringTraversalAtNodeLimit(t *testing.T) {
	a := NewRecipeArena(RecipeArenaOptions{MaxNodes: 8})
	value, err := a.Formal(0, "int")
	if err != nil {
		t.Fatal(err)
	}
	one := a.Constant("int", "1")
	for i := 0; i < 5000; i++ {
		value, err = a.Primitive("int", ModeInteger, "+", []RecipeID{value, one})
		if err != nil {
			t.Fatal(err)
		}
	}
	outcome := a.BuildOutcome(value)
	if !outcome.Unknown || outcome.Reason != ReasonRecipeLimit || outcome.UniqueNodes > 9 || outcome.ElementVisits > 32 {
		t.Fatalf("walked oversized recipe before applying cap: %+v", outcome)
	}
}

func TestBooleanIdentityPreservesCoercionsAndTypes(t *testing.T) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "number")
	not := mustPrimitive(t, a, "boolean", ModeJSNumber, "!", []RecipeID{x})
	twice := mustPrimitive(t, a, "boolean", ModeJSNumber, "!", []RecipeID{not})
	if twice == x {
		t.Fatal("JS truthiness erased")
	}
	b := mustFormal(t, a, 1, "bool")
	converted := mustPrimitive(t, a, "boolean", ModeBoolean, "==", []RecipeID{b, a.Constant("bool", "true")})
	if converted == b {
		t.Fatal("result type erased")
	}
	negated := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{b})
	identity := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{negated})
	if identity != b {
		t.Fatal("primitive Boolean double negation is not identity")
	}
}

func TestBooleanDoubleNegationPreservesInnerConversion(t *testing.T) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "boolean")
	inner := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{x})
	outer := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{inner})
	node, ok := a.Lookup(outer)
	if outer == x || !ok || node.Type != "bool" {
		t.Fatal("double negation erased inner conversion")
	}
}

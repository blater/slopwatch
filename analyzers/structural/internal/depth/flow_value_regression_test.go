package depth

import (
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func reviewRoot(id string) AliasRoot {
	r := NewAliasRoot(id, true, OwnershipOwned)
	r.Kind = "allocation"
	return r
}
func reviewSeed(t *testing.T, a *RecipeArena, s *TransferState, id string, roots ...AliasRoot) {
	t.Helper()
	recipe, err := a.Formal(0, "Box")
	if err != nil {
		t.Fatal(err)
	}
	s.Seed(id, NewValue(recipe, "Box", KindReference, nil, NewAliasSet(roots...)))
}
func reviewNumber(a *RecipeArena, s *TransferState, id, value string) {
	s.Seed(id, ScalarValue(a.Constant("int", value), "int", KindNumeric))
}
func reviewWrite(a *RecipeArena, s *TransferState, receiver, value, field string, path ...facts.AliasPathSegment) TransferResult {
	return Apply(a, s, facts.Instruction{ID: "write", Opcode: facts.OpFieldWrite, Operands: []string{receiver, value}, Type: "int", ValueKind: facts.FlowKindNumeric, FieldID: field, AccessPath: path}, TransferOptions{})
}
func reviewRead(a *RecipeArena, s *TransferState, receiver, result, field string, path ...facts.AliasPathSegment) DomainValue {
	Apply(a, s, facts.Instruction{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{receiver}, Results: []string{result}, Type: "int", ValueKind: facts.FlowKindNumeric, FieldID: field, AccessPath: path}, TransferOptions{})
	v, _ := s.Value(result)
	return v
}
func TestTransferRegressionEmptyConstant(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	r := Apply(a, s, facts.Instruction{ID: "empty", Opcode: facts.OpConstant, Results: []string{"v"}, Type: "string", ValueKind: facts.FlowKindString, Value: &facts.Value{Type: "string", ValueKind: facts.FlowKindString, Constant: "", Concepts: []string{"text"}}}, TransferOptions{})
	v, ok := s.Value("v")
	if r.Status != TransferOK || !ok || v.Unknown || v.Recipe != a.Constant("string", "") {
		t.Fatalf("empty literal lost: %+v %+v", r, v)
	}
	if !reflect.DeepEqual(v.Concepts, []string{"text"}) {
		t.Fatalf("constant concepts lost: %+v", v)
	}
}
func TestTransferRegressionUnknownScalarHasNoAlias(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	Apply(a, s, facts.Instruction{ID: "missing", Opcode: facts.OpConstant, Results: []string{"v"}, Type: "int", ValueKind: facts.FlowKindNumeric}, TransferOptions{})
	v, ok := s.Value("v")
	if !ok || !v.Unknown || !v.Aliases.IsKnown() || len(v.Aliases.Roots()) != 0 {
		t.Fatalf("unknown scalar invented alias: %+v", v)
	}
}
func TestTransferRegressionUnknownReferenceRetainsAliasGap(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	Apply(a, s, facts.Instruction{ID: "missing", Opcode: facts.OpUnknown, Results: []string{"v"}, Type: "Box", ValueKind: facts.FlowKindReference}, TransferOptions{})
	v, ok := s.Value("v")
	if !ok || !v.Unknown || !v.Aliases.IsUnknown() {
		t.Fatalf("unknown reference lost alias uncertainty: %+v", v)
	}
}
func TestTransferRegressionStrongWriteRead(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "box", reviewRoot("A"))
	reviewNumber(a, s, "n", "7")
	r := reviewWrite(a, s, "box", "n", "Box.count")
	v := reviewRead(a, s, "box", "out", "Box.count")
	if r.Status != TransferOK || v.Unknown || v.Recipe != a.Constant("int", "7") || len(v.Aliases.Roots()) != 0 {
		t.Fatalf("strong write/read failed: %+v %+v", r, v)
	}
}
func TestTransferRegressionTwoPossibleTargets(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	left, right := reviewRoot("A"), reviewRoot("B")
	reviewSeed(t, a, s, "a", left)
	reviewSeed(t, a, s, "b", right)
	reviewSeed(t, a, s, "either", left, right)
	reviewNumber(a, s, "one", "1")
	reviewNumber(a, s, "two", "2")
	reviewNumber(a, s, "three", "3")
	reviewWrite(a, s, "a", "one", "Box.count")
	reviewWrite(a, s, "b", "two", "Box.count")
	reviewWrite(a, s, "either", "three", "Box.count")
	for _, id := range []string{"a", "b"} {
		v := reviewRead(a, s, id, "out", "Box.count")
		if !v.Unknown {
			t.Fatalf("possible write treated as exact/no write for %s: %+v", id, v)
		}
	}
}
func TestTransferRegressionWildcardOrdering(t *testing.T) {
	for _, wildcardFirst := range []bool{true, false} {
		a := NewRecipeArena()
		s := NewTransferState()
		reviewSeed(t, a, s, "array", reviewRoot("A"))
		reviewNumber(a, s, "one", "1")
		reviewNumber(a, s, "two", "2")
		wild := facts.AliasPathSegment{Kind: "index", Dynamic: true}
		exact := facts.AliasPathSegment{Kind: "index", Name: "0"}
		if wildcardFirst {
			reviewWrite(a, s, "array", "one", "", wild)
			reviewWrite(a, s, "array", "two", "", exact)
		} else {
			reviewWrite(a, s, "array", "two", "", exact)
			reviewWrite(a, s, "array", "one", "", wild)
		}
		v := reviewRead(a, s, "array", "out", "", exact)
		if wildcardFirst && (v.Unknown || v.Recipe != a.Constant("int", "2")) {
			t.Fatalf("older wildcard poisoned later exact write: %+v", v)
		}
		if !wildcardFirst && !v.Unknown {
			t.Fatalf("later wildcard write ignored: %+v", v)
		}
	}
}
func TestTransferRegressionDynamicReadIsNotStrong(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "array", reviewRoot("A"))
	reviewNumber(a, s, "one", "1")
	wild := facts.AliasPathSegment{Kind: "index", Dynamic: true}
	reviewWrite(a, s, "array", "one", "", wild)
	v := reviewRead(a, s, "array", "out", "", wild)
	if !v.Unknown {
		t.Fatalf("unrelated dynamic read assumed same element as dynamic write: %+v", v)
	}
}
func TestTransferRegressionLiteralStarIsExact(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "array", reviewRoot("A"))
	reviewNumber(a, s, "one", "1")
	star := facts.AliasPathSegment{Kind: "index", Name: "*"}
	zero := facts.AliasPathSegment{Kind: "index", Name: "0"}
	reviewWrite(a, s, "array", "one", "", star)
	v := reviewRead(a, s, "array", "out", "", zero)
	if v.Recipe == a.Constant("int", "1") || v.Unknown {
		t.Fatalf("literal star treated as wildcard: %+v", v)
	}
}
func TestTransferRegressionUnknownMutationInvalidatesRead(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("A"))
	reviewNumber(a, s, "one", "1")
	reviewWrite(a, s, "a", "one", "Box.count")
	s.Seed("unknown", NewValue(UnknownRecipeID, "Box", KindReference, nil, UnknownAliasSet("unknown_receiver")))
	reviewWrite(a, s, "unknown", "one", "Box.count")
	v := reviewRead(a, s, "a", "out", "Box.count")
	if !v.Unknown {
		t.Fatalf("unknown mutation left stale exact value: %+v", v)
	}
}
func TestTransferRegressionEscapeUpdatesOtherAlias(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	root := reviewRoot("A")
	reviewSeed(t, a, s, "a", root)
	reviewSeed(t, a, s, "b", root)
	reviewNumber(a, s, "one", "1")
	reviewNumber(a, s, "two", "2")
	reviewWrite(a, s, "a", "one", "Box.count")
	Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"a"}}, TransferOptions{})
	reviewWrite(a, s, "b", "two", "Box.count")
	v := reviewRead(a, s, "b", "out", "Box.count")
	if !v.Unknown {
		t.Fatalf("stale second alias still strong after escape: %+v", v)
	}
}
func TestTransferRegressionEscapeDoesNotReachUnrelatedStoredReference(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	for _, id := range []string{"a", "b", "c"} {
		reviewSeed(t, a, s, id, reviewRoot(id))
	}
	Apply(a, s, facts.Instruction{ID: "store", Opcode: facts.OpFieldWrite, Operands: []string{"b", "c"}, Type: "Box", ValueKind: facts.FlowKindReference, FieldID: "Box.child"}, TransferOptions{})
	Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"a"}}, TransferOptions{})
	v, _ := s.Value("c")
	for _, r := range v.Aliases.Roots() {
		if r.Ownership != OwnershipOwned {
			t.Fatalf("unrelated nested reference escaped: %+v", r)
		}
	}
}
func TestTransferRegressionStorageIdentityIsStructured(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("r"))
	reviewSeed(t, a, s, "b", reviewRoot("r/field:a"))
	x := reviewRead(a, s, "a", "x", "a/field:b")
	y := reviewRead(a, s, "b", "y", "b")
	if x.Recipe == y.Recipe {
		t.Fatalf("different roots/paths collide: %s", x.Recipe)
	}
}
func TestTransferRegressionAllocationRetainsRootKind(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	r := Apply(a, s, facts.Instruction{ID: "allocate", Opcode: facts.OpAllocate, Results: []string{"v"}, Type: "Box", ValueKind: facts.FlowKindReference, Roots: []facts.AliasRoot{{ID: "A", Kind: "allocation", Ownership: "owned", Mutable: true}}}, TransferOptions{})
	v, _ := s.Value("v")
	roots := v.Aliases.Roots()
	if r.Status != TransferOK || len(roots) != 1 || roots[0].Kind != "allocation" {
		t.Fatalf("allocation identity kind lost: %+v %+v", r, roots)
	}
}
func TestTransferRegressionBudgetDoesNotCommitPartialWrite(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "either", reviewRoot("A"), reviewRoot("B"))
	reviewNumber(a, s, "one", "1")
	before := s.MemorySnapshot()
	effects := s.EffectsSnapshot()
	r := Apply(a, s, facts.Instruction{ID: "write", Opcode: facts.OpFieldWrite, Operands: []string{"either", "one"}, Type: "int", ValueKind: facts.FlowKindNumeric, FieldID: "Box.count"}, TransferOptions{MaxWork: s.Work() + 3})
	if r.Status != TransferLimit {
		t.Fatalf("budget exceeded without limit: %+v", r)
	}
	if !reflect.DeepEqual(before, s.MemorySnapshot()) || !reflect.DeepEqual(effects, s.EffectsSnapshot()) {
		t.Fatal("aborted write committed partial state/effects")
	}
}
func TestTransferRegressionPackFieldsAndBindKeepSeparateAliases(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	for i, id := range []string{"a", "b"} {
		recipe, err := a.Formal(i, "Box")
		if err != nil {
			t.Fatal(err)
		}
		s.Seed(id, NewValue(recipe, "Box", KindReference, nil, NewAliasSet(reviewRoot(id))))
	}
	reviewNumber(a, s, "n", "7")
	Apply(a, s, facts.Instruction{ID: "pack", Opcode: facts.OpPack, Results: []string{"pair"}, Type: "Pair", ValueKind: facts.FlowKindRecord, FieldBindings: []facts.FieldBinding{{Field: "Pair.left", Value: "a"}, {Field: "Pair.right", Value: "b"}, {Field: "Pair.count", Value: "n"}}}, TransferOptions{})
	Apply(a, s, facts.Instruction{ID: "bind", Opcode: facts.OpBind, Operands: []string{"pair"}, Results: []string{"copy"}}, TransferOptions{})
	for _, field := range []struct{ name, id string }{{"Pair.left", "a"}, {"Pair.right", "b"}} {
		r := Apply(a, s, facts.Instruction{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"copy"}, Results: []string{"out"}, Type: "Box", ValueKind: facts.FlowKindReference, FieldID: field.name}, TransferOptions{})
		v, _ := s.Value("out")
		roots := v.Aliases.Roots()
		if r.Status != TransferOK || v.Unknown || len(roots) != 1 || roots[0].ID != field.id {
			t.Fatalf("pack field aliases conflated for %s: %+v %+v", field.name, r, v)
		}
	}
	v := reviewRead(a, s, "copy", "count", "Pair.count")
	if v.Unknown || v.Recipe != a.Constant("int", "7") || len(v.Aliases.Roots()) != 0 {
		t.Fatalf("scalar pack field lost: %+v", v)
	}
}
func TestTransferRegressionEffectsReturnedPerInstruction(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("A"))
	reviewNumber(a, s, "one", "1")
	r := reviewWrite(a, s, "a", "one", "Box.count")
	if len(r.Effects) != 1 || r.Effects[0].RootID != "A" || r.Effects[0].Kind != "memory_write" {
		t.Fatalf("instruction effects absent or wrong: %+v", r)
	}
	r.Effects[0].Path[0].Name = "corrupt"
	effects := s.EffectsSnapshot()
	if effects[0].Path[0].Name != "Box.count" {
		t.Fatal("result effect exposes mutable state")
	}
}
func TestTransferRegressionPossibleAliasDoesNotEscapeOtherTargets(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	left, right := reviewRoot("A"), reviewRoot("B")
	reviewSeed(t, a, s, "a", left)
	reviewSeed(t, a, s, "b", right)
	reviewSeed(t, a, s, "either", left, right)
	Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"a"}}, TransferOptions{})
	v, _ := s.Value("b")
	for _, r := range v.Aliases.Roots() {
		if r.Ownership != OwnershipOwned {
			t.Fatalf("may-alias join incorrectly acts as reachability edge: %+v", r)
		}
	}
}
func TestTransferRegressionMissingWriteReceiverInvalidates(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("A"))
	reviewNumber(a, s, "one", "1")
	reviewWrite(a, s, "a", "one", "Box.count")
	reviewWrite(a, s, "missing", "one", "Box.count")
	v := reviewRead(a, s, "a", "out", "Box.count")
	if !v.Unknown {
		t.Fatalf("unresolved write left stale exact memory: %+v", v)
	}
}
func TestTransferRegressionReadMissingTypeIsUnknown(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("A"))
	r := Apply(a, s, facts.Instruction{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"a"}, Results: []string{"out"}, FieldID: "Box.count"}, TransferOptions{})
	v, _ := s.Value("out")
	if r.Status == TransferOK || !v.Unknown {
		t.Fatalf("missing essential type accepted: %+v %+v", r, v)
	}
}
func TestTransferRegressionEscapeAbortAtomic(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "either", reviewRoot("A"), reviewRoot("B"))
	before := s.ValuesSnapshot()
	effects := s.EffectsSnapshot()
	r := Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"either"}}, TransferOptions{MaxWork: s.Work() + 3})
	if r.Status != TransferLimit {
		t.Fatalf("expected escape limit: %+v", r)
	}
	if !reflect.DeepEqual(before, s.ValuesSnapshot()) || !reflect.DeepEqual(effects, s.EffectsSnapshot()) {
		t.Fatal("escape aborted after partially changing ownership/effects")
	}
}
func TestTransferRegressionPackWorkCharged(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewNumber(a, s, "one", "1")
	fields := make([]facts.FieldBinding, 10)
	for i := range fields {
		fields[i] = facts.FieldBinding{Field: string(rune('a' + i)), Value: "one"}
	}
	r := Apply(a, s, facts.Instruction{ID: "pack", Opcode: facts.OpPack, Results: []string{"out"}, Type: "Record", ValueKind: facts.FlowKindRecord, FieldBindings: fields}, TransferOptions{MaxWork: 3})
	if r.Status != TransferLimit {
		t.Fatalf("pack member work uncharged: %+v", r)
	}
	if _, ok := s.Value("out"); ok {
		t.Fatal("limited pack committed a result")
	}
}
func TestTransferRegressionEscapedFieldKeepsSiblingOwned(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	left, right := reviewRoot("A"), reviewRoot("A")
	left.Path = []PathSegment{FieldSegment("left")}
	right.Path = []PathSegment{FieldSegment("right")}
	reviewSeed(t, a, s, "left", left)
	reviewSeed(t, a, s, "right", right)
	Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"left"}}, TransferOptions{})
	v, _ := s.Value("right")
	if v.Aliases.Roots()[0].Ownership != OwnershipOwned {
		t.Fatal("escaping a field escaped an unrelated sibling")
	}
}
func TestTransferRegressionEscapeIgnoresOverwrittenReference(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	for _, id := range []string{"a", "b", "c"} {
		reviewSeed(t, a, s, id, reviewRoot(id))
	}
	for _, value := range []string{"b", "c"} {
		Apply(a, s, facts.Instruction{ID: "store", Opcode: facts.OpFieldWrite, Operands: []string{"a", value}, FieldID: "Box.child", Type: "Box", ValueKind: facts.FlowKindReference}, TransferOptions{})
	}
	Apply(a, s, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"a"}}, TransferOptions{})
	b, _ := s.Value("b")
	c, _ := s.Value("c")
	if b.Aliases.Roots()[0].Ownership != OwnershipOwned || c.Aliases.Roots()[0].Ownership != OwnershipEscaped {
		t.Fatal("escape followed overwritten storage instead of its current reference")
	}
}
func TestTransferRegressionEqualRecipesKeepDifferentRecordAliases(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewSeed(t, a, s, "a", reviewRoot("A"))
	reviewSeed(t, a, s, "b", reviewRoot("B"))
	for _, id := range []string{"a", "b"} {
		Apply(a, s, facts.Instruction{ID: "pack", Opcode: facts.OpPack, Results: []string{"pack_" + id}, Type: "Pair", FieldBindings: []facts.FieldBinding{{Field: "Pair.value", Value: id}}}, TransferOptions{})
	}
	Apply(a, s, facts.Instruction{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"pack_a"}, Results: []string{"out"}, FieldID: "Pair.value", Type: "Box", ValueKind: facts.FlowKindReference}, TransferOptions{})
	v, _ := s.Value("out")
	if len(v.Aliases.Roots()) != 1 || v.Aliases.Roots()[0].ID != "A" {
		t.Fatalf("record alias metadata overwritten by equal pure recipe: %+v", v)
	}
}
func TestTransferRegressionRepeatedAllocationSiteIsNotOneInstance(t *testing.T) {
	a := NewRecipeArena()
	s := NewTransferState()
	reviewNumber(a, s, "one", "1")
	reviewNumber(a, s, "two", "2")
	for _, v := range []string{"one", "two"} {
		r := Apply(a, s, facts.Instruction{ID: "alloc", Opcode: facts.OpAllocate, Results: []string{"box_" + v}, Type: "Box", ValueKind: facts.FlowKindReference, Roots: []facts.AliasRoot{{ID: "site", Kind: "allocation", Mutable: true, Ownership: "owned"}}, FieldBindings: []facts.FieldBinding{{Field: "Box.count", Value: v}}}, TransferOptions{})
		if r.Status != TransferOK {
			t.Fatal(r)
		}
	}
	v := reviewRead(a, s, "box_one", "out", "Box.count")
	if !v.Unknown {
		t.Fatalf("repeated site conflated concrete instances: %+v", v)
	}
}

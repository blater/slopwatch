package depth

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func transferRef(id string, ownership OwnershipState) DomainValue {
	return NewValue(RecipeID(id), "Box", KindReference, nil, NewAliasSet(NewAliasRoot("obj", true, ownership)))
}

func transferConst(t *testing.T, a *RecipeArena, state *TransferState, id, typ string, kind facts.FlowValueKind, literal string) {
	t.Helper()
	got := state.Apply(a, facts.Instruction{ID: id + ".constant", Opcode: facts.OpConstant, Results: []string{id}, Type: typ, ValueKind: kind, Value: &facts.Value{Type: typ, ValueKind: kind, Constant: literal}}, TransferOptions{})
	if got.Status != TransferOK {
		t.Fatalf("constant status = %s (%v)", got.Status, got.Reasons)
	}
}

func TestTransferConstantEmptyLiteralAndBindPreserveMetadata(t *testing.T) {
	a, state := NewRecipeArena(), NewTransferState()
	transferConst(t, a, state, "empty", "string", facts.FlowKindString, "")
	value, ok := state.Value("empty")
	if !ok || value.Type != "string" || value.Kind != KindString {
		t.Fatalf("constant value = %+v, %v", value, ok)
	}
	node, ok := a.Lookup(value.Recipe)
	if !ok || node.Literal != "" {
		t.Fatalf("literal was not preserved: %+v, %v", node, ok)
	}
	state.Apply(a, facts.Instruction{ID: "bind", Opcode: facts.OpBind, Operands: []string{"empty"}, Results: []string{"copy"}}, TransferOptions{})
	copy, _ := state.Value("copy")
	if copy.Recipe != value.Recipe || copy.Type != value.Type || copy.Kind != value.Kind {
		t.Fatalf("bind changed value: %+v", copy)
	}
	missing := state.Apply(a, facts.Instruction{ID: "bad", Opcode: facts.OpConstant, Results: []string{"bad"}, Value: &facts.Value{Constant: "0"}}, TransferOptions{})
	if missing.Status != TransferPartial {
		t.Fatalf("missing metadata status = %s", missing.Status)
	}
	bad, _ := state.Value("bad")
	if !bad.Unknown || len(bad.Aliases.Roots()) != 0 {
		t.Fatalf("unknown scalar invented aliases: %+v", bad)
	}
}

func TestTransferWeakTwoRootsAndOrderedWildcardWrites(t *testing.T) {
	a, state := NewRecipeArena(), NewTransferState()
	r1 := NewAliasRoot("one", true, OwnershipOwned)
	r2 := NewAliasRoot("two", true, OwnershipOwned)
	state.Seed("receiver", NewValue("receiver", "Box", KindReference, nil, NewAliasSet(r1, r2)))
	transferConst(t, a, state, "one", "int", facts.FlowKindNumeric, "1")
	write := facts.Instruction{ID: "write", Opcode: facts.OpFieldWrite, Operands: []string{"receiver", "one"}, FieldID: "Box.count"}
	if got := state.Apply(a, write, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("weak write status = %s (%v)", got.Status, got.Reasons)
	}
	read := facts.Instruction{ID: "read", Opcode: facts.OpFieldRead, Operands: []string{"receiver"}, Results: []string{"value"}, Type: "int", ValueKind: facts.FlowKindNumeric, FieldID: "Box.count"}
	got := state.Apply(a, read, TransferOptions{})
	if got.Status != TransferPartial {
		t.Fatalf("weak read status = %s (%v)", got.Status, got.Reasons)
	}
	value, _ := state.Value("value")
	if value.Recipe == "" {
		t.Fatal("weak read had no recipe")
	}
	if len(state.MemorySnapshot()) != 2 {
		t.Fatalf("expected one write per target: %+v", state.MemorySnapshot())
	}

	// Dynamic write must affect a later concrete read, while a later strong
	// exact write replaces that earlier possible write for the exact location.
	state.Seed("array", NewValue("array", "Array", KindReference, nil, NewAliasSet(NewAliasRoot("array", true, OwnershipOwned))))
	transferConst(t, a, state, "twoValue", "int", facts.FlowKindNumeric, "2")
	dynamic := facts.Instruction{ID: "dynamic", Opcode: facts.OpFieldWrite, Operands: []string{"array", "one"}, AccessPath: []facts.AliasPathSegment{{Kind: "index", Dynamic: true}}}
	if got := state.Apply(a, dynamic, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("dynamic write status = %s (%v)", got.Status, got.Reasons)
	}
	exact := facts.Instruction{ID: "exact", Opcode: facts.OpFieldWrite, Operands: []string{"array", "twoValue"}, AccessPath: []facts.AliasPathSegment{{Kind: "index", Name: "2"}}}
	if got := state.Apply(a, exact, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("exact write status = %s (%v)", got.Status, got.Reasons)
	}
	exactRead := exact
	exactRead.ID = "exact-read"
	exactRead.Opcode = facts.OpFieldRead
	exactRead.Results = []string{"exactValue"}
	exactRead.Operands = []string{"array"}
	exactRead.Type = "int"
	exactRead.ValueKind = facts.FlowKindNumeric
	if got := state.Apply(a, exactRead, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("exact read status = %s (%v)", got.Status, got.Reasons)
	}
	value, _ = state.Value("exactValue")
	node, _ := a.Lookup(value.Recipe)
	if node.Kind != KindConstant || node.Literal != "2" {
		t.Fatalf("later exact write was poisoned: value=%+v node=%+v", value, node)
	}
}

func TestTransferEscapeUpdatesStaleAliasesAndPackUnpack(t *testing.T) {
	a, state := NewRecipeArena(), NewTransferState()
	root := NewAliasRoot("owned", true, OwnershipOwned)
	state.Seed("a", NewValue("a", "Box", KindReference, nil, NewAliasSet(root)))
	state.Seed("b", NewValue("b", "Box", KindReference, nil, NewAliasSet(root)))
	if got := state.Apply(a, facts.Instruction{ID: "escape", Opcode: facts.OpEscape, Operands: []string{"a"}}, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("escape status = %s", got.Status)
	}
	b, _ := state.Value("b")
	if b.Aliases.Roots()[0].Ownership != OwnershipEscaped {
		t.Fatalf("stale alias remained owned: %+v", b.Aliases.Roots())
	}
	transferConst(t, a, state, "n", "int", facts.FlowKindNumeric, "3")
	write := facts.Instruction{ID: "stale-write", Opcode: facts.OpFieldWrite, Operands: []string{"b", "n"}, FieldID: "Box.count"}
	if got := state.Apply(a, write, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("stale write status = %s", got.Status)
	}
	entries := state.MemorySnapshot()
	if len(entries) == 0 || entries[len(entries)-1].Strong {
		t.Fatalf("escaped alias write was strong: %+v", entries)
	}

	state2 := NewTransferState()
	xRecipe, _ := a.Formal(0, "int")
	refRecipe, _ := a.Formal(1, "Buffer")
	state2.Seed("x", ScalarValue(xRecipe, "int", KindNumeric))
	state2.Seed("ref", NewValue(refRecipe, "Buffer", KindReference, nil, NewAliasSet(NewAliasRoot("backing", true, OwnershipOwned))))
	pack := facts.Instruction{ID: "pack", Opcode: facts.OpPack, Results: []string{"pair"}, Type: "Pair", FieldBindings: []facts.FieldBinding{{Field: "Pair.x", Value: "x"}, {Field: "Pair.ref", Value: "ref"}}}
	if got := state2.Apply(a, pack, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("pack status = %s", got.Status)
	}
	unpack := facts.Instruction{ID: "unpack", Opcode: facts.OpFieldRead, Operands: []string{"pair"}, Results: []string{"unpacked"}, Type: "Buffer", ValueKind: facts.FlowKindReference, FieldID: "Pair.ref"}
	if got := state2.Apply(a, unpack, TransferOptions{}); got.Status != TransferOK {
		t.Fatalf("unpack status = %s (%v)", got.Status, got.Reasons)
	}
	value, _ := state2.Value("unpacked")
	if len(value.Aliases.Roots()) != 1 || value.Aliases.Roots()[0].ID != "backing" {
		t.Fatalf("reference field lost backing alias: %+v", value)
	}
}

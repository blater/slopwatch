package depth

import "slopslap.dev/structural/internal/facts"

// These are raw token events. Matching/universal proof belongs to the later
// control/proof stage. Acquisition must already be on its success continuation.
func transferResource(t *transferStep) {
	var roots []AliasRoot
	var operands []RecipeID
	for _, raw := range t.in.Roots {
		root, ok := rootFromFact(raw)
		if !ok {
			t.unknown("invalid_resource_root")
			return
		}
		roots = append(roots, root)
	}
	for _, id := range t.in.Operands {
		value, ok := t.operand(id)
		if !ok || value.Aliases.IsUnknown() {
			t.unknown("unknown_resource_token")
			return
		}
		roots = append(roots, value.Aliases.Roots()...)
		operands = append(operands, value.Recipe)
	}
	roots = NewAliasSet(roots...).Roots()
	if len(roots) == 0 {
		t.unknown("missing_resource_token")
		return
	}
	for _, root := range roots {
		if !t.budget.charge(1 + len(root.Path)) {
			return
		}
		t.delta.effects = append(t.delta.effects, TransferEffect{Kind: string(t.in.Opcode), RootID: root.ID, RootKind: root.Kind, Ownership: root.Ownership, Mutable: root.Mutable, Path: root.Path, Operands: operands, Instruction: t.in.ID})
	}
	if t.in.Opcode != facts.OpAcquire {
		return
	}
	if len(roots) != 1 || t.in.Type == "" {
		t.unknown("invalid_resource_producer")
		return
	}
	recipe, err := t.arena.Storage(t.in.Type, locationString(roots[0], nil))
	if err != nil {
		t.unknown(reasonCode(err))
		return
	}
	t.output(NewValue(recipe, t.in.Type, KindReference, nil, NewAliasSet(roots[0])))
}

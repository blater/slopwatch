package depth

func transferAllocate(t *transferStep) {
	if len(t.in.Roots) != 1 {
		t.unknown("allocation_requires_exact_site")
		return
	}
	root, ok := rootFromFact(t.in.Roots[0])
	if !ok || root.Ownership != OwnershipOwned {
		t.unknown("invalid_allocation_root")
		return
	}
	root.Multiple = t.state.allocations[aliasKey(root)]
	t.delta.allocations = append(t.delta.allocations, root)
	kind := suppliedKind(t.in.ValueKind)
	if t.in.Type == "" || !referenceValueKind(kind) {
		t.unknown("missing_allocation_metadata")
		return
	}
	recipe, err := t.arena.Storage(t.in.Type, locationString(root, nil))
	if err != nil {
		t.unknown(reasonCode(err))
		return
	}
	for _, binding := range t.in.FieldBindings {
		if !t.budget.charge(1) {
			return
		}
		if binding.Field == "" {
			t.unknown("missing_field_identity")
			return
		}
		value, ok := t.operand(binding.Value)
		if !ok {
			t.unknown(ReasonUnknownOperand)
			return
		}
		location := cloneRoot(root)
		location.Path = append(location.Path, FieldSegment(binding.Field))
		if len(location.Path) > 4 {
			t.unknown(AccessPathLimitReason)
			return
		}
		t.delta.writes = append(t.delta.writes, memoryWrite{Root: location, MemoryEntry: MemoryEntry{Value: value, Strong: rootCanStrong(location)}})
	}
	if kind == KindError {
		recipe, err = errorRecipe(t.arena, true, recipe)
		if err != nil {
			t.unknown(reasonCode(err))
			return
		}
	}
	t.output(NewValue(recipe, t.in.Type, kind, nil, NewAliasSet(root)))
}

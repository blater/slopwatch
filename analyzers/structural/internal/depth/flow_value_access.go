package depth

func transferRead(t *transferStep) {
	kind := suppliedKind(t.in.ValueKind)
	if t.in.Type == "" || kind == KindUnknownValue {
		t.unknown("missing_read_metadata")
		return
	}
	receiver, ok := t.operand(firstOperand(t.in))
	if !ok {
		t.unknown(ReasonUnknownOperand)
		return
	}
	path, ok := accessPath(t.in)
	if !ok {
		t.unknown("invalid_access_path")
		return
	}
	if readRecord(t, receiver, path) {
		return
	}
	if receiver.Kind == KindRecord {
		t.unknown("unknown_record_fields")
		return
	}
	roots, ok := targetLocations(receiver.Aliases, path)
	if !ok {
		t.unknown("unknown_alias")
		return
	}
	var value DomainValue
	for _, root := range roots {
		if !t.budget.charge(1 + len(root.Path)) {
			return
		}
		if t.state.unknownMemory || rootMarked(t.state.invalidRoots, root) {
			t.unknown("unknown_memory")
			return
		}
		fallback := func(r AliasRoot, p []PathSegment) DomainValue { return priorStorage(t.arena, t.in.Type, r, p, kind) }
		next, _, complete := t.state.memory.readBounded(root, nil, fallback, t.budget.charge)
		if !complete {
			return
		}
		if !t.budget.charge(valueWork(next)) {
			return
		}
		value = value.Join(effectiveValue(t.state, next))
		t.effect("memory_read", root, next.Recipe)
	}
	if !referenceValueKind(kind) {
		value.Aliases = NewAliasSet()
	}
	t.output(value)
}
func priorStorage(arena *RecipeArena, typ string, root AliasRoot, path []PathSegment, kind ValueKind) DomainValue {
	id := locationString(root, path)
	recipe, err := arena.Storage(typ, id)
	if err != nil {
		return typedUnknown(typ, kind, reasonCode(err))
	}
	aliases := NewAliasSet()
	if referenceValueKind(kind) {
		aliases = NewAliasSet(root)
	}
	value := NewValue(recipe, typ, kind, nil, aliases)
	value.Dependencies = []string{"storage:" + id}
	return value
}
func transferWrite(t *transferStep) {
	receiver, ok := t.operand(firstOperand(t.in))
	if !ok {
		unknownWrite(t, ReasonUnknownOperand)
		return
	}
	if receiver.RecordID != "" {
		t.partial("record_write_requires_lvalue")
		t.result.Status = TransferUnsupported
		return
	}
	path, ok := accessPath(t.in)
	if !ok {
		unknownWrite(t, "invalid_access_path")
		return
	}
	roots, ok := targetLocations(receiver.Aliases, path)
	if !ok {
		unknownWrite(t, "unknown_alias")
		return
	}
	valueID := ""
	if len(t.in.Operands) > 1 {
		valueID = t.in.Operands[1]
	}
	if len(t.in.FieldBindings) == 1 {
		valueID = t.in.FieldBindings[0].Value
	}
	value, ok := t.operand(valueID)
	if !ok {
		value = typedUnknown(t.in.Type, suppliedKind(t.in.ValueKind), ReasonUnknownOperand)
		t.partial(ReasonUnknownOperand)
	}
	for _, root := range roots {
		if !t.budget.charge(1 + len(root.Path) + valueWork(value)) {
			return
		}
		strong := len(roots) == 1 && rootCanStrong(root)
		t.delta.writes = append(t.delta.writes, memoryWrite{Root: root, MemoryEntry: MemoryEntry{Value: value, Strong: strong}})
		t.effect("memory_write", root, value.Recipe)
	}
	if value.Unknown {
		t.partial(firstReason(value.Reasons, "unknown_write_value"))
	}
}
func unknownWrite(t *transferStep, reason string) {
	t.partial(reason)
	t.delta.unknownMemory = true
	t.delta.effects = append(t.delta.effects, TransferEffect{Kind: "memory_write", Unknown: true, Reason: reason, Instruction: t.in.ID})
}

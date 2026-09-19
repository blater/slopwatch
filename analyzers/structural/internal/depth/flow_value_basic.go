package depth

func transferConstant(t *transferStep) {
	in := t.in
	if in.Value == nil {
		t.unknown("missing_constant")
		return
	}
	typ := in.Value.Type
	if typ == "" {
		typ = in.Type
	}
	kind := in.Value.ValueKind
	if kind == "" {
		kind = in.ValueKind
	}
	if typ == "" || suppliedKind(kind) == KindUnknownValue {
		t.unknown("missing_constant_metadata")
		return
	}
	if suppliedKind(kind) == KindError && in.Value.Constant == "nil" {
		recipe, err := errorRecipe(t.arena, false, t.arena.Constant(typ, "nil"))
		if err != nil {
			t.unknown(reasonCode(err))
			return
		}
		t.output(NewValue(recipe, typ, KindError, nil, NewAliasSet()))
		return
	}
	if referenceValueKind(suppliedKind(kind)) {
		t.unknown("constant_requires_storage_metadata")
		return
	}
	t.output(ScalarValue(t.arena.Constant(typ, in.Value.Constant), typ, suppliedKind(kind), in.Value.Concepts...))
}
func transferBind(t *transferStep) {
	value, ok := t.operand(firstOperand(t.in))
	if !ok {
		t.unknown(ReasonUnknownOperand)
		return
	}
	t.output(value)
}
func transferPrimitive(t *transferStep) {
	if t.in.Type == "" || suppliedKind(t.in.ValueKind) == KindUnknownValue {
		t.unknown("missing_primitive_metadata")
		return
	}
	recipes := make([]RecipeID, 0, len(t.in.Operands))
	var deps, reasons []string
	unknown := false
	for _, id := range t.in.Operands {
		value, ok := t.operand(id)
		if !ok {
			t.unknown(ReasonUnknownOperand)
			return
		}
		recipes = append(recipes, value.Recipe)
		deps = append(deps, value.Dependencies...)
		reasons = appendUniqueStrings(reasons, value.Reasons...)
		unknown = unknown || value.Unknown
	}
	if !t.budget.charge(len(recipes)) {
		return
	}
	recipe, err := t.arena.Primitive(t.in.Type, ArithmeticMode(t.in.ArithmeticMode), t.in.Operator, recipes)
	if err != nil {
		t.unknown(reasonCode(err))
		return
	}
	value := PrimitiveResult(recipe, t.in.Type, suppliedKind(t.in.ValueKind), deps)
	value.Unknown = unknown
	value.Reasons = reasons
	t.output(value)
}
func transferUnknown(t *transferStep) {
	t.unknown("unknown_operation")
	for _, id := range t.in.Operands {
		value, ok := t.operand(id)
		if !ok {
			t.delta.unknownMemory = true
			continue
		}
		if !referenceValueKind(value.Kind) {
			continue
		}
		if value.Aliases.IsUnknown() {
			t.delta.unknownMemory = true
			continue
		}
		t.delta.invalid = append(t.delta.invalid, value.Aliases.Roots()...)
	}
	t.delta.effects = append(t.delta.effects, TransferEffect{Kind: "unknown", Unknown: true, Reason: "unknown_operation", Instruction: t.in.ID})
}

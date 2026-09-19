package depth

import "sort"

func closeNumericRegion(e *controlEvaluation, p numericRegion, frame *controlFrame, initial map[string]DomainValue, condition RecipeID, gapStart, effectStart int) {
	bindings := make([]NumericLoopBinding, 0, len(p.slots))
	metadata := map[string]DomainValue{}
	for _, slot := range p.slots {
		update := numericLoopUpdate(e, slot, frame)
		metadata[slot.ID] = combineLoopMetadata(initial[slot.ID], update)
		bindings = append(bindings, NumericLoopBinding{Kind: KindNumeric, Slot: slot.ID, Type: slot.Type, Initial: initial[slot.ID].Recipe, Update: update.Recipe})
	}
	if e.guards.atom(condition) != guardTrue && !loopConditionVaries(e, condition, bindings) {
		e.gap(p.header, "", frame.guard, "invariant_loop_condition")
	}
	unsupportedEffects := len(e.result.Effects) != effectStart
	replacements := map[RecipeID]RecipeID{}
	for _, slot := range p.slots {
		if !e.budget.charge(1 + len(bindings)) {
			return
		}
		result, err := e.arena.NumericLoop(slot.Type, slot.ID, condition, bindings)
		if err != nil {
			e.gap(p.header, slot.PhiID, frame.guard, reasonCode(err))
		}
		if unsupportedEffects {
			result = UnknownRecipeID
		}
		ref, _ := e.arena.RecurrenceRef(slot.Type, slot.ID, nil)
		replacements[ref] = result
	}
	propagateLoopMetadata(e, p, frame, bindings, metadata, condition)
	rewriteLoopState(e, frame, replacements)
	// A repeated effect is not a single execution of its body recipe. Until the
	// effect proof domain models repetition, retain a may-effect and invalidate
	// memory precision; no resource/state responsibility can be awarded from it.
	for i := effectStart; i < len(e.result.Effects); i++ {
		effect := &e.result.Effects[i]
		effect.Guard = e.guards.recipe(frame.guard)
		effect.Effect.Unknown = true
		effect.Effect.Reason = "unsupported_loop_effect"
		effect.Effect.Value = UnknownRecipeID
		effect.Effect.Operands = nil
	}
	for i := gapStart; i < len(e.result.Gaps); i++ {
		e.result.Gaps[i].Guard = e.guards.recipe(frame.guard)
	}
	if unsupportedEffects {
		frame.state.unknownMemory = true
		e.gap(p.header, "", frame.guard, "unsupported_loop_effect")
	}
	// This is the state conditional on normal loop completion, not evidence of
	// termination. The initial false guard alone must not gate the eventual exit.
	if e.guards.atom(condition) != guardTrue {
		e.inputs[p.exit.To] = append(e.inputs[p.exit.To], controlInput{predecessor: p.header, edge: p.exit, guard: frame.guard, state: frame.state, pending: frame.pending})
	}
}
func numericLoopUpdate(e *controlEvaluation, slot CompiledRecurrence, frame *controlFrame) DomainValue {
	var out DomainValue
	for i, input := range e.inputs[slot.Header] {
		if !e.budget.charge(1) {
			return typedUnknown(slot.Type, KindNumeric, "work_limit")
		}
		value, ok := input.state.Value(slot.Updates[input.predecessor])
		if !ok {
			value = typedUnknown(slot.Type, KindNumeric, "missing_loop_update")
		}
		if i == 0 {
			out = value
		} else {
			out = selectControlRecord(e.arena, frame.state.records, e.guards.recipe(input.guard), value, out, e.budget.charge)
		}
	}
	if out.isBottom() {
		return typedUnknown(slot.Type, KindNumeric, "missing_loop_update")
	}
	return out
}
func rewriteLoopState(e *controlEvaluation, frame *controlFrame, replacements map[RecipeID]RecipeID) {
	w := &recipeRewriter{builder: e.arena.runtime.builder, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: e.budget.charge}
	keys := make([]string, 0, len(frame.state.values))
	for id := range frame.state.values {
		if !e.budget.charge(1) {
			return
		}
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		value := frame.state.values[id]
		if value.Recipe == "" {
			continue
		}
		recipe, err := w.rewrite(value.Recipe)
		value.Recipe = recipe
		if err != nil || recipe == UnknownRecipeID {
			value.Unknown = true
			value.Reasons = appendUniqueStrings(value.Reasons, "unknown_loop_value")
		}
		frame.state.values[id] = value
	}
}

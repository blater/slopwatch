package depth

import "sort"

// NumericLoopBinding is a simultaneous loop-carried numeric slot. References in
// Update/Condition use RecurrenceRef with this canonical slot ID. Definitions
// are immutable children of the result recipe, not a mutable arena side table.
type NumericLoopBinding struct {
	Kind            ValueKind
	Slot, Type      string
	Initial, Update RecipeID
}

func (a *RecipeArena) NumericLoop(typ, resultSlot string, condition RecipeID, bindings []NumericLoopBinding) (RecipeID, error) {
	return buildNumericLoop(a.runtime.builder, typ, resultSlot, condition, bindings)
}
func buildNumericLoop(a *recipeBuilder, typ, resultSlot string, condition RecipeID, bindings []NumericLoopBinding) (RecipeID, error) {
	if typ == "" || resultSlot == "" {
		return a.unknownResult(ReasonInvalidRecurrence, "missing loop result")
	}
	if len(bindings) == 0 || len(bindings) > a.maxNodes {
		return a.unknownResult(ReasonRecipeLimit, "loop binding count")
	}
	predicate, ok := a.nodes[condition]
	if !ok || !isBooleanType(predicate.Type) {
		return a.unknownResult(ReasonInvalidType, "loop condition")
	}
	ordered := append([]NumericLoopBinding(nil), bindings...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Slot < ordered[j].Slot })
	fields := make([]FieldRecipeBinding, 0, len(ordered))
	seen := map[string]bool{}
	for _, binding := range ordered {
		if binding.Slot == "" || binding.Type == "" || binding.Kind != KindNumeric || seen[binding.Slot] {
			return a.unknownResult(ReasonInvalidRecurrence, "invalid or duplicate loop slot")
		}
		if err := a.validateChildren([]RecipeID{binding.Initial, binding.Update}, 2, 2); err != nil {
			return a.unknownResult(ReasonInvalidChild, "loop binding")
		}
		if a.nodes[binding.Initial].Type != binding.Type || a.nodes[binding.Update].Type != binding.Type {
			return a.unknownResult(ReasonInvalidType, "loop binding type")
		}
		if binding.Slot == resultSlot && binding.Type != typ {
			return a.unknownResult(ReasonInvalidType, "loop result type")
		}
		seen[binding.Slot] = true
		node := a.intern(RecipeNode{Kind: KindLoopBinding, Type: binding.Type, RecurrenceID: binding.Slot, Children: []RecipeID{binding.Initial, binding.Update}})
		fields = append(fields, FieldRecipeBinding{Field: binding.Slot, Recipe: node})
	}
	if !seen[resultSlot] {
		return a.unknownResult(ReasonInvalidRecurrence, "unbound loop result")
	}
	initialCondition, err := loopInitialCondition(a, condition, ordered)
	if err != nil {
		return a.unknownResult(reasonCode(err), "loop initial condition")
	}
	if node := a.nodes[initialCondition]; node.Kind == KindConstant && node.Literal == "false" {
		for _, binding := range ordered {
			if binding.Slot == resultSlot {
				return binding.Initial, nil
			}
		}
	}
	return a.intern(RecipeNode{Kind: KindLoop, Type: typ, RecurrenceID: resultSlot, Children: []RecipeID{condition, initialCondition}, Fields: fields}), nil
}

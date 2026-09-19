package depth

func instantiateCallValue(t *transferStep, value DomainValue, function *CompiledFunction, actuals []DomainValue) (DomainValue, string) {
	value = value.Copy()
	outcome := t.arena.BuildOutcome(value.Recipe)
	if !t.budget.charge(outcome.ElementVisits) {
		return value, "work_limit"
	}
	if outcome.Unknown {
		return value, outcome.Reason
	}
	formals, hasLoop, _ := callRecipeInputs(t, value.Recipe)
	replacements := map[RecipeID]RecipeID{}
	for index, formal := range function.FormalsSnapshot() {
		if !formals[index] {
			continue
		}
		actual := actuals[index]
		if hasLoop {
			_, _, recurrence := callRecipeInputs(t, actual.Recipe)
			if recurrence {
				return value, "nested_helper_recurrence"
			}
		}
		recipe, err := t.arena.Formal(index, formal.Type)
		if err != nil {
			return value, reasonCode(err)
		}
		replacements[recipe] = actual.Recipe
		value = combineLoopMetadata(value, actual)
	}
	w := &recipeRewriter{builder: t.arena.runtime.builder, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: t.budget.charge}
	recipe, err := w.rewrite(value.Recipe)
	if err != nil {
		return value, reasonCode(err)
	}
	value.Recipe = recipe
	value.Dependencies = appendUniqueStrings(value.Dependencies, "function:"+function.function.ID)
	return value, ""
}

// Inspect interned structure, never recipe text or identifier spelling. The walk
// is charged while traversing, including actual recipes containing loop refs.
func callRecipeInputs(t *transferStep, root RecipeID) (map[int]bool, bool, bool) {
	formals := map[int]bool{}
	loop, recurrence := false, false
	recipeAny(t.arena, root, t.budget.charge, func(node RecipeNode) bool {
		switch node.Kind {
		case KindFormal:
			formals[node.FormalIndex] = true
		case KindLoop:
			loop = true
		case KindRecurrence:
			recurrence = true
		}
		return false
	})
	return formals, loop, recurrence
}

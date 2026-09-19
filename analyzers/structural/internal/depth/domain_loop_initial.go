package depth

func loopInitialCondition(a *recipeBuilder, condition RecipeID, bindings []NumericLoopBinding) (RecipeID, error) {
	replacements := make(map[RecipeID]RecipeID, len(bindings))
	for _, binding := range bindings {
		reference, err := a.RecurrenceRef(binding.Type, binding.Slot, nil)
		if err != nil {
			return UnknownRecipeID, err
		}
		replacements[reference] = binding.Initial
	}
	remaining := maxOutcomeElementVisits
	charge := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	rewrite := recipeRewriter{builder: a, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: charge}
	return rewrite.rewrite(condition)
}

func pruneLoopInitial(node recipeNode, nodes map[RecipeID]recipeNode, rewrite func(RecipeID) (RecipeID, error)) (RecipeID, bool, error) {
	if len(node.Children) != 2 {
		return UnknownRecipeID, true, &RecipeError{Code: ReasonInvalidRecurrence, Message: "loop condition arity"}
	}
	condition, err := rewrite(node.Children[1])
	if err != nil {
		return condition, true, err
	}
	initial := nodes[condition]
	if initial.Kind != KindConstant || initial.Literal != "false" {
		return "", false, nil
	}
	for _, field := range node.Fields {
		if field.Field != node.RecurrenceID {
			continue
		}
		binding := nodes[field.Recipe]
		if len(binding.Children) != 2 {
			return UnknownRecipeID, true, &RecipeError{Code: ReasonInvalidRecurrence, Message: "loop binding arity"}
		}
		value, err := rewrite(binding.Children[0])
		return value, true, err
	}
	return UnknownRecipeID, true, &RecipeError{Code: ReasonInvalidRecurrence, Message: "loop result slot"}
}

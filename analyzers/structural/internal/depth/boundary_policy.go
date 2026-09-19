package depth

import "sort"

// Public Boolean selection is policy when both distinct branches consume an
// independent payload. Boolean-to-literal encoding is ordinary data work.
func outcomePolicyInputs(arena *RecipeArena, values []DomainValue, charge func(int) bool) ([]int, string) {
	selected := map[int]bool{}
	reason := ""
	for _, value := range values {
		recipeAny(arena, value.Recipe, charge, func(node RecipeNode) bool {
			if node.Kind == KindLoop && loopSelectsPublicBoolean(arena, node.ID, charge) {
				reason = "unsupported_loop_policy"
				return true
			}
			if node.Kind != KindSelect || node.TrueValue == node.FalseValue {
				return false
			}
			predicate, _ := arena.Lookup(node.Predicate)
			if predicate.Kind != KindFormal || !isBooleanType(predicate.Type) {
				return false
			}
			if independentOutcomeInput(arena, node.TrueValue, predicate.FormalIndex, charge) && independentOutcomeInput(arena, node.FalseValue, predicate.FormalIndex, charge) {
				selected[predicate.FormalIndex] = true
			}
			return false
		})
		if !charge(0) {
			return nil, "work_limit"
		}
		if reason != "" {
			return nil, reason
		}
	}
	indices := make([]int, 0, len(selected))
	for index := range selected {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	return indices, ""
}
func independentOutcomeInput(arena *RecipeArena, root RecipeID, selector int, charge func(int) bool) bool {
	return recipeAny(arena, root, charge, func(node RecipeNode) bool { return node.Kind == KindFormal && node.FormalIndex != selector })
}
func loopSelectsPublicBoolean(arena *RecipeArena, root RecipeID, charge func(int) bool) bool {
	return recipeAny(arena, root, charge, func(node RecipeNode) bool {
		if node.Kind != KindSelect {
			return false
		}
		predicate, _ := arena.Lookup(node.Predicate)
		return predicate.Kind == KindFormal && isBooleanType(predicate.Type)
	})
}

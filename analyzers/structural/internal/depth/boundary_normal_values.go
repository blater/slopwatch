package depth

import "strconv"

// Normalize returned recipes under the conditions necessary for normal return.
// Validation relevance is proved before this erases a constrained input.
func constrainNormalValues(arena *RecipeArena, completion FlowCompletion, charge func(int) bool) ([]DomainValue, string) {
	g := newControlGuards(arena, charge)
	coverage := g.atom(completion.Guard)
	replacements := map[RecipeID]RecipeID{}
	nodes := append([]guardNode(nil), g.nodes[2:]...)
	for _, node := range nodes {
		if !charge(1) {
			return nil, "work_limit"
		}
		atom := g.atom(node.predicate)
		literal := ""
		if g.and(coverage, g.not(atom)) == guardFalse {
			literal = strconv.FormatBool(true)
		}
		if g.and(coverage, atom) == guardFalse {
			literal = strconv.FormatBool(false)
		}
		if literal != "" {
			predicate, _ := arena.Lookup(node.predicate)
			replacements[node.predicate] = arena.Constant(predicate.Type, literal)
		}
	}
	if !charge(0) {
		return nil, "work_limit"
	}
	if len(replacements) == 0 {
		return completion.Values, ""
	}
	rewrite := &recipeRewriter{builder: arena.runtime.builder, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: charge}
	values := append([]DomainValue(nil), completion.Values...)
	for index, value := range values {
		recipe, err := rewrite.rewrite(value.Recipe)
		if err != nil {
			return nil, reasonCode(err)
		}
		values[index].Recipe = recipe
	}
	return values, ""
}

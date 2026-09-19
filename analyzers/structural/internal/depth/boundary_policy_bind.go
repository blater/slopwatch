package depth

func policyOutcomeVariants(arena *RecipeArena, values []DomainValue, indices []int, charge func(int) bool) ([][]DomainValue, string) {
	variants := [][]DomainValue{values}
	for _, index := range indices {
		next := [][]DomainValue{}
		seen := map[string]bool{}
		for _, variant := range variants {
			for _, literal := range []string{"false", "true"} {
				replacements := map[RecipeID]RecipeID{}
				for _, typ := range []string{"bool", "boolean"} {
					formal, _ := arena.Formal(index, typ)
					replacements[formal] = arena.Constant(typ, literal)
				}
				rewrite := &recipeRewriter{builder: arena.runtime.builder, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: charge}
				bound := make([]DomainValue, len(variant))
				var key []byte
				for position, value := range variant {
					recipe, err := rewrite.rewrite(value.Recipe)
					if err != nil {
						return nil, reasonCode(err)
					}
					value.Recipe = recipe
					bound[position] = value
					key = appendID(key, recipe)
				}
				if !seen[string(key)] {
					seen[string(key)] = true
					next = append(next, bound)
				}
				if len(next) > 32 {
					return nil, "alternative_limit"
				}
			}
		}
		variants = next
	}
	return variants, ""
}

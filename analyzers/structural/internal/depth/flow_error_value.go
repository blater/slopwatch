package depth

import "strconv"

func errorRecipe(arena *RecipeArena, present bool, payload RecipeID) (RecipeID, error) {
	return arena.Pack("error", []FieldRecipeBinding{{Field: "error.present", Recipe: arena.Constant("bool", strconv.FormatBool(present))}, {Field: "error.payload", Recipe: payload}})
}

func errorPresence(arena *RecipeArena, recipe RecipeID, charge func(int) bool, memo map[RecipeID]RecipeID) (RecipeID, error) {
	if !charge(1) {
		return UnknownRecipeID, &RecipeError{Code: "work_limit"}
	}
	if value, ok := memo[recipe]; ok {
		return value, nil
	}
	node, ok := arena.Lookup(recipe)
	if !ok {
		return UnknownRecipeID, &RecipeError{Code: "unknown_error_presence"}
	}
	if node.Kind == KindPack {
		for _, field := range node.Fields {
			if !charge(1) {
				return UnknownRecipeID, &RecipeError{Code: "work_limit"}
			}
			if field.Field == "error.present" {
				value, known := arena.Lookup(field.Recipe)
				if !known || (value.Type != "bool" && value.Type != "boolean") {
					return UnknownRecipeID, &RecipeError{Code: "invalid_error_presence"}
				}
				memo[recipe] = field.Recipe
				return field.Recipe, nil
			}
		}
	}
	if node.Kind == KindSelect {
		yes, err := errorPresence(arena, node.TrueValue, charge, memo)
		if err != nil {
			return UnknownRecipeID, err
		}
		no, err := errorPresence(arena, node.FalseValue, charge, memo)
		if err != nil {
			return UnknownRecipeID, err
		}
		result, err := arena.Select("bool", node.Predicate, yes, no)
		if err == nil {
			memo[recipe] = result
		}
		return result, err
	}
	return UnknownRecipeID, &RecipeError{Code: "unknown_error_presence"}
}

func transferErrorPresent(t *transferStep) {
	value, ok := t.operand(firstOperand(t.in))
	if !ok || value.Kind != KindError || value.Unknown || t.in.Type != "bool" || suppliedKind(t.in.ValueKind) != KindBoolean {
		t.unknown("unsupported_error_presence")
		return
	}
	recipe, err := errorPresence(t.arena, value.Recipe, t.budget.charge, map[RecipeID]RecipeID{})
	if err != nil {
		t.unknown(reasonCode(err))
		return
	}
	t.output(PrimitiveResult(recipe, "bool", KindBoolean, value.Dependencies))
}

package depth

// recipeRewriter substitutes exact interned leaves and rebuilds their connected
// expressions. Replacement DAGs are shared and are not recursively rewritten.
// This binds recurrence results as well as parameterized helper value recipes.
type recipeRewriter struct {
	builder      *recipeBuilder
	replacements map[RecipeID]RecipeID
	memo         map[RecipeID]RecipeID
	charge       func(int) bool
}

func (w *recipeRewriter) rewrite(id RecipeID) (RecipeID, error) {
	if !w.charge(1) {
		return UnknownRecipeID, &RecipeError{Code: "work_limit", Message: "recipe rewrite"}
	}
	if out, ok := w.memo[id]; ok {
		return out, nil
	}
	if out, ok := w.replacements[id]; ok {
		if _, exists := w.builder.nodes[out]; !exists {
			return UnknownRecipeID, &RecipeError{Code: ReasonInvalidChild, Message: string(out)}
		}
		w.memo[id] = out
		return out, nil
	}
	node, ok := w.builder.nodes[id]
	if !ok {
		return UnknownRecipeID, &RecipeError{Code: ReasonUnknownRecipe, Message: string(id)}
	}
	if node.Kind == KindLoop {
		if out, done, err := pruneLoopInitial(node, w.builder.nodes, w.rewrite); done {
			if err == nil {
				w.memo[id] = out
			}
			return out, err
		}
	}
	if node.Kind == KindSelect {
		return w.selection(node)
	}
	children, err := w.children(node.Children)
	if err != nil {
		return UnknownRecipeID, err
	}
	fields, err := w.fields(node.Fields)
	if err != nil {
		return UnknownRecipeID, err
	}
	out, err := rebuildSubstituted(&recipeSubstituter{recipeMetadata: w.builder.recipeMetadata, builder: w.builder}, node, children, fields, "", "", "")
	if err == nil {
		w.memo[id] = out
	}
	return out, err
}
func (w *recipeRewriter) children(input []RecipeID) ([]RecipeID, error) {
	if !w.charge(len(input)) {
		return nil, &RecipeError{Code: "work_limit", Message: "recipe children"}
	}
	out := make([]RecipeID, len(input))
	for i, id := range input {
		next, err := w.rewrite(id)
		if err != nil {
			return nil, err
		}
		out[i] = next
	}
	return out, nil
}
func (w *recipeRewriter) fields(input []FieldRecipeBinding) ([]FieldRecipeBinding, error) {
	if !w.charge(len(input)) {
		return nil, &RecipeError{Code: "work_limit", Message: "recipe fields"}
	}
	out := make([]FieldRecipeBinding, len(input))
	for i, field := range input {
		next, err := w.rewrite(field.Recipe)
		if err != nil {
			return nil, err
		}
		out[i] = FieldRecipeBinding{field.Field, next}
	}
	return out, nil
}
func (w *recipeRewriter) selection(node recipeNode) (RecipeID, error) {
	predicate, err := w.rewrite(node.Predicate)
	if err != nil {
		return UnknownRecipeID, err
	}
	condition := w.builder.nodes[predicate]
	if condition.Kind == KindConstant {
		switch condition.Literal {
		case "true":
			return w.rememberSelected(node.ID, node.TrueValue)
		case "false":
			return w.rememberSelected(node.ID, node.FalseValue)
		}
	}
	values, err := w.children([]RecipeID{node.TrueValue, node.FalseValue})
	if err != nil {
		return UnknownRecipeID, err
	}
	out, err := w.builder.Select(node.Type, predicate, values[0], values[1])
	if err == nil {
		w.memo[node.ID] = out
	}
	return out, err
}
func (w *recipeRewriter) rememberSelected(id, selected RecipeID) (RecipeID, error) {
	out, err := w.rewrite(selected)
	if err == nil {
		w.memo[id] = out
	}
	return out, err
}

package depth

func booleanLiteral(a *recipeBuilder, id RecipeID, literal string) bool {
	node, ok := a.nodes[id]
	return ok && node.Kind == KindConstant && isBooleanType(node.Type) && node.Literal == literal
}
func simplifySelectPredicate(a *recipeBuilder, predicate, yes, no RecipeID) (RecipeID, RecipeID, RecipeID) {
	node := a.nodes[predicate]
	if node.Kind == KindSelect && isBooleanType(node.Type) && booleanLiteral(a, node.TrueValue, "false") && booleanLiteral(a, node.FalseValue, "true") {
		return node.Predicate, no, yes
	}
	if node.Kind == KindPrimitive && isBooleanType(node.Type) && node.ArithmeticMode == ModeBoolean && node.Operator == "!" && len(node.Children) == 1 {
		return node.Children[0], no, yes
	}
	return predicate, yes, no
}

func simplifySelectBranches(a *recipeBuilder, predicate, yes, no RecipeID) (RecipeID, RecipeID) {
	if node := a.nodes[yes]; node.Kind == KindSelect && node.Predicate == predicate {
		yes = node.TrueValue
	}
	if node := a.nodes[no]; node.Kind == KindSelect && node.Predicate == predicate {
		no = node.FalseValue
	}
	return yes, no
}

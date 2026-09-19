package depth

// recipeAny searches the immutable DAG with bounded shared-node visits. Callers
// retain the charge failure state; false alone is not proof of absence on failure.
func recipeAny(arena *RecipeArena, root RecipeID, charge func(int) bool, match func(RecipeNode) bool) bool {
	seen := map[RecipeID]bool{}
	pending := []RecipeID{root}
	for len(pending) != 0 {
		if !charge(1) {
			return false
		}
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		node, ok := arena.Lookup(id)
		if !ok {
			continue
		}
		if match(node) {
			return true
		}
		if !charge(3 + len(node.Children) + len(node.Fields)) {
			return false
		}
		pending = append(pending, node.Children...)
		for _, field := range node.Fields {
			pending = append(pending, field.Recipe)
		}
		if node.Kind == KindSelect {
			pending = append(pending, node.Predicate, node.TrueValue, node.FalseValue)
		}
	}
	return false
}

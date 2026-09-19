package depth

// selectControlValue preserves precise conditional recipes while joining only
// metadata which is a may fact. Unknown input precision never becomes exact.
func selectControlValue(arena *RecipeArena, p RecipeID, a, b DomainValue) DomainValue {
	if n, ok := arena.Lookup(p); ok && n.Kind == KindConstant {
		if n.Literal == "true" {
			return a.Copy()
		}
		if n.Literal == "false" {
			return b.Copy()
		}
	}
	// Align recipe identities for the metadata join; ordinary Join deliberately
	// widens different recipes and is unsuitable for an exact control choice.
	left, right := a.Copy(), b.Copy()
	left.Recipe = right.Recipe
	out := left.Join(right)
	if a.Type != b.Type || a.Kind != b.Kind {
		return out
	}
	recipe, err := arena.Select(a.Type, p, a.Recipe, b.Recipe)
	out.Recipe = recipe
	if err != nil {
		out.Unknown = true
		out.Reasons = appendUniqueStrings(out.Reasons, reasonCode(err))
	}
	return out
}

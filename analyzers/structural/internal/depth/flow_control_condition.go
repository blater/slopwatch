package depth

func (g *controlGuards) atom(id RecipeID) guardID {
	if !g.charge(1) {
		return guardFalse
	}
	if out, ok := g.atoms[id]; ok {
		return out
	}
	node, known := g.arena.Lookup(id)
	out, handled := g.booleanRecipe(node, known)
	if !handled {
		out = g.node(guardNode{predicate: id, yes: guardTrue, no: guardFalse})
	}
	g.atoms[id] = out
	return out
}
func (g *controlGuards) booleanRecipe(n RecipeNode, known bool) (guardID, bool) {
	if !known {
		return guardFalse, false
	}
	if n.Kind == KindConstant {
		switch n.Literal {
		case "true":
			return guardTrue, true
		case "false":
			return guardFalse, true
		}
	}
	if n.Kind == KindSelect {
		p := g.atom(n.Predicate)
		return g.or(g.and(p, g.atom(n.TrueValue)), g.and(g.not(p), g.atom(n.FalseValue))), true
	}
	if n.Kind == KindPrimitive && n.ArithmeticMode == ModeBoolean {
		return g.booleanPrimitive(n)
	}
	return guardFalse, false
}
func (g *controlGuards) booleanPrimitive(n RecipeNode) (guardID, bool) {
	switch n.Operator {
	case "!", "not":
		return g.not(g.atom(n.Children[0])), true
	case "&&":
		return g.and(g.atom(n.Children[0]), g.atom(n.Children[1])), true
	case "||":
		return g.or(g.atom(n.Children[0]), g.atom(n.Children[1])), true
	case "==", "!=":
		a, b := g.atom(n.Children[0]), g.atom(n.Children[1])
		equal := g.or(g.and(a, b), g.and(g.not(a), g.not(b)))
		if n.Operator == "!=" {
			equal = g.not(equal)
		}
		return equal, true
	}
	return guardFalse, false
}

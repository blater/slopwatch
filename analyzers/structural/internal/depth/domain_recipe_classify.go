package depth

// Classify reports one classification for the connected returned recipe.
func (a *recipeAnalyzer) Classify(id RecipeID) TransformationClass {
	outcome := a.BuildOutcome(id)
	if outcome.Unknown {
		return TransformationUnknown
	}
	return a.classifyMemo(id, make(map[RecipeID]TransformationClass))
}

func (a *recipeAnalyzer) classifyMemo(id RecipeID, memo map[RecipeID]TransformationClass) TransformationClass {
	if class, ok := memo[id]; ok {
		return class
	}
	n, ok := a.nodes[id]
	if !ok || n.Kind == KindUnknown || n.Kind == KindRecurrence {
		return rememberClass(memo, id, TransformationUnknown)
	}
	switch n.Kind {
	case KindLoop:
		return rememberClass(memo, id, classifyNumericLoop(a, n))
	case KindConstant:
		return rememberClass(memo, id, TransformationConstant)
	case KindFormal:
		return rememberClass(memo, id, TransformationIdentity)
	case KindStorage:
		// Allocation/prior-storage identity is evidence of a location, not a
		// transformation obligation.
		return rememberClass(memo, id, TransformationIdentity)
	case KindFieldRead:
		if len(n.Children) == 0 {
			return rememberClass(memo, id, TransformationUnknown)
		}
		return rememberClass(memo, id, a.classifyMemo(n.Children[0], memo))
	case KindPack:
		class := TransformationIdentity
		for _, field := range n.Fields {
			class = combineClassification(class, a.classifyMemo(field.Recipe, memo))
		}
		return rememberClass(memo, id, class)
	case KindSelect:
		if n.TrueValue == n.FalseValue {
			return rememberClass(memo, id, a.classifyMemo(n.TrueValue, memo))
		}
		class := combineClassification(a.classifyMemo(n.Predicate, memo), a.classifyMemo(n.TrueValue, memo))
		class = combineClassification(class, a.classifyMemo(n.FalseValue, memo))
		if class == TransformationUnknown {
			return rememberClass(memo, id, class)
		}
		return rememberClass(memo, id, TransformationPrimitive)
	case KindPrimitive:
		return rememberClass(memo, id, classifyPrimitive(a, n, memo))
	default:
		return rememberClass(memo, id, TransformationUnknown)
	}
}

func rememberClass(memo map[RecipeID]TransformationClass, id RecipeID, class TransformationClass) TransformationClass {
	memo[id] = class
	return class
}

func classifyPrimitive(a *recipeAnalyzer, n recipeNode, memo map[RecipeID]TransformationClass) TransformationClass {
	allConst := true
	for _, child := range n.Children {
		if a.classifyMemo(child, memo) != TransformationConstant {
			allConst = false
			break
		}
	}
	if allConst {
		return TransformationConstant
	}
	if !supportedOperator(n.Operator, n.ArithmeticMode) {
		return TransformationUnknown
	}
	for _, child := range n.Children {
		if a.classifyMemo(child, memo) == TransformationUnknown {
			return TransformationUnknown
		}
	}
	return TransformationPrimitive
}

func combineClassification(left, right TransformationClass) TransformationClass {
	if left == TransformationUnknown || right == TransformationUnknown {
		return TransformationUnknown
	}
	if left == TransformationPrimitive || right == TransformationPrimitive {
		return TransformationPrimitive
	}
	if left == TransformationConstant && right == TransformationConstant {
		return TransformationConstant
	}
	return TransformationIdentity
}

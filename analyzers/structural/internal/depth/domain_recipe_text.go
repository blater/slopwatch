package depth

import "strconv"

func simplifyText(a *recipeStore, typ, op string, operands []RecipeID) (RecipeID, bool) {
	if op != "+" || len(operands) != 2 {
		return "", false
	}
	leftType, leftLiteral, leftConst := constantLiteral(a, operands[0])
	rightType, rightLiteral, rightConst := constantLiteral(a, operands[1])
	if leftConst && rightConst && leftType == typ && rightType == typ {
		left, leftOK := textLiteral(leftLiteral)
		right, rightOK := textLiteral(rightLiteral)
		if leftOK && rightOK {
			return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: strconv.Quote(left + right)}), true
		}
	}
	if leftConst && leftType == typ && isEmptyTextLiteral(leftLiteral) {
		return operands[1], true
	}
	if rightConst && rightType == typ && isEmptyTextLiteral(rightLiteral) {
		return operands[0], true
	}
	return "", false
}

func isEmptyTextLiteral(literal string) bool {
	value, ok := textLiteral(literal)
	return ok && value == ""
}

func textLiteral(literal string) (string, bool) {
	value, err := strconv.Unquote(literal)
	if err != nil {
		return "", false
	}
	return value, true
}

func textConcatenation(arena *RecipeArena, id RecipeID) bool {
	node, ok := arena.Lookup(id)
	return ok && node.Kind == KindPrimitive && node.ArithmeticMode == ModeText && node.Operator == "+" && len(node.Children) == 2
}

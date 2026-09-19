package depth

import (
	"math/big"
	"strconv"
)

// Fold exact normalized integer constants and reflexive comparisons. Floating point/JS comparisons
// and source-specific literal spellings keep their original recipes.
func foldIntegerComparison(a *recipeStore, typ, op string, operands []RecipeID) (RecipeID, bool) {
	if len(operands) != 2 || !isBooleanType(typ) {
		return "", false
	}
	left, right := a.nodes[operands[0]], a.nodes[operands[1]]
	if operands[0] == operands[1] && left.Kind != KindUnknown && left.Kind != KindConstant {
		if value, known := integerComparison(op, 0); known {
			return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: strconv.FormatBool(value)}), true
		}
	}
	if left.Kind != KindConstant || right.Kind != KindConstant || left.Type != right.Type {
		return "", false
	}
	x, ok := normalizedInteger(left.Literal)
	if !ok {
		return "", false
	}
	y, ok := normalizedInteger(right.Literal)
	if !ok {
		return "", false
	}
	value, known := integerComparison(op, x.Cmp(y))
	if !known {
		return "", false
	}
	return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: strconv.FormatBool(value)}), true
}
func normalizedInteger(text string) (*big.Int, bool) {
	if len(text) == 0 || len(text) > 256 {
		return nil, false
	}
	digits := text
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" {
		return nil, false
	}
	if len(digits) > 1 && digits[0] == '0' {
		return nil, false
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return nil, false
		}
	}
	return new(big.Int).SetString(text, 10)
}
func integerComparison(op string, comparison int) (bool, bool) {
	switch op {
	case "==":
		return comparison == 0, true
	case "!=":
		return comparison != 0, true
	case "<":
		return comparison < 0, true
	case "<=":
		return comparison <= 0, true
	case ">":
		return comparison > 0, true
	case ">=":
		return comparison >= 0, true
	}
	return false, false
}

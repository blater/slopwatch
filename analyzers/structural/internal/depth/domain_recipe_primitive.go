package depth

import (
	"strconv"
	"strings"
)

type primitiveSpec struct{ arity int }

var primitiveSpecs = map[ArithmeticMode]map[string]primitiveSpec{
	ModeInteger:  specs(2, "+", "-", "*", "/", "%", "|", "&", "^", "==", "!=", "<", "<=", ">", ">="),
	ModeWrapping: specs(2, "+", "-", "*", "/", "%", "|", "&", "^", "==", "!=", "<", "<=", ">", ">="),
	ModeFloat:    specs(2, "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">="),
	ModeBoolean:  specs(2, "&&", "||", "==", "!="),
	ModeJSNumber: specs(2, "+", "-", "*", "/", "%", "|", "&", "^", "&&", "||", "==", "!=", "<", "<=", ">", ">="),
	ModeText:     specs(2, "+"),
}

func specs(arity int, operators ...string) map[string]primitiveSpec {
	out := make(map[string]primitiveSpec, len(operators))
	for _, operator := range operators {
		out[operator] = primitiveSpec{arity: arity}
	}
	return out
}

func primitiveSpecFor(mode ArithmeticMode, op string, operands int) (primitiveSpec, bool) {
	byMode, ok := primitiveSpecs[mode]
	if !ok {
		return primitiveSpec{}, false
	}
	if operands == 1 {
		switch op {
		case "!", "not":
			return primitiveSpec{arity: 1}, mode == ModeBoolean || mode == ModeJSNumber
		case "neg", "~", "-":
			return primitiveSpec{arity: 1}, mode == ModeInteger || mode == ModeWrapping || mode == ModeFloat || mode == ModeJSNumber
		case "bind":
			return primitiveSpec{arity: 1}, true
		}
	}
	spec, ok := byMode[op]
	return spec, ok && operands == spec.arity
}

func supportedOperator(op string, mode ArithmeticMode) bool {
	_, binary := primitiveSpecFor(mode, op, 2)
	_, unary := primitiveSpecFor(mode, op, 1)
	return binary || unary
}

func isIntegerType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch t {
	case "", "integer", "byte", "rune", "long", "usize", "isize", "i32", "i64":
		return true
	}
	return strings.HasPrefix(t, "int") || strings.HasPrefix(t, "uint")
}

func isFloatType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	return t == "" || strings.HasPrefix(t, "float") || t == "number" || t == "double" || t == "f32" || t == "f64"
}

func isBooleanType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	return t == "" || t == "bool" || t == "boolean"
}

func compatibleOperand(mode ArithmeticMode, resultType, operandType string) bool {
	if operandType == "" {
		return true
	}
	switch mode {
	case ModeInteger, ModeWrapping:
		return isIntegerType(operandType)
	case ModeFloat:
		return isFloatType(operandType)
	case ModeBoolean:
		return isBooleanType(operandType)
	case ModeJSNumber:
		return isIntegerType(operandType) || isFloatType(operandType) || isBooleanType(operandType) || strings.EqualFold(operandType, resultType)
	case ModeText:
		return strings.TrimSpace(operandType) == strings.TrimSpace(resultType) && strings.TrimSpace(resultType) == "string"
	default:
		return false
	}
}

func constantLiteral(a *recipeStore, id RecipeID) (string, string, bool) {
	n, ok := a.nodes[id]
	if !ok || n.Kind != KindConstant {
		return "", "", false
	}
	return n.Type, n.Literal, true
}

func isZero(typ, lit string) bool {
	s := strings.TrimSpace(lit)
	if typ == "bool" || typ == "boolean" {
		return false
	}
	if i, err := strconv.ParseInt(s, 0, 64); err == nil {
		return i == 0
	}
	return s == "0" || s == "0.0" || s == "-0" || s == "-0.0"
}

func isOne(typ, lit string) bool {
	s := strings.TrimSpace(lit)
	if typ == "bool" || typ == "boolean" {
		return false
	}
	if i, err := strconv.ParseInt(s, 0, 64); err == nil {
		return i == 1
	}
	return s == "1" || s == "1.0"
}

func isBool(typ, lit string, want bool) bool {
	return isBooleanType(typ) && strings.EqualFold(strings.TrimSpace(lit), strconv.FormatBool(want))
}

func buildPrimitive(a *recipeStore, typ string, mode ArithmeticMode, op string, operands []RecipeID) (RecipeID, error) {
	if err := a.validateChildren(operands, 1, -1); err != nil {
		return a.unknownResult(err.(*RecipeError).Code, err.Error())
	}
	spec, ok := primitiveSpecFor(mode, op, len(operands))
	if !ok {
		return unknownPrimitive(a, mode, op)
	}
	if len(operands) != spec.arity {
		return a.unknownResult(ReasonInvalidArity, op)
	}
	if err := validateOperandTypes(a, mode, typ, operands); err != nil {
		return a.unknownResult(err.Code, err.Message)
	}
	if simplified, changed := simplifyPrimitive(a, typ, mode, op, operands); changed {
		return simplified, nil
	}
	return a.intern(RecipeNode{Kind: KindPrimitive, Type: typ, Operator: op, ArithmeticMode: mode, Children: append([]RecipeID(nil), operands...)}), nil
}

func unknownPrimitive(a *recipeStore, mode ArithmeticMode, op string) (RecipeID, error) {
	if _, ok := primitiveSpecs[mode]; !ok {
		return a.unknownResult(ReasonUnknownArithmetic, string(mode))
	}
	return a.unknownResult(ReasonUnknownOperator, op)
}

func validateOperandTypes(a *recipeStore, mode ArithmeticMode, resultType string, operands []RecipeID) *RecipeError {
	for _, operand := range operands {
		node := a.nodes[operand]
		if !compatibleOperand(mode, resultType, node.Type) {
			return &RecipeError{Code: ReasonInvalidType, Message: node.Type}
		}
	}
	return nil
}

func simplifyPrimitive(a *recipeStore, typ string, mode ArithmeticMode, op string, operands []RecipeID) (RecipeID, bool) {
	if op == "bind" {
		return operands[0], true
	}
	if mode == ModeBoolean && !exactBooleanOperands(a, typ, operands) {
		return "", false
	}
	if (op == "!" || op == "not") && (mode == ModeBoolean || mode == ModeJSNumber) {
		return simplifyBooleanNot(a, mode, operands[0])
	}
	if mode == ModeInteger || mode == ModeWrapping {
		if result, ok := foldIntegerComparison(a, typ, op, operands); ok {
			return result, true
		}
		return simplifyInteger(a, op, operands)
	}
	if mode == ModeBoolean {
		return simplifyBoolean(a, op, operands)
	}
	if mode == ModeText {
		return simplifyText(a, typ, op, operands)
	}
	return "", false
}

func simplifyBooleanNot(a *recipeStore, mode ArithmeticMode, operand RecipeID) (RecipeID, bool) {
	node := a.nodes[operand]
	if mode == ModeBoolean && node.Kind == KindPrimitive && node.ArithmeticMode == ModeBoolean && (node.Operator == "!" || node.Operator == "not") && len(node.Children) == 1 && a.nodes[node.Children[0]].Type == node.Type {
		return node.Children[0], true
	}
	typ, literal, ok := constantLiteral(a, operand)
	if !ok {
		return "", false
	}
	if isBool(typ, literal, true) {
		return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: "false"}), true
	}
	if isBool(typ, literal, false) {
		return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: "true"}), true
	}
	return "", false
}

func simplifyInteger(a *recipeStore, op string, operands []RecipeID) (RecipeID, bool) {
	if len(operands) != 2 {
		return "", false
	}
	leftType, leftLiteral, leftConst := constantLiteral(a, operands[0])
	rightType, rightLiteral, rightConst := constantLiteral(a, operands[1])
	rightZero := rightConst && isZero(rightType, rightLiteral)
	leftZero := leftConst && isZero(leftType, leftLiteral)
	rightOne := rightConst && isOne(rightType, rightLiteral)
	leftOne := leftConst && isOne(leftType, leftLiteral)
	switch op {
	case "+":
		return chooseIdentity(operands[0], rightZero, operands[1], leftZero)
	case "-":
		return operands[0], rightZero
	case "*":
		return chooseIdentity(operands[0], rightOne, operands[1], leftOne)
	case "/":
		return operands[0], rightOne
	case "|", "^":
		return operands[0], rightZero
	default:
		return "", false
	}
}

func chooseIdentity(left RecipeID, leftValid bool, right RecipeID, rightValid bool) (RecipeID, bool) {
	if leftValid {
		return left, true
	}
	if rightValid {
		return right, true
	}
	return "", false
}

func simplifyBoolean(a *recipeStore, op string, operands []RecipeID) (RecipeID, bool) {
	if len(operands) != 2 {
		return "", false
	}
	leftType, leftLiteral, leftConst := constantLiteral(a, operands[0])
	rightType, rightLiteral, rightConst := constantLiteral(a, operands[1])
	switch op {
	case "==":
		return chooseIdentity(operands[0], rightConst && isBool(rightType, rightLiteral, true), operands[1], leftConst && isBool(leftType, leftLiteral, true))
	case "!=":
		return chooseIdentity(operands[0], rightConst && isBool(rightType, rightLiteral, false), operands[1], leftConst && isBool(leftType, leftLiteral, false))
	case "&&":
		return chooseIdentity(operands[0], rightConst && isBool(rightType, rightLiteral, true), operands[1], leftConst && isBool(leftType, leftLiteral, true))
	case "||":
		return chooseIdentity(operands[0], rightConst && isBool(rightType, rightLiteral, false), operands[1], leftConst && isBool(leftType, leftLiteral, false))
	default:
		return "", false
	}
}

func exactBooleanOperands(a *recipeStore, typ string, operands []RecipeID) bool {
	if typ != "bool" && typ != "boolean" {
		return false
	}
	for _, operand := range operands {
		if a.nodes[operand].Type != typ {
			return false
		}
	}
	return true
}

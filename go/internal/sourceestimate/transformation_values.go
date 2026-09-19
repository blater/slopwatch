package sourceestimate

import "strings"

type transformationExpr struct {
	form           string
	args           []*transformationExpr
	dependsOnInput bool
	transformed    bool
	stringLike     bool
}

func (e *transformationExpr) key() string {
	if e == nil {
		return ""
	}
	if len(e.args) == 0 {
		return e.form
	}
	parts := make([]string, len(e.args))
	for i, arg := range e.args {
		parts[i] = arg.key()
	}
	return e.form + "(" + strings.Join(parts, ",") + ")"
}

func combineTransformation(operator string, left, right *transformationExpr) *transformationExpr {
	if left == nil || right == nil {
		return nil
	}
	if neutralTransformation(operator, left, right) {
		if operator == "*" && isTransformationConstant(left, "0") {
			return &transformationExpr{form: "0"}
		}
		if operator == "*" && isTransformationConstant(right, "0") {
			return &transformationExpr{form: "0"}
		}
		if operator == "-" && left.key() == right.key() {
			return &transformationExpr{form: "0"}
		}
		if isTransformationConstant(left, "0") || isTransformationConstant(left, "__empty_string__") {
			return right
		}
		return left
	}
	return &transformationExpr{
		form:           operator,
		args:           []*transformationExpr{left, right},
		dependsOnInput: left.dependsOnInput || right.dependsOnInput,
		transformed:    left.transformed || right.transformed || (left.dependsOnInput || right.dependsOnInput) && isTransformationOperator(operator),
		stringLike:     operator == "+" && (left.stringLike || right.stringLike),
	}
}

func neutralTransformation(operator string, left, right *transformationExpr) bool {
	switch operator {
	case "+":
		return (!left.stringLike && !right.stringLike && (isTransformationConstant(left, "0") || isTransformationConstant(right, "0"))) || isTransformationConstant(left, "__empty_string__") || isTransformationConstant(right, "__empty_string__")
	case "-":
		return isTransformationConstant(right, "0")
	case "*":
		return isTransformationConstant(left, "0") || isTransformationConstant(left, "1") || isTransformationConstant(right, "0") || isTransformationConstant(right, "1")
	case "/":
		return isTransformationConstant(right, "1")
	}
	return false
}

func isTransformationConstant(value *transformationExpr, text string) bool {
	return value != nil && !value.dependsOnInput && value.form == text
}

func isTransformationOperator(operator string) bool {
	switch operator {
	case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>":
		return true
	default:
		return false
	}
}

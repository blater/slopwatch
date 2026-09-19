package sourceestimate

func localCategories(body []token, op *operation, u unit) map[string]bool {
	result := map[string]bool{}
	if hasValidationContext(body, op.language, gradedBuiltinPanic(op, u)) {
		result["validation"] = true
	}
	if hasState(body) {
		result["state"] = true
	}
	return result
}

func hasTransform(body []token) bool {
	for i, item := range body {
		if !transformOperator(item.text) {
			continue
		}
		left, right := adjacentToken(body, i-1), adjacentToken(body, i+1)
		if trivialTransform(item.text, left, right) || unaryOperator(body, i) {
			continue
		}
		return true
	}
	return false
}

func transformOperator(text string) bool {
	switch text {
	case "+", "-", "*", "/", "%", "&", "|", "^":
		return true
	default:
		return false
	}
}

func adjacentToken(body []token, index int) token {
	if index < 0 || index >= len(body) {
		return token{}
	}
	return body[index]
}

func trivialTransform(operator string, left, right token) bool {
	if operator == "+" && noopAddition(left, right) {
		return true
	}
	if operator == "-" && right.text == "0" || operator == "/" && right.text == "1" {
		return true
	}
	return operator == "*" && (left.text == "0" || left.text == "1" || right.text == "0" || right.text == "1")
}

func unaryOperator(body []token, index int) bool {
	return (body[index].text == "+" || body[index].text == "-") && (index == 0 || isOperator(body[index-1].text))
}

func noopAddition(left, right token) bool {
	if left.kind == "stringvar" || right.kind == "stringvar" || left.kind == "string" && left.text == "__string__" || right.kind == "string" && right.text == "__string__" {
		return false
	}
	if left.text == "__empty_string__" || right.text == "__empty_string__" {
		return true
	}
	return left.text == "0" || right.text == "0"
}

func hasValidation(body []token) bool { return hasValidationContext(body, "", false) }

func hasValidationContext(body []token, language string, builtinPanic bool) bool {
	guard, guardedThrow := false, false
	returns := 0
	for i, item := range body {
		switch item.text {
		case "if", "match", "switch", "assert":
			if !validationGuard(item.text, language) || invalidGuardStart(body, i) {
				continue
			}
			guard = true
		case "throw", "panic":
			if !validationThrow(item.text, language, builtinPanic, body, i) {
				continue
			}
			if guard {
				guardedThrow = true
			}
		case "return":
			returns++
		}
	}
	if guardedThrow {
		return true
	}
	if !guard || returns < 2 {
		return false
	}
	values := returnedValues(body)
	return len(values) >= 2 && values[0] != values[1]
}

func validationGuard(text, language string) bool {
	return !(text == "match" && language != "rust") && !(text == "assert" && language != "java")
}

func invalidGuardStart(body []token, index int) bool {
	if index+1 >= len(body) || body[index+1].text == "=" || body[index+1].text == ";" {
		return true
	}
	return body[index].text == "if" && index+3 < len(body) && body[index+2].text == "false" && body[index+3].text == "&&"
}

func validationThrow(text, language string, builtinPanic bool, body []token, index int) bool {
	if text == "throw" {
		return language == "java" || language == "typescript"
	}
	return builtinPanic && language == "go" && index+1 < len(body) && body[index+1].text == "(" && (index == 0 || body[index-1].text != ".")
}

func returnedValues(body []token) []string {
	values := make([]string, 0, 2)
	for i, item := range body {
		if item.text != "return" || i+1 >= len(body) {
			continue
		}
		end := i + 1
		for end < len(body) && body[end].text != ";" && body[end].text != "}" {
			end++
		}
		values = append(values, joinTokens(body[i+1:end]))
		if len(values) == 2 {
			break
		}
	}
	return values
}

func hasState(body []token) bool {
	for i := 0; i+3 < len(body); i++ {
		if !isMember(body, i) {
			continue
		}
		if memberUpdate(body, i) {
			return true
		}
	}
	for i := 1; i+2 < len(body); i++ {
		if (body[i-1].text == "++" || body[i-1].text == "--") && isMember(body, i) {
			return true
		}
	}
	return false
}

func memberUpdate(body []token, index int) bool {
	switch body[index+3].text {
	case "++", "--":
		return true
	case "+=", "-=", "*=", "/=", "%=":
		end := statementEnd(body, index+4)
		return compoundUpdateChanges(body[index+4:end], body[index+3].text)
	case "=":
		end := statementEnd(body, index+4)
		return readsAndChangesMember(body[index:index+3], body[index+4:end])
	default:
		return false
	}
}

func isMember(body []token, start int) bool {
	return start+2 < len(body) && isIdentifier(body[start].text) && body[start+1].text == "." && isIdentifier(body[start+2].text)
}

func statementEnd(body []token, start int) int {
	for i := start; i < len(body); i++ {
		if body[i].text == ";" || body[i].text == "}" {
			return i
		}
	}
	return len(body)
}

func compoundUpdateChanges(rhs []token, operator string) bool {
	if len(rhs) != 1 || rhs[0].kind != "number" {
		return true
	}
	switch operator {
	case "+=", "-=":
		return rhs[0].text != "0"
	case "*=", "/=":
		return rhs[0].text != "1"
	default:
		return true
	}
}

func readsAndChangesMember(member, rhs []token) bool {
	for i := 0; i+2 < len(rhs); i++ {
		if sameMember(member, rhs[i:i+3]) {
			return hasTransform(rhs)
		}
	}
	return false
}

func sameMember(left, right []token) bool {
	return len(left) == 3 && len(right) >= 3 && left[0].text == right[0].text && left[1].text == right[1].text && left[2].text == right[2].text
}

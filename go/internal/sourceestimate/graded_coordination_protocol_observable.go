package sourceestimate

func gradedObservableProtocolRead(op *operation, u unit, index int) bool {
	body := op.body
	start := index
	for start > 0 && body[start-1].text != ";" && body[start-1].text != "{" && body[start-1].text != "}" {
		start--
	}
	for i := start; i < index; i++ {
		if body[i].text == "return" {
			return true
		}
		if body[i].text == "=" && i > 0 && gradeOwnedFieldReference(op, u, body, i-1) {
			if _, ok := callerDeclaredFields(u, op.owner)[body[i-1].text]; ok {
				return true
			}
		}
	}
	for i := 0; i < index; i++ {
		if body[i].text == "if" {
			conditionStart, conditionEnd := i+1, i+1
			if conditionStart < len(body) && body[conditionStart].text == "(" {
				conditionEnd = matching(body, conditionStart, "(", ")")
			} else {
				for conditionEnd < len(body) && body[conditionEnd].text != "{" {
					conditionEnd++
				}
			}
			if index > conditionStart && index < conditionEnd && hasValidationContext(body, op.language, gradedBuiltinPanic(op, u)) {
				return true
			}
		}
		if op.language == "rust" && (body[i].text == "assert" || body[i].text == "assert_eq") && i+2 < len(body) && body[i+1].text == "!" && body[i+2].text == "(" && matching(body, i+2, "(", ")") > index {
			return true
		}
	}
	return false
}

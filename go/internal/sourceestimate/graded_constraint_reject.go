package sourceestimate

func gradedConstraintRejects(op *operation, u unit, body []token, end int) bool {
	for i := end; i < len(body); i++ {
		if body[i].text == "throw" || gradedConstraintBuiltinReject(op, u, body, i) {
			return true
		}
		if body[i].text == "}" || body[i].text == ";" {
			break
		}
	}
	for i := end - 1; i >= 0 && body[i].text != ";" && body[i].text != "{"; i-- {
		if body[i].text == "throw" || gradedConstraintBuiltinReject(op, u, body, i) {
			return true
		}
	}
	return false
}
func gradedConstraintBuiltinReject(op *operation, u unit, body []token, i int) bool {
	name := body[i].text
	if op.language == "go" && name == "panic" && i+1 < len(body) && body[i+1].text == "(" {
		return gradedBuiltinName(u, op, name)
	}
	if op.language == "rust" && name == "assert" && i+1 < len(body) && body[i+1].text == "!" {
		for j := 0; j+2 < len(u.tokens); j++ {
			if u.tokens[j].text == "macro_rules" && u.tokens[j+2].text == name {
				return false
			}
		}
		return true
	}
	return false
}

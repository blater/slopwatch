package sourceestimate

func gradedBooleanCondition(body []token, field string, value bool) (bool, bool) {
	for len(body) > 1 && body[0].text == "(" && matching(body, 0, "(", ")") == len(body)-1 {
		body = body[1 : len(body)-1]
	}
	if len(body) > 0 && body[0].text == "!" {
		v, ok := gradedBooleanCondition(body[1:], field, value)
		return !v, ok
	}
	if len(body) == 1 && body[0].text == field {
		return value, true
	}
	if len(body) == 3 && body[1].text == "." && body[2].text == field {
		return value, true
	}
	for i, t := range body {
		if t.text == "==" || t.text == "!=" {
			a, ok := gradedBooleanCondition(body[:i], field, value)
			if !ok || i+2 != len(body) {
				return false, false
			}
			b := body[i+1].text
			if b != "true" && b != "false" {
				return false, false
			}
			result := a == (b == "true")
			if t.text == "!=" {
				result = !result
			}
			return result, true
		}
	}
	return false, false
}

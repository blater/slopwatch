package sourceestimate

func straightLinePrefix(body []token) bool {
	for i, item := range body {
		if (item.text == "panic" || item.text == "raise") && i+1 < len(body) && body[i+1].text == "(" {
			return false
		}
		switch item.text {
		case "if", "for", "while", "switch", "match", "try", "catch", "defer", "throw", "await", "yield", "go", "select":
			return false
		}
	}
	return true
}

// pruneDeadFalseBranches removes only syntactically explicit always-false if
// branches. It does not attempt general constant folding, so an uncertain
// condition still makes the caller incomplete. The result is read-only and may
// share the input backing array; capped capacity makes appends safe.
func pruneDeadFalseBranches(body []token) []token {
	if len(body) == 0 {
		return body[:0:0]
	}
	var result []token
	for index := 0; index < len(body); {
		if body[index].text == "if" {
			if end, ok := deadFalseBranchEnd(body, index); ok {
				if result == nil {
					result = make([]token, index, len(body))
					copy(result, body[:index])
				}
				index = end
				continue
			}
		}
		if result != nil {
			result = append(result, body[index])
		}
		index++
	}
	if result == nil {
		return body[:len(body):len(body)]
	}
	return result[:len(result):len(result)]
}

func deadFalseBranchEnd(body []token, start int) (int, bool) {
	conditionStart := start + 1
	conditionEnd := conditionStart
	if conditionStart < len(body) && body[conditionStart].text == "(" {
		close := matching(body, conditionStart, "(", ")")
		if close < 0 {
			return 0, false
		}
		conditionStart++
		conditionEnd = close
	} else {
		for conditionEnd < len(body) && body[conditionEnd].text != "{" && body[conditionEnd].text != ";" {
			conditionEnd++
		}
	}
	condition := body[conditionStart:conditionEnd]
	if !alwaysFalseCondition(condition) {
		return 0, false
	}
	bodyStart := conditionEnd
	if bodyStart < len(body) && body[bodyStart].text == ")" {
		bodyStart++
	}
	if bodyStart < len(body) && body[bodyStart].text == "{" {
		close := matching(body, bodyStart, "{", "}")
		if close < 0 || close+1 < len(body) && body[close+1].text == "else" {
			return 0, false
		}
		return close + 1, true
	}
	for bodyStart < len(body) && body[bodyStart].text != ";" {
		bodyStart++
	}
	if bodyStart < len(body) {
		return bodyStart + 1, true
	}
	return len(body), true
}

func alwaysFalseCondition(condition []token) bool {
	for len(condition) >= 2 && condition[0].text == "(" && condition[len(condition)-1].text == ")" {
		if matching(condition, 0, "(", ")") != len(condition)-1 {
			break
		}
		condition = condition[1 : len(condition)-1]
	}
	return len(condition) == 1 && condition[0].text == "false"
}

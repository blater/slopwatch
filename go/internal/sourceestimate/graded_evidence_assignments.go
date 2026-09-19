package sourceestimate

func gradeComputedAssignment(op *operation, u unit, body []token) bool {
	for _, effect := range gradedStorageEffects(op, u, body) {
		if effect.computed {
			return true
		}
	}
	return false
}
func gradedRustReturnedMatchCalls(arms []token) bool {
	for i, t := range arms {
		if t.text != "=>" {
			continue
		}
		start, end := i+1, i+1
		if start >= len(arms) {
			continue
		}
		if arms[start].text == "{" {
			close := matching(arms, start, "{", "}")
			if close < 0 {
				continue
			}
			statements := gradedResultStatements(arms[start+1 : close])
			for j, statement := range statements {
				if len(statement) == 0 {
					continue
				}
				if statement[0].text == "return" {
					if len(callsIn(statement[1:])) > 0 || hasTransform(statement[1:]) {
						return true
					}
				} else if j == len(statements)-1 && arms[close-1].text != ";" && !gradedResultControl(statement[0].text) {
					assignment := false
					for _, tok := range statement {
						assignment = assignment || tok.text == "=" || tok.text == ":="
					}
					if !assignment && (len(callsIn(statement)) > 0 || hasTransform(statement)) {
						return true
					}
				}
			}
			continue
		}
		depth := 0
		for end < len(arms) {
			v := arms[end].text
			if depth == 0 && v == "," {
				break
			}
			switch v {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
			end++
		}
		if len(callsIn(arms[start:end])) > 0 || hasTransform(arms[start:end]) {
			return true
		}
	}
	return false
}
func gradeAssertion(body []token) bool {
	for i := 0; i+1 < len(body); i++ {
		if (body[i].text == "assert" || body[i].text == "assert_eq") && body[i+1].text == "!" {
			return true
		}
	}
	return false
}

package sourceestimate

import "strings"

func gradedPossibleOwnedProjection(roots []*operation, u unit) bool {
	returned := map[string]bool{}
	for _, op := range roots {
		for name := range gradedDirectReturnedFields(op, u) {
			returned[name] = true
		}
	}

	if len(returned) == 0 {
		return false
	}
	for _, op := range roots {
		body := pruneDeadFalseBranches(op.body)
		for i, t := range body {
			if t.text != "for" || i+1 >= len(body) || body[i+1].text != "(" {
				continue
			}
			end := matching(body, i+1, "(", ")")
			if end < 0 || end+1 >= len(body) || body[end+1].text != "{" {
				continue
			}
			colon := -1
			for j := i + 2; j < end; j++ {
				if body[j].text == ":" {
					colon = j
					break
				}
			}
			if colon <= i+2 {
				continue
			}
			element := body[colon-1].text
			start := colon + 1
			if start+2 < end && body[start].text == "this" && body[start+1].text == "." {
				start += 2
			}
			ownerConnected := start < end && returned[body[start].text] && gradeOwnedFieldReference(op, u, body, start)

			if !ownerConnected {
				continue
			}
			close := matching(body, end+1, "{", "}")
			if close < 0 {
				continue
			}
			segment := body[end+2 : close]
			invalid := false
			for j, t := range segment {
				if t.text == element && gradedStorageWriteAt(segment, j) {
					invalid = true
				}
			}
			if invalid {
				continue
			}
			for _, c := range callsIn(segment) {
				if !strings.HasPrefix(c.name, element+".") || len(c.actuals) == 0 {
					continue
				}
				for _, arg := range c.actuals {
					if len(callsIn(arg)) > 0 {
						return true
					}
				}
			}
		}
	}
	return false
}
func gradedDirectReturnedFields(op *operation, u unit) map[string]bool {
	result := map[string]bool{}
	fields := callerDeclaredFields(u, op.owner)
	for _, statement := range gradedResultStatements(op.body) {
		if len(statement) < 2 || statement[0].text != "return" {
			continue
		}
		expression := statement[1:]
		arms := [][]token{expression}
		question, colon := -1, -1
		for i, t := range expression {
			if t.text == "?" {
				question = i
			}
			if t.text == ":" {
				colon = i
			}
		}
		if question >= 0 && colon > question {
			arms = [][]token{expression[question+1 : colon], expression[colon+1:]}
		}
		for _, arm := range arms {
			arm = trimSemicolonTokens(arm)
			for len(arm) > 1 && arm[0].text == "(" && matching(arm, 0, "(", ")") == len(arm)-1 {
				arm = arm[1 : len(arm)-1]
			}
			index := 0
			if len(arm) == 3 && arm[0].text == "this" && arm[1].text == "." {
				index = 2
			} else if len(arm) != 1 {
				continue
			}
			if _, ok := fields[arm[index].text]; ok && gradeOwnedFieldReference(op, u, arm, index) {
				result[arm[index].text] = true
			}
		}
	}
	return result
}

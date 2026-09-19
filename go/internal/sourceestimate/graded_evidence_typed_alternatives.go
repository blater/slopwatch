package sourceestimate

import "strings"

func gradeTypedAlternatives(op *operation, u unit, body []token) bool {
	body = gradedEagerBody(pruneDeadFalseBranches(body), op.language)
	ranges := [][2]int{}
	for i, t := range body {
		if t.text != "switch" && t.text != "match" {
			continue
		}
		open := i + 1
		for open < len(body) && body[open].text != "{" {
			open++
		}
		if open >= len(body) {
			continue
		}
		close := matching(body, open, "{", "}")
		if close < 0 {
			continue
		}
		ranges = append(ranges, [2]int{open, close})
	}
	inside := func(index int) bool {
		for _, span := range ranges {
			if index > span[0] && index < span[1] {
				return true
			}
		}
		return false
	}
	if len(ranges) == 0 {
		return false
	}

	// Rust's final match is itself the returned expression.
	if op.language == "rust" && op.returnType != "" && op.returnType != "()" && len(body) > 0 && body[len(body)-1].text == "}" {
		for _, statement := range gradedResultStatements(body) {
			if len(statement) == 0 || statement[0].text != "match" {
				continue
			}
			open := 0
			for open < len(statement) && statement[open].text != "{" {
				open++
			}
			if open < len(statement) && statement[len(statement)-1].offset == body[len(body)-1].offset && gradedRustReturnedMatchCalls(statement[open+1:len(statement)-1]) {
				return true
			}
		}
	}
	for _, c := range callsIn(body) {
		if !inside(c.position) {
			continue
		}
		for _, buffer := range op.outputBuffers {
			if strings.HasPrefix(c.name, buffer+".") {
				for _, argument := range c.actuals {
					if len(callsIn(argument)) > 0 {
						return true
					}
				}
			}
		}
	}
	// A branch's call is useful only when its result reaches a returned
	// expression. Calls/calculations in discarded branch locals do not qualify.
	for i, tok := range body {
		if !inside(i) {
			continue
		}
		if tok.text == "return" {
			end := statementEnd(body, i+1)
			if len(callsIn(body[i+1:end])) > 0 || hasTransform(body[i+1:end]) {
				return true
			}
		}
		if tok.text == "=" && i > 0 && gradeOwnedFieldReference(op, u, body, i-1) {
			end := statementEnd(body, i+1)
			for j := i + 1; j < end; j++ {
				if body[j].text == "{" && matching(body, j, "{", "}") >= 0 {
					return true
				}
			}
		}
	}
	return false
}

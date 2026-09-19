package sourceestimate

import "strings"

func gradedBooleanWrites(op *operation, units []unit, value string) []string {
	fields := callerDeclaredFields(units[op.file], op.owner)
	final := map[string]bool{}
	lastWrite := map[string]int{}
	for i := 0; i+1 < len(op.body); i++ {
		if _, ok := fields[op.body[i].text]; !ok || !gradeOwnedFieldReference(op, units[op.file], op.body, i) {
			continue
		}
		if !gradedStorageWriteAt(op.body, i) {
			continue
		}
		lastWrite[op.body[i].text] = i
		final[op.body[i].text] = gradedExactBooleanAssignment(op.body, i, value) && operationBodyUnconditional(op, op.body, i)
	}
	// A later call may restore or otherwise mutate the same protocol state.
	// Without a complete call effect contract, the earlier literal is not a
	// final-value proof (including when that call resolves to reacquisition).
	for _, c := range callsIn(op.body) {
		for field, position := range lastWrite {
			if c.position > position {
				final[field] = false
			}
		}
	}
	result := []string{}
	for name, valid := range final {
		if valid {
			result = append(result, name)
		}
	}
	return result
}
func gradedExactBooleanAssignment(body []token, index int, value string) bool {
	if index+2 >= len(body) || body[index+1].text != "=" {
		return false
	}
	end := statementEnd(body, index+2)
	return end == index+3 && body[index+2].text == value
}
func gradedRustCleanupReset(function rustFunction, u unit, depth int) map[string]string {
	if depth >= maxCallDepth {
		return nil
	}
	candidate := &operation{language: "rust", owner: function.owner, file: 0, body: u.tokens[function.bodyStart+1 : function.bodyEnd]}
	reset := gradedCleanupResetFields(candidate, []unit{u})
	if len(reset) > 0 {
		return reset
	}
	calls := callsIn(candidate.body)
	if len(calls) != 1 || gradedHasAssignment(candidate.body) || !strings.HasPrefix(calls[0].name, "self.") || !operationBodyUnconditional(candidate, candidate.body, calls[0].position) {
		return nil
	}
	name := strings.TrimPrefix(calls[0].name, "self.")
	for _, helper := range rustUnitMembers(u, function.owner, name) {
		if helper.owner == function.owner && helper.name == name {
			return gradedRustCleanupReset(helper, u, depth+1)
		}
	}
	return nil
}
func gradedStorageWriteAt(body []token, field int) bool {
	if field+1 >= len(body) {
		return false
	}
	switch body[field+1].text {
	case "=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", ">>>=", "&&=", "||=", "??=", "++", "--":
		return true
	case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>", ">>>", "&&", "||", "??":
		return field+2 < len(body) && body[field+2].text == "="
	}
	start := gradedStorageReferenceStart(body, field)
	return start > 0 && (body[start-1].text == "++" || body[start-1].text == "--")
}
func gradedTryProtected(op *operation, body []token, finally int) (int, int) {
	for i := finally - 1; i >= 0; i-- {
		if body[i].text != "try" || i+1 >= len(body) || body[i+1].text != "{" {
			continue
		}
		close := matching(body, i+1, "{", "}")
		if close < 0 {
			continue
		}
		next := close + 1
		for next < finally && body[next].text == "catch" {
			start := next + 1
			if start < len(body) && body[start].text == "(" {
				end := matching(body, start, "(", ")")
				if end < 0 {
					break
				}
				start = end + 1
			}
			if start >= len(body) || body[start].text != "{" {
				break
			}
			end := matching(body, start, "{", "}")
			if end < 0 {
				break
			}
			next = end + 1
		}
		if next == finally && operationBodyUnconditional(op, body, i) {
			return i + 2, finally - 1
		}
	}
	return -1, -1
}
func gradedOperationWritesField(op *operation, u unit, field string) bool {
	for i, tok := range op.body {
		if tok.text == field && gradeOwnedFieldReference(op, u, op.body, i) && gradedStorageWriteAt(op.body, i) {
			return true
		}
	}
	return false
}

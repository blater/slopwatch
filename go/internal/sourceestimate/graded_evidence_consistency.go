package sourceestimate

func gradeConsistentWrites(op *operation, u unit, body []token) bool {
	if op.language == "go" && !gradedMutableGoReceiver(op, u) {
		return false
	}
	for _, constraint := range gradedOwnerConstraints(u, op.owner, u.ops) {
		if constraint.derivedTarget != "" {
			stored := map[string]string{}
			for i, t := range body {
				if t.text != "=" || i == 0 || !gradeOwnedFieldReference(op, u, body, i-1) || !gradedUnconditional(body, i) {
					continue
				}
				rhs := gradedConstraintExpression(op, u, body[i+1:statementEnd(body, i+1)])
				if body[i-1].text == constraint.derivedTarget {
					expectedTokens, _, _ := lex([]byte(constraint.derivedExpression))
					expected := ""
					connected := false
					for _, token := range expectedTokens {
						if replacement, ok := stored[token.text]; ok {
							expected += replacement
							connected = true
						} else {
							expected += token.text
						}
					}
					if connected && rhs == expected && !gradedConstraintChanged(op, u, body, statementEnd(body, i+1), len(body), constraint.fields) {
						return true
					}
				}
				stored[body[i-1].text] = rhs
			}
		}

		if constraint.kind != "index-bounds" || len(constraint.fields) != 2 {
			continue
		}
		for _, target := range constraint.fields {
			for _, data := range constraint.fields {
				if data == target || constraint.indexExpression != target+"-1" {
					continue
				}
				writtenData := false
				for i, t := range body {
					if t.text != "=" || i == 0 || !gradeOwnedFieldReference(op, u, body, i-1) || !gradedUnconditional(body, i) {
						continue
					}
					if body[i-1].text == data {
						writtenData = true
					}
					if body[i-1].text == target && writtenData && gradedConstraintExpression(op, u, body[i+1:statementEnd(body, i+1)]) == "size("+data+")" && !gradedConstraintChanged(op, u, body, statementEnd(body, i+1), len(body), constraint.fields) {
						return true
					}
				}
			}
		}
	}
	return false
}

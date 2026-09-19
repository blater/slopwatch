package sourceestimate

func gradedConstraintChanged(op *operation, u unit, body []token, start, end int, fields []string) bool {
	if start > end {
		return true
	}
	for _, call := range callsIn(body[start:end]) {
		if !(call.name == "len" && op.language == "go" && gradedBuiltinName(u, op, "len")) {
			return true
		}
	}
	receiver := op.receiverName
	if op.constraintReceiver != "" {
		receiver = op.constraintReceiver
	}
	if op.language == "go" {
		for operator := start; operator < end; operator++ {
			if body[operator].text != "=" && body[operator].text != ":=" {
				continue
			}
			for _, target := range gradedAssignmentTargets(body, operator) {
				if target < start {
					continue
				}
				if receiver != "" && body[target].text == receiver && (target == 0 || body[target-1].text != ".") {
					return true
				}
				for _, name := range fields {
					if body[target].text == name && gradeOwnedFieldReference(op, u, body, target) {
						return true
					}
				}
			}
		}
	}
	for j := start; j < end; j++ {
		if receiver != "" && body[j].text == receiver && j+1 < end && (body[j+1].text == "=" || body[j+1].text == ":=") {
			return true
		}
		for _, name := range fields {
			if body[j].text == name && gradeOwnedFieldReference(op, u, body, j) && gradedStorageWriteAt(body, j) {
				return true
			}
		}
	}
	return false
}

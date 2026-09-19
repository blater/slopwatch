package sourceestimate

func gradedProtocolOutcome(op *operation, u unit, body []token) int {
	for len(body) > 0 && body[0].text == ";" {
		body = body[1:]
	}
	if len(body) == 0 {
		return 0
	}
	if body[0].text == "throw" && (op.language == "java" || op.language == "typescript") {
		return -1
	}
	if body[0].text == "panic" && op.language == "go" && gradedBuiltinPanic(op, u) && len(body) > 1 && body[1].text == "(" {
		return -1
	}
	if op.language == "rust" && body[0].text == "panic" && len(body) > 1 && body[1].text == "!" {
		return -1
	}
	if body[0].text == "return" {
		return 1
	}
	if op.language == "rust" && len(body) == 1 && (body[0].kind == "number" || body[0].text == "true" || body[0].text == "false") {
		return 1
	}
	// A void admitted path may perform observable work rather than return a
	// value (for example appending into owned accumulated state).
	if !gradedBodyHasControl(body) {
		fields := callerDeclaredFields(u, op.owner)
		for i := range body {
			_, declared := fields[body[i].text]
			if declared && gradeOwnedFieldReference(op, u, body, i) && gradedStorageWriteAt(body, i) {
				return 1
			}
		}
	}
	return 0
}

package sourceestimate

func gradedCheapOperation(u unit, op *operation) bool {
	body := trimSemicolonTokens(op.body)
	if op.language == "rust" && len(body) > 0 && op.returnType != "" && op.returnType != "()" && body[len(body)-1].text != ";" && body[0].text != "return" {
		body = append([]token{{text: "return"}}, body...)
	}
	if len(body) == 0 || gradedSurfaceConstructor(op) {
		return true
	}
	// A direct field setter has one assignment and no control flow, call, or
	// computed expression. It still exposes a small ordinary caller choice.
	if len(body) >= 4 && gradedHasAssignment(body) && !gradedBodyHasCall(body) && !gradedBodyHasControl(body) {
		meaningful := 0
		for _, item := range body {
			if isIdentifier(item.text) {
				meaningful++
			}
		}
		if meaningful <= 3 {
			return true
		}
	}
	// Pure accessors and simple value predicates are cheap when they return a
	// source value directly. Arithmetic and other transformations remain a
	// caller-visible operation even when the expression is short.
	return gradedDirectRead(body) || gradedOwnedQuery(u, op, body)
}

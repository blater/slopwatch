package sourceestimate

// gradeUnresolvedResultCall is deliberately separate from protocol transparency:
// validation may precede a returned conversion without discarding its unknown
// result duty. Recognize only a final, top-level call (or its immediately
// returned local). This bounded shape excludes nested closures, discarded calls
// and any intervening alias overwrite without a new control-flow analysis.
func gradeUnresolvedResultCall(op *operation) (call, bool) {
	if gradedSurfaceConstructor(op) {
		return call{}, false
	}
	body := normalizedPrunedBody(op)
	calls := callsIn(body)
	if len(calls) == 0 {
		return call{}, false
	}
	c := calls[len(calls)-1]
	if len(c.actuals) == 0 || len(c.actuals) != len(op.paramNames) {
		return call{}, false
	}
	if !gradedForwardsParameters(op, c) {
		return call{}, false
	}
	start, end := c.position, matching(body, c.position+1, "(", ")")
	if end < 0 {
		return call{}, false
	}
	for start >= 2 && (body[start-1].text == "." || body[start-1].text == "::") && isReceiverPart(body[start-2]) {
		start -= 2
	}
	start, end, valid := gradedNormalizeResultCall(op.language, body, start, end)
	if !valid {
		return call{}, false
	}
	if !gradedUnconditionalResultPrefix(body[:start]) {
		return call{}, false
	}
	rest := body[end+1:]
	if start > 0 && body[start-1].text == "return" {
		for len(rest) > 0 && rest[0].text == ";" {
			rest = rest[1:]
		}
		return c, len(rest) == 0
	}
	if start > 0 && (body[start-1].text == "=" || body[start-1].text == ":=") {
		local := gradedResultLocal(op.language, body, start-1)
		for len(rest) > 0 && rest[0].text == ";" {
			rest = rest[1:]
		}
		explicit := len(rest) > 0 && rest[0].text == "return"
		if explicit {
			rest = rest[1:]
			for len(rest) > 0 && rest[len(rest)-1].text == ";" {
				rest = rest[:len(rest)-1]
			}
		}
		for len(rest) > 2 && rest[0].text == "(" && matching(rest, 0, "(", ")") == len(rest)-1 {
			rest = rest[1 : len(rest)-1]
		}
		return c, local != "" && len(rest) == 1 && rest[0].text == local && (explicit || op.language == "rust")
	}
	// A Rust tail or expression-bodied arrow returns the expression itself;
	// a semicolon makes a Rust call a discarded statement.
	boundary := start == 0 || body[start-1].text == ";" || body[start-1].text == "}"
	return c, boundary && len(rest) == 0 && (op.expressionBody || op.language == "rust" && op.returnType != "" && op.returnType != "()")
}

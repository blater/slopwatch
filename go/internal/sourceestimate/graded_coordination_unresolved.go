package sourceestimate

import "strings"

func gradedUnresolvedCleanup(roots []*operation, units []unit, byKey map[string][]*operation) bool {
	for _, root := range roots {
		for _, op := range gradeOwnedOperations(root, units, byKey) {
			if gradedUnresolvedOperation(op, units, byKey) {
				return true
			}
		}
	}
	return false
}

func gradedUnresolvedOperation(op *operation, units []unit, byKey map[string][]*operation) bool {
	body := normalizedPrunedBody(op)
	for i := range body {
		start, end, protectedStart, protectedEnd := gradedUnresolvedRegion(op, body, i)
		if start < 0 || end <= start || protectedStart < 0 {
			continue
		}
		var regionFlow unconditionalQueries
		for _, c := range callsIn(body[start:end]) {
			if gradedUnresolvedCall(op, units, byKey, body, start, end, protectedStart, protectedEnd, c, &regionFlow) {
				return true
			}
		}
	}
	return false
}

func gradedUnresolvedRegion(op *operation, body []token, i int) (start, end, protectedStart, protectedEnd int) {
	start, end, protectedStart, protectedEnd = -1, -1, -1, -1
	if body[i].text == "finally" && i+1 < len(body) && body[i+1].text == "{" {
		start, end = i+2, matching(body, i+1, "{", "}")
		protectedStart, protectedEnd = gradedTryProtected(op, body, i)
	}
	if body[i].text == "defer" && operationBodyUnconditional(op, body, i) {
		start = i + 1
		open := start
		for open < len(body) && body[open].text != "(" && body[open].text != ";" {
			open++
		}
		if open < len(body) && body[open].text == "(" {
			end = matching(body, open, "(", ")") + 1
		}
		protectedStart, protectedEnd = end, len(body)
	}
	return
}

func gradedUnresolvedCall(op *operation, units []unit, byKey map[string][]*operation, body []token, start, end, protectedStart, protectedEnd int, c call, flow *unconditionalQueries) bool {
	dot := strings.LastIndexByte(c.name, '.')
	if dot < 0 || !flow.unconditional(body[start:end], c.position) {
		return false
	}
	receiver := c.name[:dot]
	if !gradedCoordinationOwnedReceiver(op, units[op.file], receiver) || len(gradedCleanupCandidates(op, units[op.file], units, c, byKey)) != 0 || gradedReceiverReplaced(body, 0, len(body), receiver) {
		return false
	}
	return gradedUnresolvedUses(body, start, end, protectedStart, protectedEnd, receiver)
}

func gradedUnresolvedUses(body []token, start, end, protectedStart, protectedEnd int, receiver string) bool {
	protected, bad := false, false
	for _, use := range callsIn(body) {
		udot := strings.LastIndexByte(use.name, '.')
		if udot < 0 || use.name[:udot] != receiver || use.position >= start && use.position < end || unreachableCall(body, use) {
			continue
		}
		if use.position >= protectedStart && use.position < protectedEnd {
			protected = true
		} else if use.position >= protectedEnd || gradedCallReturns(body, use) {
			bad = true
		}
	}
	return protected && !bad
}

// gradedUnconditional handles isolated queries. Repeated-query callers retain
// a local or operation-owned index instead of rebuilding this one-off index.
func gradedUnconditional(body []token, position int) bool {
	if position <= 0 {
		return true
	}
	answers := buildUnconditionalAnswers(body)
	if position <= len(body) {
		return answers[position] == 2
	}
	// Beyond the documented range the old scan can return early only upon a
	// reachable depth-zero exit. Otherwise it indexes past the body and panics.
	depth := 0
	for i, tok := range body {
		if tok.text == "{" {
			depth++
		}
		if tok.text == "}" {
			depth--
		}
		isExit := tok.text == "return" || tok.text == "throw" || tok.text == "panic" && i+1 < len(body) && body[i+1].text == "("
		if depth == 0 && isExit && answers[i] == 2 {
			return false
		}
	}
	_ = body[position-1] // Preserve out-of-range panic without recursive scanning.
	return false
}

func gradedGuardKeyword(text string) bool {
	switch text {
	case "if", "else", "for", "while", "loop", "switch", "match":
		return true
	default:
		return false
	}
}

func gradedReceiverReplaced(body []token, start, end int, receiver string) bool {
	parts := strings.Split(receiver, ".")
	for i := start; i+1 < end && i+1 < len(body); i++ {
		if body[i].text == parts[len(parts)-1] && (body[i+1].text == "=" || body[i+1].text == ":=") {
			return true
		}
	}
	return false
}

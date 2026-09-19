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
		start, end, protectedStart, protectedEnd := gradedUnresolvedRegion(body, i)
		if start < 0 || end <= start || protectedStart < 0 {
			continue
		}
		for _, c := range callsIn(body[start:end]) {
			if gradedUnresolvedCall(op, units, byKey, body, start, end, protectedStart, protectedEnd, c) {
				return true
			}
		}
	}
	return false
}

func gradedUnresolvedRegion(body []token, i int) (start, end, protectedStart, protectedEnd int) {
	start, end, protectedStart, protectedEnd = -1, -1, -1, -1
	if body[i].text == "finally" && i+1 < len(body) && body[i+1].text == "{" {
		start, end = i+2, matching(body, i+1, "{", "}")
		protectedStart, protectedEnd = gradedTryProtected(body, i)
	}
	if body[i].text == "defer" && gradedUnconditional(body, i) {
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

func gradedUnresolvedCall(op *operation, units []unit, byKey map[string][]*operation, body []token, start, end, protectedStart, protectedEnd int, c call) bool {
	dot := strings.LastIndexByte(c.name, '.')
	if dot < 0 || !gradedUnconditional(body[start:end], c.position) {
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
func gradedUnconditional(body []token, position int) bool {
	return gradedUnconditionalExits(body, position) && gradedUnconditionalGuards(body, position)
}

func gradedUnconditionalExits(body []token, position int) bool {
	depth := 0
	for i := 0; i < position; i++ {
		if body[i].text == "{" {
			depth++
		}
		if body[i].text == "}" {
			depth--
		}
		if gradedUnconditionalExit(body, position, i, depth) {
			return false
		}
	}
	return true
}

func gradedUnconditionalExit(body []token, position, i, depth int) bool {
	isExit := body[i].text == "return" || body[i].text == "throw" || body[i].text == "panic" && i+1 < len(body) && body[i+1].text == "("
	return depth == 0 && isExit && statementEnd(body, i) < position && gradedUnconditional(body, i)
}

func gradedUnconditionalGuards(body []token, position int) bool {
	for i := 0; i < position; i++ {
		if !gradedGuardKeyword(body[i].text) {
			continue
		}
		start, ok := gradedGuardStart(body, i)
		if !ok {
			return false
		}
		if start >= len(body) {
			continue
		}
		end := statementEnd(body, start)
		if body[start].text == "{" {
			end = matching(body, start, "{", "}")
		}
		if position >= start && position <= end {
			return false
		}
	}
	return true
}

func gradedGuardKeyword(text string) bool {
	switch text {
	case "if", "else", "for", "while", "loop", "switch", "match":
		return true
	default:
		return false
	}
}

func gradedGuardStart(body []token, keyword int) (int, bool) {
	start := keyword + 1
	if start < len(body) && body[start].text == "(" {
		end := matching(body, start, "(", ")")
		if end < 0 {
			return 0, false
		}
		return end + 1, true
	}
	if body[keyword].text != "else" {
		for start < len(body) && body[start].text != "{" {
			start++
		}
	}
	return start, true
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

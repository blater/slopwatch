package sourceestimate

import "strings"

func gradeGuaranteedCleanup(ops []*operation, units []unit, byKey map[string][]*operation) bool {
	return gradedCleanupMode(ops, units, byKey, false)
}
func gradedCleanupMode(ops []*operation, units []unit, byKey map[string][]*operation, acquisition bool) bool {
	if len(ops) == 0 || len(units) == 0 {
		return false
	}
	for _, op := range ops {
		if op == nil || op.file < 0 || op.file >= len(units) {
			continue
		}
		u := units[op.file]
		body := normalizedPrunedBody(op)
		if gradedFinallyCleanup(op, u, units, body, byKey, acquisition) || gradedDeferCleanup(op, u, units, body, byKey, acquisition) || gradedRustRAIICleanup(op, u, body, units, byKey, acquisition) {
			return true
		}
	}
	return false
}
func gradedFinallyCleanup(op *operation, u unit, units []unit, body []token, byKey map[string][]*operation, acquisition bool) bool {
	for i, item := range body {
		if item.text != "finally" {
			continue
		}
		start, end := i+1, len(body)
		if start < len(body) && body[start].text == "{" {
			close := matching(body, start, "{", "}")
			if close < 0 {
				continue
			}
			start, end = start+1, close
		}
		protectedStart, protectedEnd := gradedTryProtected(op, body, i)
		if protectedStart < 0 {
			continue
		}
		if gradedCleanupCallInRange(op, u, units, body, start, end, protectedStart, protectedEnd, byKey, acquisition) {
			return true
		}
	}
	return false
}
func gradedDeferCleanup(op *operation, u unit, units []unit, body []token, byKey map[string][]*operation, acquisition bool) bool {
	for i, item := range body {
		if item.text != "defer" || !operationBodyUnconditional(op, body, i) {
			continue
		}
		end := i + 1
		for end < len(body) && body[end].text != "(" && body[end].text != ";" {
			end++
		}
		if end >= len(body) || body[end].text != "(" {
			continue
		}
		end = matching(body, end, "(", ")")
		if end < 0 {
			continue
		}
		// A deferred closure includes its body; a deferred call ends at its
		// argument list. Later statements are never part of either cleanup.
		if i+1 < len(body) && body[i+1].text == "func" && end+1 < len(body) && body[end+1].text == "{" {
			end = matching(body, end+1, "{", "}")
			if end < 0 {
				continue
			}
		}
		if gradedCleanupCallInRange(op, u, units, body, i+1, end+1, end+1, len(body), byKey, acquisition) {
			return true
		}
	}
	return false
}
func gradedCleanupCallInRange(op *operation, u unit, units []unit, body []token, start, end, protectedStart, protectedEnd int, byKey map[string][]*operation, acquisition bool) bool {
	if start < 0 || start >= end || end > len(body) {
		return false
	}
	segment := body[start:end]
	// A conditional cleanup call is not guaranteed on the relevant exit paths.
	for _, tok := range segment {
		if tok.text == "if" || tok.text == "else" || tok.text == "for" || tok.text == "while" || tok.text == "loop" || tok.text == "switch" || tok.text == "?" || tok.text == "&&" || tok.text == "||" {
			return false
		}
	}
	for _, call := range callsIn(segment) {
		dot := strings.LastIndexByte(call.name, '.')
		if dot >= 0 {
			receiver := call.name[:dot]
			if gradedCoordinationOwnedReceiver(op, u, receiver) && gradedCleanupResolvedCall(op, u, units, call, byKey) && gradedConnectedCleanup(op, u, units, body, start, end, protectedStart, protectedEnd, call, byKey, acquisition) {
				return true
			}
		}
	}
	return false
}
func gradedCleanupCandidates(op *operation, u unit, units []unit, c call, byKey map[string][]*operation) []*operation {
	matches := resolveCall(op, c, units, byKey)
	if len(matches) == 0 {
		dot := strings.LastIndexByte(c.name, '.')
		if dot >= 0 {
			fields := callerDeclaredFields(u, op.owner)
			for _, part := range strings.Split(c.name[:dot], ".") {
				if field, ok := fields[part]; ok && field.typeName != "" {
					matches = byKey[scopedOperationKey(op, c.name[dot+1:], field.typeName)]
					break
				}
			}
		}
	}
	// Follow a bounded unconditional forwarding helper, keeping the resolved
	// owner/storage identity rather than granting credit to the helper name.
	for depth := 0; len(matches) == 1 && depth < maxCallDepth; depth++ {
		candidate := matches[0]
		calls := callsIn(candidate.body)
		if len(calls) != 1 || gradedHasAssignment(candidate.body) || !operationBodyUnconditional(candidate, candidate.body, calls[0].position) {
			break
		}
		if len(gradedBooleanWrites(candidate, units, "true")) > 0 || len(gradedBooleanWrites(candidate, units, "false")) > 0 {
			break
		}
		next := resolveCall(candidate, calls[0], units, byKey)
		if len(next) != 1 || next[0].owner != candidate.owner || next[0] == candidate {
			break
		}
		matches = next
	}
	return matches
}
func gradedCleanupResolvedCall(op *operation, u unit, units []unit, c call, byKey map[string][]*operation) bool {
	candidates := gradedCleanupCandidates(op, u, units, c, byKey)
	return len(candidates) == 1 && len(gradedCleanupResetFields(candidates[0], units)) > 0
}
func gradedCleanupResetFields(op *operation, units []unit) map[string]string {
	result := map[string]string{}
	if op == nil || op.file < 0 || op.file >= len(units) {
		return result
	}
	for _, state := range []string{"false", "true"} {
		for _, field := range gradedBooleanWrites(op, units, state) {
			result[field] = state
		}
	}
	return result
}

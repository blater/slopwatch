package sourceestimate

import "strings"

func gradedConnectedCleanup(op *operation, u unit, units []unit, body []token, start, end, protectedStart, protectedEnd int, cleanup call, byKey map[string][]*operation, acquisition bool) bool {
	candidates := gradedCleanupCandidates(op, u, units, cleanup, byKey)
	if len(candidates) != 1 {
		return false
	}
	reset := gradedCleanupResetFields(candidates[0], units)
	receiver := cleanup.name[:strings.LastIndexByte(cleanup.name, '.')]
	acquired, used := map[string]bool{}, map[string]bool{}
	invalidated := map[string]bool{}
	if gradedReceiverReplaced(body, 0, end, receiver) {
		return false
	}
	for _, c := range callsIn(body) {
		if c.position >= start && c.position < end || unreachableCall(body, c) {
			continue
		}
		dot := strings.LastIndexByte(c.name, '.')
		if dot < 0 || gradeReceiver(op, c.name[:dot]) != gradeReceiver(op, receiver) {
			continue
		}
		matches := gradedCleanupCandidates(op, u, units, c, byKey)
		if len(matches) != 1 {
			for field := range reset {
				acquired[field], invalidated[field] = false, true
			}
			continue
		}
		other := matches[0]
		if other.owner != candidates[0].owner || other.pkg != candidates[0].pkg {
			continue
		}
		for i, tok := range other.body {
			if reset[tok.text] == "" || !gradeOwnedFieldReference(other, units[other.file], other.body, i) {
				continue
			}
			// Acquisition changes the same storage to the opposite cleanup state. The latter
			// supports cleanup owned by a complete() stage after caller acquisition.
			if gradedStorageWriteAt(other.body, i) {
				acquired[tok.text], invalidated[tok.text] = false, true
				if gradedHasBooleanWrite(other, units, tok.text, gradedOppositeBoolean(reset[tok.text])) && c.position < start && operationBodyUnconditional(op, body, c.position) && operationBodyUnconditional(other, other.body, i) && !gradedReceiverReplaced(body, c.position, start, receiver) {
					acquired[tok.text], invalidated[tok.text] = true, false
					for _, tok := range body[c.position:maxInt(c.position, protectedStart)] {
						if tok.text == "return" || tok.text == "throw" {
							return false
						}
					}
				}
			} else if !gradedReceiverReplaced(body, minInt(c.position, start), maxInt(c.position, end), receiver) {
				if invalidated[tok.text] || acquisition && !acquired[tok.text] || !gradedProtocolReadAdmits(other, units[other.file], i, reset[tok.text]) {
					return false
				}
				if c.position < protectedStart || c.position >= protectedEnd {
					return false
				}
				used[tok.text] = true
			}
		}
	}
	for field := range reset {
		if acquired[field] && used[field] || !acquisition && (used[field] || acquired[field]) {
			return true
		}
	}
	return false
}

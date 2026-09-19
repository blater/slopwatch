package sourceestimate

import "sort"

// Observable mutation through a caller-supplied buffer leaves allocation,
// retention and reset/combination with the caller. This is alias burden, not
// hidden responsibility or coupling inferred from parameter count.
func gradedCallerOutputBuffers(op *operation, units []unit, index map[string][]*operation, depth int) []string {
	if depth >= maxCallDepth || op.file < 0 || op.file >= len(units) {
		return nil
	}
	if index == nil {
		index = operationIndex(units)
	}
	if op.outputAssessed {
		return op.outputBuffers
	}
	op.outputAssessed = true
	u := units[op.file]
	body := normalizedEagerBody(op)
	for i, t := range body {
		if t.text == "return" && normalizedEagerUnconditional(op, i) {
			body = body[:statementEnd(body, i)]
			break
		}
	}
	kinds, aliases := gradedOutputBufferKinds(op, u)
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	outputs := map[string]bool{}
	declarations := gradedOutputDeclarations(body, op.language)
	scopes := []map[string]string{{}}
	gradedOutputBufferScan(op, units, index, depth, body, kinds, aliases, calls, outputs, declarations, &scopes)
	result := []string{}
	for name := range outputs {
		result = append(result, name)
	}
	sort.Strings(result)
	op.outputBuffers = result
	return result
}

func gradedOutputBufferScan(op *operation, units []unit, index map[string][]*operation, depth int, body []token, kinds map[string]string, aliases map[string]string, calls map[int]call, outputs map[string]bool, declarations map[int]gradedOutputDeclaration, scopes *[]map[string]string) {
	for i, t := range body {
		gradedOutputBufferToken(op, units, index, depth, body, kinds, aliases, calls, outputs, declarations, scopes, i, t)
	}
}

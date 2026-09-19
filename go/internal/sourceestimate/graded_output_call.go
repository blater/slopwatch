package sourceestimate

import "strings"

func gradedOutputBufferCall(op *operation, units []unit, index map[string][]*operation, depth int, aliases map[string]string, outputs map[string]bool, c call) {
	matches := resolveCall(op, c, units, index)
	if len(matches) != 1 || matches[0].exposed || matches[0].owner != op.owner {
		return
	}
	callee := matches[0]
	mutated := gradedCallerOutputBuffers(callee, units, index, depth+1)
	for j, param := range callee.paramNames {
		if j >= len(c.actuals) {
			continue
		}
		found := false
		for _, name := range mutated {
			found = found || name == param
		}
		if !found {
			continue
		}
		actual := strings.TrimPrefix(strings.TrimPrefix(joinTokens(c.actuals[j]), "&mut"), "&")
		if root := aliases[actual]; root != "" {
			outputs[root] = true
		}
	}
}

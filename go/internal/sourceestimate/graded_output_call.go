package sourceestimate

import "strings"

func gradedOutputBufferCall(op *operation, units []unit, index *operationLookup, depth int, aliases map[string]string, outputs map[string]bool, c call) {
	matches := resolveCall(op, c, units, index)
	if matches.count() != 1 || matches.unique().exposed || matches.unique().owner != op.owner {
		return
	}
	callee := matches.unique()
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

package sourceestimate

func goCallGraph(units []unit, byKey *operationLookup) map[string][]*operation {
	return boundedCallGraph(units, byKey, "go")
}

func boundedCallGraph(units []unit, byKey *operationLookup, language string) map[string][]*operation {
	graph := map[string][]*operation{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != language {
			continue
		}
		for _, op := range unit.ops {
			graph[op.id] = appendResolvedCalls(graph[op.id], op, units, byKey)
		}
	}
	return graph
}

func appendResolvedCalls(current []*operation, op *operation, units []unit, byKey *operationLookup) []*operation {
	calls := callsIn(op.body)
	if len(calls) > maxCallsPerRoot {
		calls = calls[:maxCallsPerRoot]
	}
	seen := map[string]bool{}
	selections := map[callSelection]bool{}
	for _, call := range calls {
		if unreachableCall(op.body, call) {
			continue
		}
		selection := resolveCall(op, call, units, byKey)
		if selections[selection] {
			continue
		}
		selections[selection] = true
		selection.each(byKey, func(candidate *operation) {
			if seen[candidate.id] {
				return
			}
			seen[candidate.id] = true
			current = append(current, candidate)
		})
	}
	return current
}

package sourceestimate

type operationMeasure struct {
	evidence     []evidenceItem
	dependencies []string
	limitations  []string
}

type evidenceItem struct {
	category, key   string
	storageComputed bool
	origin          *operation
}

func categoryWeight(category string) float64 {
	switch category {
	case "transform", "state", "resource", "coordination":
		return 2
	default:
		return 1
	}
}

func measureOperation(op *operation, units []unit, byKey map[string][]*operation, seen map[string]bool, depth int, bindings map[string][]token, budget *int) operationMeasure {
	if cutoff, limited := measurementCutoff(op, seen, depth); cutoff {
		return limited
	}
	seen[op.id] = true
	defer delete(seen, op.id)
	body, expanded := measuredBody(op, bindings)
	if !expanded {
		return expansionLimitedMeasure()
	}
	m := localOperationMeasure(op, body, units, byKey)
	m = appendCallMeasure(m, op, body, units, byKey, seen, depth, budget)
	m = appendUnsupportedOutcome(m, op, body, units, byKey)
	setEvidenceOrigins(m.evidence, op)
	return m
}

func measurementCutoff(op *operation, seen map[string]bool, depth int) (bool, operationMeasure) {
	if op == nil {
		if depth > maxCallDepth {
			return true, operationMeasure{limitations: []string{"call_depth_limit"}}
		}
		return true, operationMeasure{}
	}
	if depth > maxCallDepth || seen[op.id] {
		limitation := "call_cycle"
		if depth > maxCallDepth {
			limitation = "call_depth_limit"
		}
		return true, operationMeasure{
			evidence:    []evidenceItem{{category: "unknown_outcome", key: "unknown_outcome|" + op.id + "|cutoff"}},
			limitations: []string{limitation},
		}
	}
	return false, operationMeasure{}
}

func measuredBody(op *operation, bindings map[string][]token) ([]token, bool) {
	body, expanded := substituteBody(op.body, bindings, op.language)
	if op.compactConstructor {
		body, expanded = op.body, true
	}
	if !expanded {
		return nil, false
	}
	body = pruneDeadFalseBranches(body)
	return markStringParams(body, op.stringParams), true
}

func expansionLimitedMeasure() operationMeasure {
	return operationMeasure{
		evidence:    []evidenceItem{{category: "unknown_outcome", key: "unknown_outcome|expansion"}},
		limitations: []string{"expression_expansion_limit"},
	}
}

func localOperationMeasure(op *operation, body []token, units []unit, byKey map[string][]*operation) operationMeasure {
	m := operationMeasure{}
	m.limitations = executionLimitations(body, op.language)
	if combinesOwnedErrors(op, units) {
		m.evidence = append(m.evidence, evidenceItem{category: "coordination", key: "coordination|owned-error-aggregation|" + op.owner})
	}
	for category := range localCategories(body, op, units[op.file]) {
		if category != "state" {
			m.evidence = append(m.evidence, evidenceItem{category: category, key: category + "|" + canonicalBody(body)})
		}
	}
	if gradeUncoveredStorageState(op, units[op.file], body) {
		m.evidence = append(m.evidence, evidenceItem{category: "state", key: "state|" + op.id})
	}
	if gradedCleanupMode([]*operation{op}, units, byKey, true) {
		m.evidence = append(m.evidence, evidenceItem{category: "resource", key: "resource|" + op.id})
	}
	m.evidence = append(m.evidence, evidenceItem{
		category: "storage_snapshot", key: "storage_snapshot|" + op.id, origin: op,
		storageComputed: gradeComputedAssignment(op, units[op.file], body),
	})
	return m
}

func executionLimitations(body []token, language string) []string {
	limitations := make([]string, 0)
	for _, item := range body {
		if unsupportedExecutionToken(item.text) {
			limitations = append(limitations, "unsupported_execution_alternatives:"+item.text)
		}
	}
	for i, item := range body {
		if language == "rust" && item.text == "!" && i > 0 && isIdentifier(body[i-1].text) {
			limitations = append(limitations, "unexpanded_macro")
		}
	}
	return limitations
}

func unsupportedExecutionToken(text string) bool {
	switch text {
	case "if", "?", "&&", "||", "switch", "try", "catch", "finally", "for", "while", "loop", "match", "await", "yield", "goto", "select", "unsafe":
		return true
	default:
		return false
	}
}

func appendCallMeasure(m operationMeasure, op *operation, body []token, units []unit, byKey map[string][]*operation, seen map[string]bool, depth int, budget *int) operationMeasure {
	calls := callsIn(body)
	if len(calls) > maxCallsPerRoot {
		calls = calls[:maxCallsPerRoot]
		m.limitations = append(m.limitations, "call_limit")
	}
	unknownCalls := map[string]bool{}
	for _, call := range calls {
		if unreachableCall(body, call) {
			continue
		}
		if *budget <= 0 {
			m.evidence = append(m.evidence, evidenceItem{category: "unknown_outcome", key: "unknown_outcome|" + op.id + "|budget"})
			m.limitations = append(m.limitations, "call_budget_limit")
			break
		}
		*budget--
		candidates := resolveCall(op, call, units, byKey)
		if len(candidates) == 1 {
			m = appendResolvedMeasure(m, op, call, candidates[0], units, byKey, seen, depth, budget)
			continue
		}
		unknownCalls[call.callSignature()] = true
	}
	if len(unknownCalls) != 0 {
		m.evidence = append(m.evidence, evidenceItem{category: "unknown_call", key: "unknown_call|" + op.id})
		for signature := range unknownCalls {
			m.limitations = append(m.limitations, "unresolved_call_range_0_2:"+signature)
		}
	}
	return m
}

func appendResolvedMeasure(m operationMeasure, op *operation, call call, callee *operation, units []unit, byKey map[string][]*operation, seen map[string]bool, depth int, budget *int) operationMeasure {
	m.dependencies = append(m.dependencies, callee.id)
	childBindings := make(map[string][]token, len(callee.paramNames))
	for i, formal := range callee.paramNames {
		if i < len(call.actuals) {
			childBindings[formal] = call.actuals[i]
		}
	}
	child := measureOperation(callee, units, byKey, seen, depth+1, childBindings, budget)
	m.evidence = append(m.evidence, child.evidence...)
	m.dependencies = append(m.dependencies, child.dependencies...)
	m.limitations = append(m.limitations, child.limitations...)
	return m
}

func appendUnsupportedOutcome(m operationMeasure, op *operation, body []token, units []unit, byKey map[string][]*operation) operationMeasure {
	if !onlyStorageSnapshots(m.evidence) || !nontrivial(body) {
		return m
	}
	if _, _, complete := returnedTransformation(op, units, byKey); complete && !op.compactConstructor {
		return m
	}
	m.evidence = append(m.evidence, evidenceItem{category: "unknown_outcome", key: "unknown_outcome|" + canonicalBody(body)})
	m.limitations = append(m.limitations, "unsupported_outcome_range_0_2")
	return m
}

func setEvidenceOrigins(evidence []evidenceItem, op *operation) {
	for i := range evidence {
		if evidence[i].origin == nil {
			evidence[i].origin = op
		}
	}
}

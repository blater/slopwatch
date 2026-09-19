package sourceestimate

// returnedTransformation normalizes the value which reaches an operation's
// return. It is intentionally narrower than the general source estimate: only
// straight-line bindings and resolvable same-workspace helper returns are
// followed. key is empty when the returned value carries no transformation.
// complete is false whenever the bounded proof cannot account for the value.
func returnedTransformation(op *operation, units []unit, byKey map[string][]*operation) (key string, transformed, complete bool) {
	if op == nil {
		return "", false, false
	}
	state := transformationState{units: units, byKey: byKey, active: map[string]bool{}}
	expression, ok := state.operation(op, nil, 0)
	if !ok {
		return "", false, false
	}
	if !expression.dependsOnInput {
		return "", false, !state.incomplete
	}
	if !expression.transformed {
		return "", false, !state.incomplete
	}
	return expression.key(), expression.transformed, !state.incomplete
}

type transformationState struct {
	units  []unit
	byKey  map[string][]*operation
	active map[string]bool
	calls  int
	tokens int
	// incomplete records unresolved side-effect statements that occur before
	// the returned value. Their uncertainty is reported by the normal call
	// evidence path; it must not erase independently recognized return work.
	incomplete bool
}

func (s *transformationState) operation(op *operation, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	if op == nil || depth > maxCallDepth || s.active[op.id] {
		return nil, false
	}
	s.active[op.id] = true
	defer delete(s.active, op.id)
	if len(op.body) > maxTokensPerFile || s.tokens+len(op.body) > maxTokensPerFile {
		return nil, false
	}
	s.tokens += len(op.body)

	local := make(map[string]*transformationExpr, len(op.paramNames)+len(bindings))
	for index, name := range op.paramNames {
		if value, ok := bindings[name]; ok {
			local[name] = value
		} else {
			local[name] = &transformationExpr{form: "p" + itoa(index), dependsOnInput: true, stringLike: op.stringParams[name]}
		}
	}
	for name, value := range bindings {
		if _, exists := local[name]; !exists {
			local[name] = value
		}
	}

	body := normalizedPrunedBody(op)
	returnIndex := -1
	for index, item := range body {
		if item.text == "return" {
			returnIndex = index
			break
		}
	}
	if returnIndex >= 0 {
		if !straightLinePrefix(body[:returnIndex]) {
			return nil, false
		}
		if !s.bindPrefix(op, local, body[:returnIndex], depth) {
			return nil, false
		}
		returned := firstTransformationExpression(body[returnIndex+1:])
		if len(returned) == 0 {
			return nil, false
		}
		parts := splitArguments(returned)
		if len(parts) != 1 {
			return nil, false
		}
		return s.expression(op, parts[0], local, depth)
	}
	if !straightLinePrefix(body) || len(body) == 0 {
		return nil, false
	}
	return s.expression(op, trimTransformationDelimiters(body), local, depth)
}

// standaloneTransformationCall identifies a call statement whose result is
// discarded. An unresolved call here is side-effect uncertainty; it does not
// determine the value returned by a later, independently parseable expression.
func standaloneTransformationCall(statement []token) (call, bool) {
	statement = trimSemicolonTokens(statement)
	if len(statement) == 0 {
		return call{}, false
	}
	calls := callsIn(statement)
	if len(calls) != 1 {
		return call{}, false
	}
	selected := calls[0]
	if selected.position < 0 || selected.position+1 >= len(statement) {
		return call{}, false
	}
	close := matching(statement, selected.position+1, "(", ")")
	return selected, close == len(statement)-1
}

func (s *transformationState) expression(op *operation, body []token, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	parser := transformationParser{state: s, operation: op, bindings: bindings, tokens: body, depth: depth}
	value, ok := parser.parse(0)
	if !ok || parser.position != len(body) {
		return nil, false
	}
	return value, true
}

func (s *transformationState) helper(op *operation, call call, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	if s.calls >= maxCallsPerRoot || depth >= maxCallDepth {
		return nil, false
	}
	s.calls++
	actuals := make([]*transformationExpr, len(call.actuals))
	for index, actual := range call.actuals {
		value, ok := s.expression(op, actual, bindings, depth)
		if !ok {
			return nil, false
		}
		actuals[index] = value
	}
	candidates := resolveCall(op, call, s.units, s.byKey)
	if len(candidates) != 1 {
		return nil, false
	}
	callee := candidates[0]
	childBindings := make(map[string]*transformationExpr, len(callee.paramNames))
	for index, formal := range callee.paramNames {
		if index >= len(actuals) {
			return nil, false
		}
		childBindings[formal] = actuals[index]
	}
	return s.operation(callee, childBindings, depth+1)
}

func (s *transformationState) bindPrefix(op *operation, local map[string]*transformationExpr, body []token, depth int) bool {
	for _, statement := range splitTransformationStatements(body) {
		if len(statement) == 0 {
			continue
		}
		if !s.bindStatement(op, local, statement, depth) {
			if discarded, standalone := standaloneTransformationCall(statement); standalone {
				if len(resolveCall(op, discarded, s.units, s.byKey)) != 1 {
					s.incomplete = true
				}
				continue
			}
			return false
		}
	}
	return true
}

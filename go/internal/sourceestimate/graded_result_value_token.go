package sourceestimate

import "strings"

func gradedResultValueToken(op *operation, expression []token, i int, current gradedResultFlow, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) (gradedResultFlow, bool, int, bool, bool) {
	tok := expression[i]
	if tok.text == "." && i > 0 && expression[i-1].text == ")" && i+2 < len(expression) && isIdentifier(expression[i+1].text) && expression[i+2].text == "(" {
		end := matching(expression, i+2, "(", ")")
		if end < 0 {
			return gradedResultFlow{}, false, i, true, false
		}
		flow := current
		for _, arg := range splitArguments(expression[i+3 : end]) {
			flow = flow.merge(gradedResultExpression(op, arg, locals, units, byKey, budget, depth+1))
		}
		if flow.unknown {
			flow.composed = true
		}
		return flow, false, end, true, true
	}
	if tok.text == "(" {
		end := matching(expression, i, "(", ")")
		if end < 0 {
			return gradedResultFlow{}, false, i, true, false
		}
		inner := gradedResultExpression(op, expression[i+1:end], locals, units, byKey, budget, depth+1)
		return inner, inner.predicate, end, true, true
	}
	if !isIdentifier(tok.text) || i > 0 && (expression[i-1].text == "." || expression[i-1].text == "::") {
		return gradedResultFlow{}, false, i, false, true
	}
	parts := []string{tok.text}
	receiver := locals[tok.text]
	for i+2 < len(expression) && (expression[i+1].text == "." || expression[i+1].text == "::") && isIdentifier(expression[i+2].text) {
		parts = append(parts, expression[i+2].text)
		i += 2
	}
	if i+1 >= len(expression) || expression[i+1].text != "(" {
		return receiver, receiver.predicate || tok.text == "true" || tok.text == "false", i, true, true
	}
	end := matching(expression, i+1, "(", ")")
	if end < 0 {
		return gradedResultFlow{}, false, i, true, false
	}
	actuals := splitArguments(expression[i+2 : end])
	dependent := receiver
	arguments := make([]gradedResultFlow, len(actuals))
	for j, arg := range actuals {
		arguments[j] = gradedResultExpression(op, arg, locals, units, byKey, budget, depth+1)
		dependent = dependent.merge(arguments[j])
	}
	c := call{name: strings.Join(parts, "."), actuals: actuals, position: i, hasArguments: len(actuals) > 0}
	matches := resolveCall(op, c, units, byKey)
	if len(matches) == 0 {
		dependent.predicate = false
		if dependent.unknown {
			dependent.composed = true
		}
		if dependent.input {
			dependent.unknown = true
		}
	} else {
		dependent = gradedResultFlow{}
		if len(matches) == 1 && matches[0].owner == op.owner && !matches[0].exposed && len(matches[0].paramNames) == len(arguments) {
			bindings := map[string]gradedResultFlow{}
			for j, name := range matches[0].paramNames {
				bindings[name] = arguments[j]
			}
			dependent = gradedOperationResult(matches[0], bindings, units, byKey, budget, depth+1)
		}
	}
	return dependent, dependent.predicate, end, true, true
}

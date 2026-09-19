package sourceestimate

func gradedResultExpression(op *operation, expression []token, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) gradedResultFlow {
	if depth > maxCallDepth || len(expression) > *budget {
		return gradedResultFlow{}
	}
	*budget -= len(expression)
	// Constructed records/maps keep the dependence of their value slots.
	// Pure copied input slots have no unknown/transform duty of their own.
	if aggregate, ok := gradedResultAggregateExpression(op, expression, locals, units, byKey, budget, depth); ok {
		return aggregate
	}
	for index, tok := range expression {
		// The shared lexer keeps JavaScript strict comparisons as a
		// two-character comparison followed by its final equals token.
		if tok.text == "=" && index > 0 && (expression[index-1].text == "==" || expression[index-1].text == "!=") {
			continue
		}
		switch tok.text {
		case "{", "}", "->", "=>", "func", "function", "|", "=", ":=", ";":
			return gradedResultFlow{}
		}
	}
	// A ternary's result kind comes from its arms, not its condition.
	if flow, ok := gradedResultConditionalFlow(op, expression, locals, units, byKey, budget, depth); ok {
		return flow
	}
	// Split lower-precedence OR before AND. A constant short circuit applies
	// only to its own operand, not a later live branch of the whole expression.
	if flow, ok := gradedResultLogicalFlow(op, expression, locals, units, byKey, budget, depth); ok {
		return flow
	}
	var flow gradedResultFlow
	predicate, selection := false, false
	for i := 0; i < len(expression); i++ {
		tok := expression[i]
		if nextFlow, nextPredicate, next, handled, valid := gradedResultValueToken(op, expression, i, flow, locals, units, byKey, budget, depth); handled {
			if !valid {
				return gradedResultFlow{}
			}
			flow, predicate, i = flow.merge(nextFlow), nextPredicate, next
			continue
		}
		switch tok.text {
		case "!", "&&", "||", "==", "!=", "===", "!==", "<", ">", "<=", ">=":
			selection = true
		case ",":
			return gradedResultFlow{}
		}
	}
	flow.predicate = selection || predicate
	if selection && flow.unknown {
		flow.composed = true
	}
	return flow
}

func gradedResultAggregateExpression(op *operation, expression []token, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) (gradedResultFlow, bool) {
	for open, tok := range expression {
		if tok.text != "{" {
			continue
		}
		for _, prefix := range expression[:open] {
			if gradedResultControl(prefix.text) || prefix.text == "func" || prefix.text == "function" || prefix.text == "unsafe" {
				return gradedResultFlow{}, true
			}
		}
		if open > 0 && (expression[open-1].text == "=>" || expression[open-1].text == "->") {
			return gradedResultFlow{}, true
		}
		close := matching(expression, open, "{", "}")
		if close != len(expression)-1 {
			return gradedResultFlow{}, true
		}
		var aggregate gradedResultFlow
		for _, slot := range splitArguments(expression[open+1 : close]) {
			value := gradedResultAggregateSlot(slot)
			aggregate = aggregate.merge(gradedResultExpression(op, value, locals, units, byKey, budget, depth+1))
		}
		aggregate.aggregate = true
		return aggregate, true
	}
	return gradedResultFlow{}, false
}

func gradedResultAggregateSlot(slot []token) []token {
	value := slot
	nesting := 0
	for j, t := range slot {
		if t.text == "(" || t.text == "[" || t.text == "{" {
			nesting++
		}
		if t.text == ")" || t.text == "]" || t.text == "}" {
			nesting--
		}
		if t.text == ":" && nesting == 0 {
			return slot[j+1:]
		}
	}
	return value
}

func gradedResultConditionalFlow(op *operation, expression []token, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) (gradedResultFlow, bool) {
	question, colon := gradedResultConditional(expression)
	if question < 0 {
		return gradedResultFlow{}, false
	}
	conditionTokens := gradedResultUngroup(expression[:question])
	leftTokens, rightTokens := expression[question+1:colon], expression[colon+1:]
	if len(conditionTokens) == 1 && (conditionTokens[0].text == "true" || conditionTokens[0].text == "false") {
		if conditionTokens[0].text == "true" {
			return gradedResultExpression(op, leftTokens, locals, units, byKey, budget, depth+1), true
		}
		return gradedResultExpression(op, rightTokens, locals, units, byKey, budget, depth+1), true
	}
	if joinTokens(leftTokens) == joinTokens(rightTokens) {
		return gradedResultExpression(op, leftTokens, locals, units, byKey, budget, depth+1), true
	}
	condition := gradedResultExpression(op, expression[:question], locals, units, byKey, budget, depth+1)
	left := gradedResultExpression(op, leftTokens, locals, units, byKey, budget, depth+1)
	right := gradedResultExpression(op, rightTokens, locals, units, byKey, budget, depth+1)
	flow := condition.merge(left).merge(right)
	flow.composed = flow.composed || flow.unknown
	flow.predicate = left.predicate && right.predicate
	return flow, true
}

func gradedResultLogicalFlow(op *operation, expression []token, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) (gradedResultFlow, bool) {
	for _, operator := range []string{"||", "&&"} {
		nesting := 0
		for i, tok := range expression {
			switch tok.text {
			case "(", "[":
				nesting++
			case ")", "]":
				nesting--
			}
			if nesting != 0 || tok.text != operator {
				continue
			}
			leftTokens := gradedResultUngroup(expression[:i])
			if len(leftTokens) == 1 && (leftTokens[0].text == "false" || leftTokens[0].text == "true") {
				if operator == "&&" && leftTokens[0].text == "false" || operator == "||" && leftTokens[0].text == "true" {
					return gradedResultFlow{predicate: true}, true
				}
				return gradedResultExpression(op, expression[i+1:], locals, units, byKey, budget, depth+1), true
			}
			left := gradedResultExpression(op, expression[:i], locals, units, byKey, budget, depth+1)
			right := gradedResultExpression(op, expression[i+1:], locals, units, byKey, budget, depth+1)
			flow := left.merge(right)
			flow.composed = flow.composed || flow.unknown
			flow.predicate = op.language != "typescript" || left.predicate && right.predicate
			return flow, true
		}
	}
	return gradedResultFlow{}, false
}

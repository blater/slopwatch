package sourceestimate

import "strings"

// These flags describe possible dependence, never recognized implementation
// duties. In particular an unknown lookup on an owned field is only a seed:
// returning it unchanged does not claim its delegate's implementation.
type gradedResultFlow struct {
	input, unknown, composed bool
	// Predicate results can support possible validation, but do not establish
	// a second result-transformation category for that same admission test.
	predicate bool
	aggregate bool
}

func (f gradedResultFlow) merge(other gradedResultFlow) gradedResultFlow {
	return gradedResultFlow{input: f.input || other.input, unknown: f.unknown || other.unknown, composed: f.composed || other.composed, aggregate: f.aggregate || other.aggregate}
}

// gradeConnectedResult follows straight-line local values to a final returned
// expression. Branch bodies and closures are not traversed. The expression
// budget and depth bound apply to the whole operation, including nested calls.
func gradeConnectedResult(op *operation, units []unit, byKey map[string][]*operation) (connected, predicateOnly, representationOnly bool) {
	locals := map[string]gradedResultFlow{}
	for _, name := range op.paramNames {
		locals[name] = gradedResultFlow{input: true}
	}
	budget := maxTokensPerFile
	flow := gradedOperationResult(op, locals, units, byKey, &budget, 0)
	return flow.unknown && flow.composed, flow.predicate, flow.aggregate
}

func gradedOperationResult(op *operation, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) gradedResultFlow {
	if gradedSurfaceConstructor(op) || depth > maxCallDepth || len(op.body) > *budget {
		return gradedResultFlow{}
	}
	*budget -= len(op.body)
	body := pruneDeadFalseBranches(op.body)
	statements := gradedResultStatements(body)
	for index, statement := range statements {
		if len(statement) == 0 {
			continue
		}
		if statement[0].text == "return" {
			if index != len(statements)-1 {
				return gradedResultFlow{}
			}
			flow := gradedResultExpression(op, statement[1:], locals, units, byKey, budget, depth+1)
			return flow
		}
		if gradedResultControl(statement[0].text) {
			// A useful early guard can precede the result. Writes inside an
			// unsupported control path invalidate local evidence instead of
			// carrying stale values past possible overwrites.
			gradedInvalidateWrites(statement, locals)
			continue
		}
		assign := -1
		for i, tok := range statement {
			if tok.text == "=" || tok.text == ":=" {
				assign = i
				break
			}
		}
		if assign >= 0 {
			name := gradedResultLocal(op.language, statement, assign)
			if name == "" && assign == 1 {
				if _, exists := locals[statement[0].text]; exists {
					name = statement[0].text
				}
			}
			if name != "" {
				locals[name] = gradedResultExpression(op, statement[assign+1:], locals, units, byKey, budget, depth+1)
			} else {
				gradedInvalidateWrites(statement, locals)
			}
			continue
		}
		gradedInvalidateWrites(statement, locals)
		if index == len(statements)-1 && (op.expressionBody || op.language == "rust" && op.returnType != "" && op.returnType != "()") && len(body) > 0 && body[len(body)-1].text != ";" {
			flow := gradedResultExpression(op, statement, locals, units, byKey, budget, depth+1)
			return flow
		}
	}
	return gradedResultFlow{}
}

func gradedResultControl(text string) bool {
	return isControl(text) || text == "else" || text == "try" || text == "finally" || text == "do"
}

// Unsupported writes must discard prior dependence, including every target of
// a Go parallel assignment. General alias resolution remains outside this pass.
func gradedInvalidateWrites(statement []token, locals map[string]gradedResultFlow) {
	for i, tok := range statement {
		switch tok.text {
		case "=", ":=", "+=", "-=", "*=", "/=", "%=", "++", "--":
			for j := i - 1; j >= 0; j -= 2 {
				if !isIdentifier(statement[j].text) {
					break
				}
				delete(locals, statement[j].text)
				if j == 0 || statement[j-1].text != "," {
					break
				}
			}
		}
	}
}

// Split a braced guard separately so it cannot swallow the following local
// declaration in Go/Rust. Other braces remain inside their statement; lambdas
// are rejected by expression analysis and never interpreted as executed calls.
func gradedResultStatements(body []token) [][]token {
	var result [][]token
	start, depth := 0, 0
	for i, tok := range body {
		if depth == 0 && i > start && (tok.text == "return" || tok.text == "const" || tok.text == "let" || tok.text == "var" || isIdentifier(tok.text) && body[i-1].text != "," && i+1 < len(body) && body[i+1].text == ":=") && !gradedResultControl(body[start].text) {
			result = append(result, body[start:i])
			start = i
		}
		switch tok.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		boundary := depth == 0 && (tok.text == ";" || tok.text == "}" && start < len(body) && gradedResultControl(body[start].text))
		if boundary {
			end := i + 1
			if tok.text == ";" {
				end = i
			}
			if end > start {
				result = append(result, body[start:end])
			}
			start = i + 1
		}
	}
	if start < len(body) {
		result = append(result, body[start:])
	}
	return result
}

func gradedResultExpression(op *operation, expression []token, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) gradedResultFlow {
	if depth > maxCallDepth || len(expression) > *budget {
		return gradedResultFlow{}
	}
	*budget -= len(expression)
	// Constructed records/maps keep the dependence of their value slots.
	// Pure copied input slots have no unknown/transform duty of their own.
	for open, tok := range expression {
		if tok.text != "{" {
			continue
		}
		for _, prefix := range expression[:open] {
			if gradedResultControl(prefix.text) || prefix.text == "func" || prefix.text == "function" || prefix.text == "unsafe" {
				return gradedResultFlow{}
			}
		}
		if open > 0 && (expression[open-1].text == "=>" || expression[open-1].text == "->") {
			return gradedResultFlow{}
		}
		close := matching(expression, open, "{", "}")
		if close != len(expression)-1 {
			return gradedResultFlow{}
		}
		var aggregate gradedResultFlow
		for _, slot := range splitArguments(expression[open+1 : close]) {
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
					value = slot[j+1:]
					break
				}
			}
			aggregate = aggregate.merge(gradedResultExpression(op, value, locals, units, byKey, budget, depth+1))
		}
		aggregate.aggregate = true
		// Aggregation alone is copying; only an inner connected transform composes.
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
	if question, colon := gradedResultConditional(expression); question >= 0 {
		conditionTokens := gradedResultUngroup(expression[:question])
		leftTokens, rightTokens := expression[question+1:colon], expression[colon+1:]
		if len(conditionTokens) == 1 && (conditionTokens[0].text == "true" || conditionTokens[0].text == "false") {
			if conditionTokens[0].text == "true" {
				return gradedResultExpression(op, leftTokens, locals, units, byKey, budget, depth+1)
			}
			return gradedResultExpression(op, rightTokens, locals, units, byKey, budget, depth+1)
		}
		if joinTokens(leftTokens) == joinTokens(rightTokens) {
			return gradedResultExpression(op, leftTokens, locals, units, byKey, budget, depth+1)
		}
		condition := gradedResultExpression(op, expression[:question], locals, units, byKey, budget, depth+1)
		left := gradedResultExpression(op, expression[question+1:colon], locals, units, byKey, budget, depth+1)
		right := gradedResultExpression(op, expression[colon+1:], locals, units, byKey, budget, depth+1)
		flow := condition.merge(left).merge(right)
		flow.composed = flow.composed || flow.unknown
		flow.predicate = left.predicate && right.predicate
		return flow
	}
	// Split lower-precedence OR before AND. A constant short circuit applies
	// only to its own operand, not a later live branch of the whole expression.
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
					return gradedResultFlow{predicate: true}
				}
				return gradedResultExpression(op, expression[i+1:], locals, units, byKey, budget, depth+1)
			}
			left := gradedResultExpression(op, expression[:i], locals, units, byKey, budget, depth+1)
			right := gradedResultExpression(op, expression[i+1:], locals, units, byKey, budget, depth+1)
			flow := left.merge(right)
			flow.composed = flow.composed || flow.unknown
			// JavaScript returns operands; other supported languages return bool.
			flow.predicate = op.language != "typescript" || left.predicate && right.predicate
			return flow
		}
	}
	var flow gradedResultFlow
	predicate, selection := false, false
	for i := 0; i < len(expression); i++ {
		tok := expression[i]
		if tok.text == "." && i > 0 && expression[i-1].text == ")" && i+2 < len(expression) && isIdentifier(expression[i+1].text) && expression[i+2].text == "(" {
			end := matching(expression, i+2, "(", ")")
			if end < 0 {
				return gradedResultFlow{}
			}
			actuals := splitArguments(expression[i+3 : end])
			for _, arg := range actuals {
				flow = flow.merge(gradedResultExpression(op, arg, locals, units, byKey, budget, depth+1))
			}
			if flow.unknown {
				flow.composed = true
			}
			predicate = false
			i = end
			continue
		}
		if tok.text == "(" {
			end := matching(expression, i, "(", ")")
			if end < 0 {
				return gradedResultFlow{}
			}
			inner := gradedResultExpression(op, expression[i+1:end], locals, units, byKey, budget, depth+1)
			flow = flow.merge(inner)
			predicate = inner.predicate
			i = end
			continue
		}
		if isIdentifier(tok.text) && (i == 0 || expression[i-1].text != "." && expression[i-1].text != "::") {
			start := i
			parts := []string{tok.text}
			receiver := locals[tok.text]
			for i+2 < len(expression) && (expression[i+1].text == "." || expression[i+1].text == "::") && isIdentifier(expression[i+2].text) {
				parts = append(parts, expression[i+2].text)
				i += 2
			}
			if i+1 < len(expression) && expression[i+1].text == "(" {
				end := matching(expression, i+1, "(", ")")
				if end < 0 {
					return gradedResultFlow{}
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
					// Consuming a predicate does not make the unknown call's
					// output another predicate (for example, its string form).
					dependent.predicate = false
					if dependent.unknown {
						dependent.composed = true
					}
					if dependent.input {
						dependent.unknown = true
					}
				} else {
					dependent = gradedResultFlow{}
					// Only a private implementation helper's actual return flow
					// propagates values. Identity and constant helpers add no duty.
					if len(matches) == 1 && matches[0].owner == op.owner && !matches[0].exposed && len(matches[0].paramNames) == len(arguments) {
						bindings := map[string]gradedResultFlow{}
						for j, name := range matches[0].paramNames {
							bindings[name] = arguments[j]
						}
						dependent = gradedOperationResult(matches[0], bindings, units, byKey, budget, depth+1)
					}
				}
				predicate = dependent.predicate
				flow = flow.merge(dependent)
				i = end
			} else {
				flow = flow.merge(locals[expression[start].text])
				predicate = locals[expression[start].text].predicate || tok.text == "true" || tok.text == "false"
			}
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

func gradedResultUngroup(expression []token) []token {
	for len(expression) > 2 && expression[0].text == "(" && matching(expression, 0, "(", ")") == len(expression)-1 {
		expression = expression[1 : len(expression)-1]
	}
	return expression
}

// Find only a top-level ternary, respecting nested arms and grouped/call
// expressions. Comparisons inside a condition do not describe its value arms.
func gradedResultConditional(expression []token) (question, colon int) {
	depth, nested := 0, 0
	question, colon = -1, -1
	for i, tok := range expression {
		switch tok.text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		}
		if depth != 0 {
			continue
		}
		if tok.text == "?" {
			if question < 0 {
				question = i
			} else {
				nested++
			}
		}
		if tok.text == ":" && question >= 0 {
			if nested == 0 {
				return question, i
			}
			nested--
		}
	}
	return -1, -1
}

package sourceestimate

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

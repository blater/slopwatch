package sourceestimate

func gradedOperationResult(op *operation, locals map[string]gradedResultFlow, units []unit, byKey map[string][]*operation, budget *int, depth int) gradedResultFlow {
	if gradedSurfaceConstructor(op) || depth > maxCallDepth || len(op.body) > *budget {
		return gradedResultFlow{}
	}
	*budget -= len(op.body)
	body := normalizedPrunedBody(op)
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

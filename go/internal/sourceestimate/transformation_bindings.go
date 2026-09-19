package sourceestimate

func splitTransformationStatements(body []token) [][]token {
	result := make([][]token, 0, 4)
	start, depth := 0, 0
	for index, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				result = append(result, body[start:index])
				start = index + 1
			}
		}
	}
	if start < len(body) {
		result = append(result, body[start:])
	}
	return result
}

func (s *transformationState) bindStatement(op *operation, bindings map[string]*transformationExpr, statement []token, depth int) bool {
	if len(statement) < 3 {
		return false
	}
	nameIndex, operatorIndex := 0, 1
	if statement[0].text == "let" || statement[0].text == "var" || statement[0].text == "const" {
		nameIndex, operatorIndex = 1, 2
	}
	if nameIndex >= len(statement) || !isIdentifier(statement[nameIndex].text) || operatorIndex >= len(statement) || statement[operatorIndex].text != "=" && statement[operatorIndex].text != ":=" {
		return false
	}
	rhs := statement[operatorIndex+1:]
	parser := transformationParser{state: s, operation: op, tokens: rhs, bindings: bindings, depth: depth}
	value, ok := parser.parse(0)
	if !ok || parser.position != len(rhs) {
		return false
	}
	bindings[statement[nameIndex].text] = value
	return true
}

func trimTransformationDelimiters(body []token) []token {
	start, end := 0, len(body)
	for start < end && body[start].text == ";" {
		start++
	}
	for end > start && (body[end-1].text == ";" || body[end-1].text == "}") {
		end--
	}
	return body[start:end]
}

func firstTransformationExpression(body []token) []token {
	body = trimTransformationDelimiters(body)
	depth := 0
	for index, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return body[:index]
			}
			depth--
		case ";":
			if depth == 0 {
				return body[:index]
			}
		}
	}
	return body
}

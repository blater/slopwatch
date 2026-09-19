package sourceestimate

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

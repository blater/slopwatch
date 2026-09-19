package sourceestimate

func gradedOutputAssignment(op *operation, body []token, aliases map[string]string, scopes *[]map[string]string, i int, t token) bool {
	if !isIdentifier(t.text) || i > 0 && body[i-1].text == "." || i+1 >= len(body) || body[i+1].text != "=" && body[i+1].text != ":=" {
		return false
	}
	declaration := i > 0 && (body[i-1].text == "const" || body[i-1].text == "let" || body[i-1].text == "mut") || body[i+1].text == ":="
	if declaration && len(*scopes) > 1 {
		scope := (*scopes)[len(*scopes)-1]
		if _, seen := scope[t.text]; !seen {
			scope[t.text] = aliases[t.text]
		}
	}
	root := ""
	if i+2 < len(body) && statementEnd(body, i+2) == i+3 && operationBodyUnconditional(op, body, i) {
		root = aliases[body[i+2].text]
	}
	if root == "" {
		delete(aliases, t.text)
	} else {
		aliases[t.text] = root
	}
	return true
}

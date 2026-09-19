package sourceestimate

func gradedOutputBufferToken(op *operation, units []unit, index map[string][]*operation, depth int, body []token, kinds map[string]string, aliases map[string]string, calls map[int]call, outputs map[string]bool, declarations map[int]gradedOutputDeclaration, scopes *[]map[string]string, i int, t token) {
	if t.text == "{" {
		*scopes = append(*scopes, map[string]string{})
	}
	if t.text == "}" && len(*scopes) > 1 {
		for name, old := range (*scopes)[len(*scopes)-1] {
			if old == "" {
				delete(aliases, name)
			} else {
				aliases[name] = old
			}
		}
		*scopes = (*scopes)[:len(*scopes)-1]
	}
	if declaration, ok := declarations[i]; ok {
		if declaration.block && len(*scopes) > 1 {
			scope := (*scopes)[len(*scopes)-1]
			if _, seen := scope[t.text]; !seen {
				scope[t.text] = aliases[t.text]
			}
		}
		root := ""
		if len(declaration.initializer) == 1 {
			root = aliases[declaration.initializer[0].text]
		}
		if root == "" {
			delete(aliases, t.text)
		} else {
			aliases[t.text] = root
		}
	} else if gradedOutputAssignment(op, body, aliases, scopes, i, t) {
		// Assignment aliases were updated by the helper.
	}
	root := aliases[t.text]
	if root != "" && (i == 0 || body[i-1].text != ".") {
		gradedOutputMutation(body, kinds, outputs, root, i)
	}
	c, ok := calls[i]
	if !ok {
		return
	}
	gradedOutputBufferCall(op, units, index, depth, aliases, outputs, c)
}

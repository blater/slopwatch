package sourceestimate

// supportingTypeScriptOwners proves data-only classes from bounded source,
// including local classes. Every member must be admitted; an empty constructor
// cannot hide executable parameter defaults, initializers or static blocks.
func supportingTypeScriptOwners(units []unit) map[attributionOwnerKey]string {
	result := map[attributionOwnerKey]string{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" || u.limited || !u.lexicallyValid {
			continue
		}
		collectPassiveTypeScriptOwners(u, result)
	}
	return result
}

func typescriptOwnerKey(u unit, owner string) attributionOwnerKey {
	return attributionOwnerKey{file: u.index, owner: owner}
}

func passiveTypeScriptMembers(body []token) bool {
	fields, methods, ok := passiveTypeScriptInventory(body)
	if !ok || len(fields) == 0 {
		return false
	}
	for _, method := range methods {
		if !passiveTypeScriptMethod(method, fields) {
			return false
		}
	}
	return true
}

func passiveTypeScriptInventory(body []token) (map[string]bool, [][]token, bool) {
	fields := map[string]bool{}
	methods := [][]token{}
	for i := 0; i < len(body); {
		if body[i].text == ";" {
			i++
			continue
		}
		start := i
		for i < len(body) && (body[i].text == "public" || body[i].text == "private" || body[i].text == "protected" || body[i].text == "readonly") {
			i++
		}
		if i >= len(body) || !isIdentifier(body[i].text) || body[i].text == "static" {
			return nil, nil, false
		}
		name := body[i].text
		i++
		if name == "get" && i < len(body) && isIdentifier(body[i].text) {
			i++
		}
		if i < len(body) && body[i].text == "(" {
			end := passiveTypeScriptMethodEnd(body, i)
			if end < 0 {
				return nil, nil, false
			}
			methods = append(methods, body[start:end+1])
			i = end + 1
			continue
		}
		fields[name] = true
		for i < len(body) && body[i].text != ";" {
			if body[i].text == "=" || body[i].text == "(" || body[i].text == "{" || body[i].text == "}" {
				return nil, nil, false
			}
			i++
		}
		if i < len(body) {
			i++
		}
	}
	return fields, methods, true
}

func passiveTypeScriptMethodEnd(body []token, open int) int {
	paramsEnd := matching(body, open, "(", ")")
	if paramsEnd < 0 {
		return -1
	}
	for _, param := range body[open+1 : paramsEnd] {
		if param.text == "=" || param.text == "..." {
			return -1
		}
	}
	start := paramsEnd + 1
	for start < len(body) && body[start].text != "{" {
		if body[start].text == ";" || body[start].text == "=" {
			return -1
		}
		start++
	}
	if start >= len(body) {
		return -1
	}
	return matching(body, start, "{", "}")
}

func passiveTypeScriptMethod(method []token, fields map[string]bool) bool {
	open := -1
	for i, t := range method {
		if t.text == "(" {
			open = i
			break
		}
	}
	if open < 1 {
		return false
	}
	close := matching(method, open, "(", ")")
	start := close + 1
	for start < len(method) && method[start].text != "{" {
		start++
	}
	if start >= len(method) {
		return false
	}
	content := trimSemicolonTokens(method[start+1 : len(method)-1])
	if method[open-1].text == "constructor" {
		params := map[string]bool{}
		for _, name := range parameterNames(method[open+1:close], "typescript") {
			params[name] = true
		}
		for len(content) > 0 {
			if len(content) < 5 || content[0].text != "this" || content[1].text != "." || !fields[content[2].text] || content[3].text != "=" || !params[content[4].text] {
				return false
			}
			content = content[5:]
			if len(content) > 0 {
				if content[0].text != ";" {
					return false
				}
				content = content[1:]
			}
		}
	} else {
		if close != open+1 || len(content) != 4 || content[0].text != "return" || content[1].text != "this" || content[2].text != "." || !fields[content[3].text] {
			return false
		}
	}
	return true
}

func collectPassiveTypeScriptOwners(u unit, result map[attributionOwnerKey]string) {
	for i := 0; i+2 < len(u.tokens); i++ {
		if u.tokens[i].text != "class" || !isIdentifier(u.tokens[i+1].text) {
			continue
		}
		start := i + 2
		if u.tokens[start].text == "<" {
			end := matching(u.tokens, start, "<", ">")
			if end < 0 {
				continue
			}
			start = end + 1
		}
		if start >= len(u.tokens) || u.tokens[start].text != "{" {
			continue
		}
		end := matching(u.tokens, start, "{", "}")
		if end < 0 {
			continue
		}
		if passiveTypeScriptMembers(u.tokens[start+1 : end]) {
			// TypeScript classes are file-local in the bounded resolver. Scope
			// the role key by language and path so same-named classes cannot
			// borrow a passive proof from an unrelated source file.
			result[typescriptOwnerKey(u, u.tokens[i+1].text)] = "passive-value-object-v1"
		}
		i = end
	}
}

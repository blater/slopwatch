package sourceestimate

func typeScriptReturnedReference(body []token, index int) bool {
	for i := index - 1; i >= 0; i-- {
		switch body[i].text {
		case ";", "{", "}":
			return false
		case "return":
			return true
		}
	}
	return false
}

// A helper's read contributes only when its result escapes this operation.
// Discarded read calls do not connect otherwise unrelated public routes.

func typeScriptCallResultObserved(body []token, c call) bool {
	start := c.position
	for start >= 2 && body[start-1].text == "." {
		start -= 2
	}
	if start > 0 && body[start-1].text == "return" {
		return true
	}
	if start < 2 || body[start-1].text != "=" || !isIdentifier(body[start-2].text) {
		return false
	}
	name := body[start-2].text
	declarations := gradedOutputDeclarations(body, "typescript")
	binding := typeScriptLocalBinding(body, declarations, name, start-2)
	if c.position+1 >= len(body) || body[c.position+1].text != "(" {
		return false
	}
	close := matching(body, c.position+1, "(", ")")
	if close < 0 {
		return false
	}
	for i := close + 1; i < len(body); i++ {
		if body[i].text == name && (i == 0 || body[i-1].text != ".") {
			if typeScriptLocalBinding(body, declarations, name, i) != binding {
				continue
			}
			if gradedStorageWriteAt(body, i) {
				return false
			}
			if typeScriptReturnedReference(body, i) {
				return true
			}
		}
	}
	return false
}

// Module-private storage retains the same duty as equivalent class-private
// storage. Grouping must not erase a counter update simply because it is lexical.

func typeScriptModuleScalarEffects(u unit, roots []*operation, units []unit, index *operationLookup) (state, computed bool) {
	if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" {
		return false, false
	}
	storage := typeScriptModuleStorage(u.tokens)
	for _, root := range roots {
		if root.owner != "" {
			continue
		}
		visits := 0
		for _, effect := range typeScriptModuleUses(root, storage, units, index, map[string]bool{}, nil, &visits, 0) {
			state = state || effect.scalarState
			computed = computed || effect.scalarComputed
		}
	}
	return
}

func typeScriptScalarEffect(body []token, index int) (state, computed bool) {
	if index > 0 && (body[index-1].text == "++" || body[index-1].text == "--") {
		return true, false
	}
	if index+1 >= len(body) {
		return false, false
	}
	operator := body[index+1].text
	if operator == "++" || operator == "--" {
		return true, false
	}
	if index+2 >= len(body) {
		return false, false
	}
	rhs := body[index+2 : statementEnd(body, index+2)]
	if (operator == "+=" || operator == "-=") && len(rhs) == 1 && rhs[0].text == "1" {
		return true, false
	}
	if operator == "=" {
		if len(rhs) == 3 && rhs[0].text == body[index].text && (rhs[1].text == "+" || rhs[1].text == "-") && rhs[2].text == "1" {
			return true, false
		}
		return false, hasTransform(rhs)
	}
	if operator == "+=" || operator == "-=" || operator == "*=" || operator == "/=" {
		return false, compoundUpdateChanges(rhs, operator)
	}
	return false, false
}

func typeScriptLocalBinding(body []token, declarations map[int]gradedOutputDeclaration, name string, before int) int {
	binding := -1
	for position, declaration := range declarations {
		if position <= before && position > binding && body[position].text == name && (!declaration.block || gradedScopeContains(body, position, before)) {
			binding = position
		}
	}
	return binding
}

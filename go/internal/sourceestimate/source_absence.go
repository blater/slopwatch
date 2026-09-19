package sourceestimate

func noAbstractionProven(u unit) bool {
	if u.limited || !u.lexicallyValid {
		return false
	}
	if len(u.tokens) == 0 {
		return true
	}
	switch normalizeLanguage(u.file.Language, u.file.Path) {
	case "go":
		return packageOnlyGo(u.tokens)
	case "java":
		return packageOnlyJava(u.tokens)
	default:
		return false
	}
}

func packageOnlyGo(tokens []token) bool {
	index := 0
	if !consumeToken(tokens, &index, "package") || !consumeIdentifier(tokens, &index) {
		return false
	}
	if index < len(tokens) && tokens[index].text != ";" {
		return false
	}
	for index < len(tokens) {
		if tokens[index].text == ";" {
			index++
			continue
		}
		if tokens[index].text != "import" {
			return false
		}
		index++
		if index < len(tokens) && tokens[index].text == "(" {
			index++
			for index < len(tokens) && tokens[index].text != ")" {
				if tokens[index].text == ";" {
					index++
					continue
				}
				if !consumeGoImport(tokens, &index) {
					return false
				}
			}
			if !consumeToken(tokens, &index, ")") || index < len(tokens) && tokens[index].text != ";" {
				return false
			}
			continue
		}
		if !consumeGoImport(tokens, &index) || index < len(tokens) && tokens[index].text != ";" {
			return false
		}
	}
	return true
}

func consumeGoImport(tokens []token, index *int) bool {
	if *index >= len(tokens) {
		return false
	}
	if tokens[*index].kind == "identifier" || tokens[*index].text == "." {
		(*index)++
		if *index >= len(tokens) {
			return false
		}
	}
	if tokens[*index].kind != "string" {
		return false
	}
	(*index)++
	return true
}

func packageOnlyJava(tokens []token) bool {
	index := 0
	if !consumeToken(tokens, &index, "package") || !consumeQualifiedName(tokens, &index) || !consumeToken(tokens, &index, ";") {
		return false
	}
	for index < len(tokens) {
		if !consumeToken(tokens, &index, "import") {
			return false
		}
		if index < len(tokens) && tokens[index].text == "static" {
			index++
		}
		if !consumeQualifiedName(tokens, &index) {
			return false
		}
		if index < len(tokens) && tokens[index].text == "." {
			index++
			if !consumeToken(tokens, &index, "*") {
				return false
			}
		}
		if !consumeToken(tokens, &index, ";") {
			return false
		}
	}
	return true
}

func consumeQualifiedName(tokens []token, index *int) bool {
	if !consumeIdentifier(tokens, index) {
		return false
	}
	for *index+1 < len(tokens) && tokens[*index].text == "." && tokens[*index+1].text != "*" {
		(*index)++
		if !consumeIdentifier(tokens, index) {
			return false
		}
	}
	return true
}

func consumeToken(tokens []token, index *int, text string) bool {
	if *index >= len(tokens) || tokens[*index].text != text {
		return false
	}
	(*index)++
	return true
}

func consumeIdentifier(tokens []token, index *int) bool {
	if *index >= len(tokens) || tokens[*index].kind != "identifier" {
		return false
	}
	(*index)++
	return true
}

package sourceestimate

func requiredParameterNames(parameters []token, language string) []string {
	if len(parameters) == 0 {
		return nil
	}
	result := make([]string, 0, parameterCount(parameters))
	for _, part := range splitParameterDeclarations(parameters) {
		if len(part) == 0 || surfaceOptionalParameter(part, language) {
			continue
		}
		names := parameterNames(part, language)
		if len(names) == 0 {
			continue
		}
		result = append(result, names[0])
	}
	return result
}

func surfaceOptionalParameter(part []token, language string) bool {
	for _, item := range part {
		switch item.text {
		case "...", "?", "=", "..":
			return true
		}
	}
	// TypeScript parameter properties and destructured/default bindings are
	// intentionally outside the bounded name model.
	if language == "typescript" {
		for _, item := range part {
			if item.text == "{" || item.text == "[" {
				return true
			}
		}
	}
	return false
}

// Signature commas inside generic type arguments do not separate parameters.
func splitParameterDeclarations(tokens []token) [][]token {
	result := [][]token{}
	start, nested, generic := 0, 0, 0
	for i, item := range tokens {
		switch item.text {
		case "(", "[", "{":
			nested++
		case ")", "]", "}":
			if nested > 0 {
				nested--
			}
		case "<":
			generic++
		case ">":
			if generic > 0 {
				generic--
			}
		case ">>":
			if generic >= 2 {
				generic -= 2
			} else {
				generic = 0
			}
		case ",":
			if nested == 0 && generic == 0 {
				result = append(result, tokens[start:i])
				start = i + 1
			}
		}
	}
	if start < len(tokens) {
		result = append(result, tokens[start:])
	}
	return result
}

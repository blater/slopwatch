package sourceestimate

import (
	"path/filepath"
	"strings"
)

func operationKey(op *operation) string { return scopedOperationKey(op, op.name, op.owner) }

func workspaceOperationKey(op *operation, name, owner string) string {
	if op.language == "rust" {
		return "workspace:rust#" + owner + "#" + name
	}
	return "workspace:" + op.language + "#" + op.pkg + "#" + owner + "#" + name
}

func scopedOperationKey(op *operation, name, owner string) string {
	scope := op.pkg
	if op.language == "typescript" || op.language == "rust" {
		scope += ":file:" + itoa(op.file)
	}
	return op.language + ":" + name + "@" + scope + "#" + owner
}

func lexicalOwners(tokens []token, language string) []string {
	owners := make([]string, len(tokens))
	if language != "java" && language != "typescript" {
		return owners
	}
	stack := make([]string, 0, 4)
	pending := ""
	for i, item := range tokens {
		if len(stack) != 0 {
			owners[i] = stack[len(stack)-1]
		}
		if classHeader(tokens, i, item) {
			pending = tokens[i+1].text
			continue
		}
		if item.text == "{" {
			stack = appendOwner(stack, pending)
			pending = ""
		} else if item.text == "}" && len(stack) != 0 {
			stack = stack[:len(stack)-1]
		}
	}
	return owners
}

func classHeader(tokens []token, index int, item token) bool {
	return (item.text == "class" || item.text == "interface" || item.text == "record") && index+1 < len(tokens) && isIdentifier(tokens[index+1].text)
}

func appendOwner(stack []string, pending string) []string {
	if pending != "" {
		return append(stack, pending)
	}
	if len(stack) != 0 {
		return append(stack, stack[len(stack)-1])
	}
	return append(stack, "")
}

func unresolvedSurface(tokens []token) bool {
	publicSurface, unknownType := false, false
	for _, item := range tokens {
		switch item.text {
		case "export", "pub", "public":
			publicSurface = true
		case "Missing", "Unknown", "Unresolved":
			unknownType = true
		}
	}
	return publicSurface && unknownType
}

func hasModifier(tokens []token, index int, modifier string) bool {
	start := index - 8
	if start < 0 {
		start = 0
	}
	for i := index - 1; i >= start; i-- {
		if tokens[i].text == ";" || tokens[i].text == "{" || tokens[i].text == "}" {
			break
		}
		if tokens[i].text == modifier {
			return true
		}
	}
	return false
}

func hasExportedType(tokens []token, index int) bool {
	depth := 0
	for i := index - 1; i >= 0 && index-i < 1024; i-- {
		switch tokens[i].text {
		case "}":
			depth++
		case "{":
			if depth > 0 {
				depth--
				continue
			}
			for j := i - 1; j >= 0 && i-j < 64; j-- {
				if tokens[j].text == "export" && j+1 < len(tokens) && tokens[j+1].text == "class" {
					return true
				}
				if tokens[j].text == "class" || tokens[j].text == "interface" {
					break
				}
			}
			return false
		}
	}
	return false
}

func declarationBody(tokens []token, close int) int {
	for i := close + 1; i < minInt(close+16, len(tokens)); i++ {
		if tokens[i].text == ";" {
			return -1
		}
		if tokens[i].text == "{" || tokens[i].text == "=>" {
			return i
		}
	}
	return -1
}

func arrowToken(tokens []token, close int) int {
	for i := close + 1; i < len(tokens) && i <= close+32; i++ {
		if tokens[i].text == "=>" {
			return i
		}
		if tokens[i].text == ";" || tokens[i].text == "{" {
			return -1
		}
	}
	return -1
}

func expressionBodyEnd(tokens []token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case ";", "}":
			if depth == 0 {
				return i
			}
		}
	}
	return len(tokens)
}

func parameterCount(tokens []token) int { return len(splitParameterDeclarations(tokens)) }

func parameterNames(tokens []token, language string) []string {
	parts := splitParameterDeclarations(tokens)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if language == "rust" && parameterIsReceiver(part) {
			continue
		}
		identifiers := parameterIdentifiers(part)
		if len(identifiers) == 0 {
			continue
		}
		result = append(result, parameterNameForLanguage(identifiers, language))
	}
	return result
}

func parameterIsReceiver(part []token) bool {
	for _, item := range part {
		if item.text == "self" {
			return true
		}
	}
	return false
}

func parameterIdentifiers(part []token) []string {
	result := make([]string, 0, 2)
	for _, item := range part {
		if isIdentifier(item.text) && item.text != "const" && item.text != "mut" {
			result = append(result, item.text)
		}
	}
	return result
}

func parameterNameForLanguage(identifiers []string, language string) string {
	if language == "java" {
		return identifiers[len(identifiers)-1]
	}
	return identifiers[0]
}

func stringParameterNames(tokens []token, language string) map[string]bool {
	result := map[string]bool{}
	for _, part := range splitArguments(tokens) {
		if !containsStringType(part) {
			continue
		}
		for _, name := range parameterNames(part, language) {
			result[name] = true
		}
	}
	return result
}

func containsStringType(part []token) bool {
	for _, item := range part {
		if item.text == "String" || item.text == "string" {
			return true
		}
	}
	return false
}

func markStringParams(body []token, names map[string]bool) []token {
	if len(names) == 0 {
		return body
	}
	result := append([]token(nil), body...)
	for i := range result {
		if names[result[i].text] {
			result[i].kind = "stringvar"
		}
	}
	return result
}

func hasReturnedValue(body []token) bool {
	for i, item := range body {
		if item.text == "return" && i+1 < len(body) && body[i+1].text != ";" {
			return true
		}
	}
	return false
}

func nontrivial(body []token) bool {
	if hasTransform(body) || hasValidation(body) || hasState(body) {
		return true
	}
	for _, item := range body {
		switch item.text {
		case "for", "while", "throw", "new", "await", "yield", "match", "switch", "try", "catch", "synchronized", "defer", "=", "+=", "-=", "++", "--", "<<", ">>":
			return true
		}
	}
	for index, item := range body {
		if item.text == "if" && !definitelyFalseIf(body, index) && !identicalReturnedOutcomes(body) {
			return true
		}
	}
	return false
}

func identicalReturnedOutcomes(body []token) bool {
	values := returnedValues(body)
	if len(values) < 2 {
		return false
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return false
		}
	}
	return true
}

func definitelyFalseIf(body []token, start int) bool {
	conditionStart, conditionEnd := start+1, start+1
	if conditionStart < len(body) && body[conditionStart].text == "(" {
		close := matching(body, conditionStart, "(", ")")
		if close < 0 {
			return false
		}
		conditionStart++
		conditionEnd = close
	} else {
		for conditionEnd < len(body) && body[conditionEnd].text != "{" && body[conditionEnd].text != ";" {
			conditionEnd++
		}
	}
	condition := body[conditionStart:conditionEnd]
	if len(condition) == 1 && condition[0].text == "false" {
		return true
	}
	if len(condition) < 3 || condition[0].text != "false" || condition[1].text != "&&" {
		return false
	}
	for _, item := range condition[2:] {
		if item.text == "||" {
			return false
		}
	}
	return true
}

func hasReachableCall(body []token) bool {
	for _, candidate := range callsIn(body) {
		if !unreachableCall(body, candidate) {
			return true
		}
	}
	return false
}

func packageName(language string, tokens []token) string {
	lang := normalizeLanguage(language, "")
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != "package" && tokens[i].text != "namespace" {
			continue
		}
		if lang == "go" {
			if tokens[i+1].kind == "identifier" {
				return tokens[i+1].text
			}
			continue
		}
		parts := make([]string, 0, 3)
		for j := i + 1; j < len(tokens) && tokens[j].text != ";" && tokens[j].text != "{"; j++ {
			if tokens[j].kind == "identifier" {
				parts = append(parts, tokens[j].text)
			}
		}
		if len(parts) != 0 {
			return strings.Join(parts, ".")
		}
	}
	if lang == "rust" {
		for i := 0; i+1 < len(tokens); i++ {
			if tokens[i].text == "mod" && tokens[i+1].kind == "identifier" {
				return tokens[i+1].text
			}
		}
	}
	return filepath.Dir(".")
}

func normalizeLanguage(language, path string) string {
	l := strings.ToLower(language)
	path = strings.ToLower(path)
	if strings.Contains(l, "typescript") || strings.Contains(l, "javascript") || strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
		return "typescript"
	}
	if strings.Contains(l, "rust") || strings.HasSuffix(path, ".rs") {
		return "rust"
	}
	if strings.Contains(l, "go") || strings.HasSuffix(path, ".go") {
		return "go"
	}
	return "java"
}

func sameLanguage(a, b string) bool { return a == b }

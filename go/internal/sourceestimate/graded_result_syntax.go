package sourceestimate

// Qualified names and array suffixes cover the simple cast/type-assertion
// wrappers accepted here. Complex type expressions remain conservative.
func gradedResultTypeEnd(body []token, start int) int {
	if start >= len(body) || !isIdentifier(body[start].text) || isControl(body[start].text) {
		return start
	}
	end := start + 1
	for end+1 < len(body) && body[end].text == "." && isIdentifier(body[end+1].text) {
		end += 2
	}
	for end+1 < len(body) && body[end].text == "[" && body[end+1].text == "]" {
		end += 2
	}
	return end
}

// Delimiter depth alone cannot distinguish `if (x) return f(x)` from an
// unconditional return. Retain pending unbraced control until its statement
// terminates. This also rejects conditional assignments/declarations.
func gradedUnconditionalResultPrefix(body []token) bool {
	depth, conditional := 0, false
	for _, tok := range body {
		switch tok.text {
		case "if", "else", "for", "while", "switch", "match", "do":
			if depth == 0 {
				conditional = true
			}
		case "{", "(", "[":
			depth++
		case "}", ")", "]":
			depth--
			if tok.text == "}" && depth == 0 {
				conditional = false
			}
		case ";":
			if depth == 0 {
				conditional = false
			}
		}
	}
	return depth == 0 && !conditional
}

// The assignment must introduce a local binding. Reading its adjacent syntax
// avoids confusing an earlier semicolonless declaration with this call, or an
// unqualified Java field assignment with a returned local.
func gradedResultLocal(language string, body []token, assign int) string {
	name := assign - 1
	if name < 0 {
		return ""
	}
	if language == "typescript" || language == "rust" {
		for i := name - 1; i >= 1 && body[i].text != "=" && body[i].text != ";"; i-- {
			if body[i].text == ":" && gradedResultTypeEnd(body, i+1) == assign {
				name = i - 1
				break
			}
		}
	}
	if !isIdentifier(body[name].text) {
		return ""
	}
	declaration := name - 1
	switch language {
	case "typescript":
		if declaration < 0 || (body[declaration].text != "const" && body[declaration].text != "let" && body[declaration].text != "var") {
			return ""
		}
	case "rust":
		if declaration >= 0 && body[declaration].text == "mut" {
			declaration--
		}
		if declaration < 0 || body[declaration].text != "let" {
			return ""
		}
	case "go":
		if body[assign].text != ":=" || declaration >= 0 && (body[declaration].text == "." || body[declaration].text == ",") {
			return ""
		}
	case "java":
		for declaration >= 0 && body[declaration].text != ";" && body[declaration].text != "}" && body[declaration].text != "{" {
			declaration--
		}
		declaration++
		if declaration < name && body[declaration].text == "final" {
			declaration++
		}
		if declaration >= name || gradedResultTypeEnd(body, declaration) != name {
			return ""
		}
	default:
		return ""
	}
	return body[name].text
}

func gradedNormalizeResultCall(language string, body []token, start, end int) (int, int, bool) {
	// Peel only value-preserving result wrappers, never another call or operator.
	for {
		switch {
		case start > 0 && body[start-1].text == "(" && matching(body, start-1, "(", ")") == end+1:
			start--
			end++
		case language == "typescript" && start > 0 && body[start-1].text == "await":
			start--
		case language == "typescript" && end+1 < len(body) && body[end+1].text == "as" && gradedResultTypeEnd(body, end+2) > end+2:
			end = gradedResultTypeEnd(body, end+2) - 1
		case language == "java" && start > 2 && body[start-1].text == ")":
			cast := start - 2
			for cast > 0 && body[cast].text != "(" {
				cast--
			}
			if body[cast].text != "(" || gradedResultTypeEnd(body, cast+1) != start-1 {
				return 0, 0, false
			}
			start = cast
		default:
			return start, end, true
		}
	}
}

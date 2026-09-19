package sourceestimate

import "unicode"

func lex(source []byte) ([]token, bool, bool) {
	out := make([]token, 0, len(source)/3)
	limited, valid := false, true
	for i := 0; i < len(source); {
		if len(out) >= maxTokensPerFile {
			locateTokens(out, source)
			return out, true, valid
		}
		if next := skipWhitespace(source, i); next != i {
			i = next
			continue
		}
		if next, handled, closed := skipComment(source, i); handled {
			if !closed {
				valid = false
				break
			}
			i = next
			continue
		}
		if next, ok := scanLifetime(source, i); ok {
			out = append(out, token{text: "'", kind: "lifetime", offset: i})
			i = next
			continue
		}
		if next, tok, ok, quotedValid := scanQuoted(source, i); ok {
			out = append(out, tok)
			valid = valid && quotedValid
			i = next
			continue
		}
		if next, tok, ok := scanIdentifier(source, i); ok {
			out = append(out, tok)
			if len(out) > maxTokensPerFile {
				limited = true
			}
			i = next
			continue
		}
		if next, tok, ok := scanNumber(source, i); ok {
			out = append(out, tok)
			i = next
			continue
		}
		if next, text, ok := scanTwoCharOperator(source, i); ok {
			out = append(out, token{text: text, offset: i})
			i = next
			continue
		}
		out = append(out, token{text: string(source[i]), offset: i})
		i++
	}
	locateTokens(out, source)
	return out, limited, valid
}

func skipWhitespace(source []byte, index int) int {
	for index < len(source) && unicode.IsSpace(rune(source[index])) {
		index++
	}
	return index
}

func skipComment(source []byte, index int) (int, bool, bool) {
	if index+1 >= len(source) || source[index] != '/' || (source[index+1] != '/' && source[index+1] != '*') {
		return index, false, true
	}
	if source[index+1] == '/' {
		index += 2
		for index < len(source) && source[index] != '\n' {
			index++
		}
		return index, true, true
	}
	index += 2
	for index+1 < len(source) && !(source[index] == '*' && source[index+1] == '/') {
		index++
	}
	if index+1 >= len(source) {
		return index, true, false
	}
	return index + 2, true, true
}

func scanLifetime(source []byte, index int) (int, bool) {
	if source[index] != '\'' || index+1 >= len(source) || !(unicode.IsLetter(rune(source[index+1])) || source[index+1] == '_') {
		return index, false
	}
	j := index + 2
	for j < len(source) && (unicode.IsLetter(rune(source[j])) || unicode.IsDigit(rune(source[j])) || source[j] == '_') {
		j++
	}
	return index + 1, j >= len(source) || source[j] != '\''
}

func scanQuoted(source []byte, index int) (int, token, bool, bool) {
	if source[index] != '"' && source[index] != '\'' && source[index] != '`' {
		return index, token{}, false, true
	}
	start, quote := index, source[index]
	index++
	closed := false
	for index < len(source) {
		if source[index] == '\\' {
			index += 2
			continue
		}
		if source[index] == quote {
			index++
			closed = true
			break
		}
		index++
	}
	kind := "__string__"
	if index-start == 2 {
		kind = "__empty_string__"
	}
	literal := string(source[start:minInt(index, len(source))])
	return index, token{text: kind, kind: "string", literal: literal, offset: start}, true, closed
}

func scanIdentifier(source []byte, index int) (int, token, bool) {
	if index >= len(source) || !(unicode.IsLetter(rune(source[index])) || source[index] == '_' || source[index] == '$') {
		return index, token{}, false
	}
	start := index
	index++
	for index < len(source) && (unicode.IsLetter(rune(source[index])) || unicode.IsDigit(rune(source[index])) || source[index] == '_' || source[index] == '$') {
		index++
	}
	return index, token{text: string(source[start:index]), kind: "identifier", offset: start}, true
}

func scanNumber(source []byte, index int) (int, token, bool) {
	if index >= len(source) || !unicode.IsDigit(rune(source[index])) {
		return index, token{}, false
	}
	start := index
	index++
	for index < len(source) && (unicode.IsDigit(rune(source[index])) || source[index] == '.' && index+1 < len(source) && unicode.IsDigit(rune(source[index+1]))) {
		index++
	}
	return index, token{text: string(source[start:index]), kind: "number", offset: start}, true
}

func scanTwoCharOperator(source []byte, index int) (int, string, bool) {
	if index+1 >= len(source) {
		return index, "", false
	}
	text := string(source[index : index+2])
	switch text {
	case "==", "!=", "<=", ">=", "=>", "++", "--", "+=", "-=", "*=", "/=", "%=", ":=", "&&", "||", "::", "->", "<<", ">>":
		return index + 2, text, true
	default:
		return index, "", false
	}
}

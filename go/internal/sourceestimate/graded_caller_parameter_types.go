package sourceestimate

// Parameter bindings are scoped to the operation. A file-wide identifier/type
// guess is not sufficient evidence that a caller manipulates this boundary.
func gradedParameterTypes(tokens []token, language string) map[string]string {
	result := map[string]string{}
	if language == "rust" || language == "typescript" {
		start := 0
		depth := 0
		for end := 0; end <= len(tokens); end++ {
			if end < len(tokens) {
				switch tokens[end].text {
				case "<", "(", "[":
					depth++
				case ">", ")", "]":
					depth--
				}
			}
			if end < len(tokens) && (tokens[end].text != "," || depth > 0) {
				continue
			}
			part := tokens[start:end]
			for i, t := range part {
				if t.text == ":" && i > 0 {
					result[part[i-1].text] = joinTokens(part[i+1:])
					break
				}
			}
			start = end + 1
		}
		return result
	}
	if language == "go" {
		names := []string{}
		for i := 0; i < len(tokens); {
			if tokens[i].text == "," {
				i++
				continue
			}
			if !isIdentifier(tokens[i].text) {
				break
			}
			names = append(names, tokens[i].text)
			i++
			if i >= len(tokens) {
				break
			}
			if tokens[i].text == "," {
				continue
			}
			start := i
			depth := 0
			for i < len(tokens) {
				if tokens[i].text == "[" {
					depth++
				}
				if tokens[i].text == "]" {
					depth--
				}
				if tokens[i].text == "," && depth == 0 {
					break
				}
				i++
			}
			typ := joinTokens(tokens[start:i])
			for _, name := range names {
				result[name] = typ
			}
			names = nil
		}
		return result
	}
	if language != "java" {
		return result
	}
	start := 0
	for end := 0; end <= len(tokens); end++ {
		if end < len(tokens) && tokens[end].text != "," {
			continue
		}
		part := tokens[start:end]
		name, typ := "", ""
		for _, t := range part {
			if isIdentifier(t.text) && t.text != "final" {
				if typ == "" {
					typ = t.text
				}
				name = t.text
			}
		}
		if name != "" && name != typ {
			result[name] = typ
		}
		start = end + 1
	}
	return result
}

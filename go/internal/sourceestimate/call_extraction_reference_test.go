package sourceestimate

import "strings"

// Frozen pre-index extraction behavior for exact differential comparison.
func referenceCallsIn(body []token) []call {
	result := make([]call, 0, 8)
	for i := 0; i+1 < len(body); i++ {
		if !referenceCallStart(body, i) {
			continue
		}
		close := matching(body, i+1, "(", ")")
		if close < 0 {
			continue
		}
		name := referenceQualifiedCallName(body, i)
		actuals := referenceSplitArguments(body[i+2 : close])
		result = append(result, call{
			signature: name + "/" + strings.TrimSpace(joinTokens(body[i+2:close])),
			name:      name, hasArguments: close > i+2, actuals: actuals, position: i,
		})
	}
	return result
}

func referenceCallStart(body []token, index int) bool {
	return isIdentifier(body[index].text) && body[index+1].text == "(" && !isControl(body[index].text)
}

func referenceQualifiedCallName(body []token, index int) string {
	parts := []string{body[index].text}
	for cursor := index - 1; cursor >= 1 && (body[cursor].text == "." || body[cursor].text == "::"); cursor -= 2 {
		if !referenceIsReceiverPart(body[cursor-1]) {
			break
		}
		parts = append([]string{body[cursor-1].text}, parts...)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, ".")
}

func referenceIsReceiverPart(item token) bool {
	return isIdentifier(item.text) || item.kind == "number"
}

func referenceSplitArguments(body []token) [][]token {
	if len(body) == 0 {
		return nil
	}
	result := make([][]token, 0, 2)
	start, depth := 0, 0
	for i, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				result = append(result, body[start:i])
				start = i + 1
			}
		}
	}
	return append(result, body[start:])
}

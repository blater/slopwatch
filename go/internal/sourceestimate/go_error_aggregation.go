package sourceestimate

import "strings"

// Recognize the standard errors.Join contract combining independent owned
// delegate outcomes. This is source evidence of aggregation, not a heuristic
// based on methods being named Close/Join. Only the imported stdlib function
// and a direct returned combination of distinct receiver fields are admitted.
func combinesOwnedErrors(op *operation, units []unit) bool {
	if op.language != "go" || op.owner == "" || op.receiverName == "" || op.file < 0 || op.file >= len(units) {
		return false
	}
	body := trimSemicolonTokens(op.body)
	if len(body) < 7 || body[0].text != "return" || body[1].text != "errors" || body[2].text != "." || body[3].text != "Join" || body[4].text != "(" || matching(body, 4, "(", ")") != len(body)-1 {
		return false
	}
	imported := false
	tokens := units[op.file].tokens
	for i, item := range tokens {
		if item.text != "import" || i+1 >= len(tokens) {
			continue
		}
		start, end := i+1, i+1
		if tokens[start].text == "(" {
			end = matching(tokens, start, "(", ")")
			start++
		} else {
			for end < len(tokens) && tokens[end].kind != "string" {
				end++
			}
			end++
		}
		if end < start || end > len(tokens) {
			continue
		}
		for j := start; j < end; j++ {
			if tokens[j].kind != "string" || strings.Trim(tokens[j].literal, "\"`") != "errors" {
				continue
			}
			if j > start && (tokens[j-1].text == "." || tokens[j-1].kind == "identifier" && tokens[j-1].text != "errors") {
				continue
			}
			imported = true
		}
	}
	if !imported || op.receiverName == "errors" {
		return false
	}
	for _, name := range op.paramNames {
		if name == "errors" {
			return false
		}
	}

	args := splitArguments(body[5 : len(body)-1])
	if len(args) < 2 {
		return false
	}
	fields := map[string]bool{}
	for _, arg := range args {
		if len(arg) != 7 || arg[0].text != op.receiverName || arg[1].text != "." || !isIdentifier(arg[2].text) || arg[3].text != "." || !isIdentifier(arg[4].text) || arg[5].text != "(" || arg[6].text != ")" {
			return false
		}
		fields[arg[2].text] = true
	}
	return len(fields) > 1
}

package sourceestimate

import "strings"

type call struct {
	signature, name string
	hasArguments    bool
	actuals         [][]token
	position        int
}

func callsIn(body []token) []call {
	result := make([]call, 0, 8)
	for i := 0; i+1 < len(body); i++ {
		if !callStart(body, i) {
			continue
		}
		close := matching(body, i+1, "(", ")")
		if close < 0 {
			continue
		}
		name := qualifiedCallName(body, i)
		actuals := splitArguments(body[i+2 : close])
		result = append(result, call{
			signature: name + "/" + strings.TrimSpace(joinTokens(body[i+2:close])),
			name:      name, hasArguments: close > i+2, actuals: actuals, position: i,
		})
	}
	return result
}

func callStart(body []token, index int) bool {
	return isIdentifier(body[index].text) && body[index+1].text == "(" && !isControl(body[index].text)
}

func qualifiedCallName(body []token, index int) string {
	parts := []string{body[index].text}
	for cursor := index - 1; cursor >= 1 && (body[cursor].text == "." || body[cursor].text == "::"); cursor -= 2 {
		if !isReceiverPart(body[cursor-1]) {
			break
		}
		parts = append([]string{body[cursor-1].text}, parts...)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, ".")
}

func isReceiverPart(item token) bool {
	return isIdentifier(item.text) || item.kind == "number"
}

func splitArguments(body []token) [][]token {
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

func substituteBody(body []token, bindings map[string][]token, language string) ([]token, bool) {
	if len(bindings) == 0 {
		return body, true
	}
	result := make([]token, 0, len(body))
	for _, item := range body {
		replacement, ok := bindings[item.text]
		if !ok || !isIdentifier(item.text) {
			result = append(result, item)
			continue
		}
		if len(result)+len(replacement) > maxTokensPerFile {
			return nil, false
		}
		result = append(result, replacement...)
	}
	return result, true
}

func unreachableCall(body []token, candidate call) bool {
	i := candidate.position
	for i >= 2 && body[i-1].text == "." {
		i -= 2
	}
	return i >= 2 && ((body[i-2].text == "false" && body[i-1].text == "&&") || (body[i-2].text == "true" && body[i-1].text == "||"))
}

func resolveCall(caller *operation, c call, units []unit, byKey map[string][]*operation) []*operation {
	if matches, handled := resolveTypeScriptImportedCall(caller, c, units, byKey); handled {
		return matches
	}
	name, owner, hasReceiverType := resolveCallOwner(caller, c.name)
	matches := findCallMatches(caller, name, owner, byKey)
	if len(matches) == 0 && owner != "" && crossFileCallAllowed(caller, owner, hasReceiverType) {
		matches = findWorkspaceCallMatches(caller, name, owner, byKey)
	}
	if len(matches) == 0 && owner != "" && caller.owner != "" {
		matches = findCallMatches(caller, name, "", byKey)
	}
	return matches
}

func resolveCallOwner(caller *operation, callName string) (string, string, bool) {
	name, owner := callName, ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		owner, name = name[:dot], name[dot+1:]
	}
	receiverType, hasReceiverType := caller.fieldTypes[owner]
	if !hasReceiverType {
		if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			receiverType, hasReceiverType = caller.fieldTypes[owner[dot+1:]]
		}
	}
	if hasReceiverType {
		owner = receiverType
	}
	if owner == "" || owner == "this" || owner == "self" {
		owner = caller.owner
	}
	return name, owner, hasReceiverType
}

func findCallMatches(caller *operation, name, owner string, byKey map[string][]*operation) []*operation {
	matches := make([]*operation, 0)
	for _, candidate := range byKey[scopedOperationKey(caller, name, owner)] {
		if !sameCallScope(caller, candidate, owner) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func sameCallScope(caller, candidate *operation, owner string) bool {
	if (caller.language == "typescript" || caller.language == "rust") && candidate.file != caller.file {
		return false
	}
	if owner != "" && candidate.owner != "" && candidate.owner != owner {
		return false
	}
	return candidate.file == caller.file || sameLanguage(candidate.language, caller.language)
}

func crossFileCallAllowed(caller *operation, owner string, hasReceiverType bool) bool {
	if caller.language != "typescript" && caller.language != "rust" {
		return false
	}
	explicitOwner := hasReceiverType
	if caller.language == "rust" {
		explicitOwner = explicitOwner || owner != caller.owner
	}
	if caller.language == "typescript" {
		_, explicitImport := caller.imports[owner]
		explicitOwner = explicitOwner || explicitImport
	}
	return explicitOwner
}

func findWorkspaceCallMatches(caller *operation, name, owner string, byKey map[string][]*operation) []*operation {
	matches := make([]*operation, 0)
	for _, candidate := range byKey[workspaceOperationKey(caller, name, owner)] {
		if candidate.file != caller.file && candidate.owner == owner && candidate.exposed {
			matches = append(matches, candidate)
		}
	}
	return matches
}

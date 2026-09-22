package sourceestimate

import "strings"

type call struct {
	signature, name string
	argumentTokens  []token
	hasArguments    bool
	actuals         [][]token
	position        int
}

func callsIn(body []token) []call {
	result := make([]call, 0, 8)
	// Most expression bodies contain no call candidate. Avoid token-sized
	// indexes until a candidate actually needs delimiter resolution.
	first := 0
	for first+1 < len(body) && !callStart(body, first) {
		first++
	}
	if first+1 >= len(body) {
		return result
	}
	// Bound rescanning on tiny expressions to avoid allocating indexes for
	// their few calls. The fixed cutoff preserves linear asymptotic work.
	const directCallTokenLimit = 64
	var index callDelimiterIndex
	if len(body) > directCallTokenLimit {
		index = indexCallDelimiters(body)
	}
	for i := first; i+1 < len(body); i++ {
		if !callStart(body, i) {
			continue
		}
		close := -1
		if index.parentheses != nil {
			close = index.parentheses[i+1]
		} else {
			close = matching(body, i+1, "(", ")")
		}
		if close < 0 {
			continue
		}
		name := qualifiedCallName(body, i)
		var actuals [][]token
		if index.parentheses != nil {
			actuals = index.arguments(body, i+2, close)
		} else {
			actuals = splitArguments(body[i+2 : close])
		}
		result = append(result, call{
			argumentTokens: body[i+2 : close],
			name:           name, hasArguments: close > i+2, actuals: actuals, position: i,
		})
	}
	return result
}

// callSignature materializes exact deduplication evidence only when consumed.
func (c call) callSignature() string {
	if c.signature != "" {
		return c.signature
	}
	return c.name + "/" + strings.TrimSpace(joinTokens(c.argumentTokens))
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
		parts = append(parts, body[cursor-1].text)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
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

func resolveCall(caller *operation, c call, units []unit, byKey *operationLookup) callSelection {
	if byKey == nil {
		return callSelection{}
	}
	byKey.observe("query")
	if matches, handled := resolveTypeScriptImportedCall(caller, c, units, byKey); handled {
		return matches
	}
	name, owner, hasReceiverType := resolveCallOwner(caller, c.name)
	matches := findCallMatches(caller, name, owner, byKey)
	if matches.count() == 0 && owner != "" && crossFileCallAllowed(caller, owner, hasReceiverType) {
		matches = findWorkspaceCallMatches(caller, name, owner, byKey)
	}
	if matches.count() == 0 && owner != "" && caller.owner != "" {
		matches = findCallMatches(caller, name, "", byKey)
	}
	return matches
}

func resolveCallOwner(caller *operation, callName string) (string, string, bool) {
	name, owner := callName, ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		owner, name = name[:dot], name[dot+1:]
	}
	receiverType, hasReceiverType := operationFieldType(caller, owner)
	if !hasReceiverType {
		if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			receiverType, hasReceiverType = operationFieldType(caller, owner[dot+1:])
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

func findCallMatches(caller *operation, name, owner string, byKey *operationLookup) callSelection {
	if byKey == nil {
		return callSelection{}
	}
	return callSelection{bucket: byKey.scoped[scopedOperationKey(caller, name, owner)]}
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

func findWorkspaceCallMatches(caller *operation, name, owner string, byKey *operationLookup) callSelection {
	if byKey == nil {
		return callSelection{}
	}
	return callSelection{bucket: byKey.workspace[workspaceOperationKey(caller, name, owner)], exclude: true, file: caller.file}
}

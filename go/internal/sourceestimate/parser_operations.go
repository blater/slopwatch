package sourceestimate

import "unicode"

type operationCandidate struct {
	nameIndex, paramOpen int
	receiverStart        int
	receiverEnd          int
	exported             bool
	singleArrowParam     bool
}

func findOperations(file File, index int, tokens []token, pkg string) []*operation {
	lang := normalizeLanguage(file.Language, file.Path)
	result := make([]*operation, 0, 8)
	fieldTypes := sourceFieldTypes(tokens)
	owners := lexicalOwners(tokens, lang)
	recordHeaders, compactConstructors := javaCompactConstructors(file, index, tokens, pkg)
	exportedClasses := exportedClassNames(tokens)
	for i := 0; i < len(tokens) && len(result) < maxOperationsPerFile; i++ {
		if recordHeaders[i] {
			continue
		}
		candidate, ok := recognizeOperation(tokens, i, lang, owners, exportedClasses)
		if !ok {
			continue
		}
		op, end, ok := buildOperation(file, index, tokens, pkg, lang, candidate, owners, fieldTypes, len(result))
		if !ok {
			continue
		}
		result = append(result, op)
		i = end
	}
	remaining := maxOperationsPerFile - len(result)
	if len(compactConstructors) > remaining {
		compactConstructors = compactConstructors[:remaining]
	}
	return append(result, compactConstructors...)
}

func recognizeOperation(tokens []token, index int, language string, owners []string, classes map[string]bool) (operationCandidate, bool) {
	switch language {
	case "go":
		return recognizeGoOperation(tokens, index)
	case "rust":
		return recognizeRustOperation(tokens, index)
	default:
		return recognizeManagedOperation(tokens, index, language, owners, classes)
	}
}

func recognizeGoOperation(tokens []token, index int) (operationCandidate, bool) {
	if tokens[index].text != "func" {
		return operationCandidate{}, false
	}
	j := index + 1
	candidate := operationCandidate{nameIndex: -1, paramOpen: -1, receiverStart: -1, receiverEnd: -1}
	if j < len(tokens) && tokens[j].text == "(" {
		end := matching(tokens, j, "(", ")")
		if end < 0 {
			return operationCandidate{}, false
		}
		candidate.receiverStart, candidate.receiverEnd = j+1, end
		j = end + 1
	}
	if j >= len(tokens) || !isIdentifier(tokens[j].text) || j+1 >= len(tokens) || tokens[j+1].text != "(" {
		return operationCandidate{}, false
	}
	candidate.nameIndex, candidate.exported = j, unicode.IsUpper(rune(tokens[j].text[0]))
	return candidate, true
}

func recognizeRustOperation(tokens []token, index int) (operationCandidate, bool) {
	if tokens[index].text != "fn" || index+1 >= len(tokens) || !isIdentifier(tokens[index+1].text) {
		return operationCandidate{}, false
	}
	j := index + 2
	if j < len(tokens) && tokens[j].text == "<" {
		end := matching(tokens, j, "<", ">")
		if end < 0 {
			return operationCandidate{}, false
		}
		j = end + 1
	}
	if j >= len(tokens) || tokens[j].text != "(" {
		return operationCandidate{}, false
	}
	return operationCandidate{nameIndex: index + 1, paramOpen: j, exported: hasModifier(tokens, index, "pub"), receiverStart: -1, receiverEnd: -1}, true
}

func recognizeManagedOperation(tokens []token, index int, language string, owners []string, classes map[string]bool) (operationCandidate, bool) {
	if functionDeclaration(tokens, index) {
		return operationCandidate{nameIndex: index + 1, paramOpen: -1, exported: hasModifier(tokens, index, "export") || hasModifier(tokens, index, "public"), receiverStart: -1, receiverEnd: -1}, true
	}
	if language == "typescript" {
		if candidate, ok := recognizeArrowOperation(tokens, index); ok {
			return candidate, true
		}
	}
	if !isIdentifier(tokens[index].text) || index+1 >= len(tokens) || tokens[index+1].text != "(" || isControl(tokens[index].text) {
		return operationCandidate{}, false
	}
	close := matching(tokens, index+1, "(", ")")
	if close < 0 || declarationBody(tokens, close) < 0 {
		return operationCandidate{}, false
	}
	private := hasModifier(tokens, index, "private")
	exported := !private && (hasModifier(tokens, index, "public") || hasModifier(tokens, index, "protected") || hasModifier(tokens, index, "export") || hasExportedType(tokens, index) || classes[owners[index]])
	return operationCandidate{nameIndex: index, paramOpen: -1, exported: exported, receiverStart: -1, receiverEnd: -1}, true
}

func functionDeclaration(tokens []token, index int) bool {
	return tokens[index].text == "function" && index+2 < len(tokens) && isIdentifier(tokens[index+1].text) && tokens[index+2].text == "("
}

func recognizeArrowOperation(tokens []token, index int) (operationCandidate, bool) {
	if index+3 >= len(tokens) || tokens[index].text != "const" || !isIdentifier(tokens[index+1].text) || tokens[index+2].text != "=" {
		return operationCandidate{}, false
	}
	exported := hasModifier(tokens, index, "export")
	if tokens[index+3].text == "(" {
		close := matching(tokens, index+3, "(", ")")
		if close >= 0 && arrowToken(tokens, close) >= 0 {
			return operationCandidate{nameIndex: index + 1, paramOpen: index + 3, exported: exported, receiverStart: -1, receiverEnd: -1}, true
		}
	}
	if isIdentifier(tokens[index+3].text) && index+4 < len(tokens) && tokens[index+4].text == "=>" {
		return operationCandidate{nameIndex: index + 1, paramOpen: index + 3, exported: exported, singleArrowParam: true, receiverStart: -1, receiverEnd: -1}, true
	}
	return operationCandidate{}, false
}

func buildOperation(file File, index int, tokens []token, pkg, language string, candidate operationCandidate, owners []string, fields map[string]string, ordinal int) (*operation, int, bool) {
	nameIndex := candidate.nameIndex
	paramOpen := candidate.paramOpen
	if nameIndex < 0 {
		return nil, 0, false
	}
	if paramOpen < 0 {
		paramOpen = nameIndex + 1
	}
	close, parameters, ok := operationParameters(tokens, paramOpen, candidate.singleArrowParam)
	if !ok {
		return nil, 0, false
	}
	bodyStart := operationBodyStart(tokens, close)
	if bodyStart < 0 {
		return nil, 0, false
	}
	body, bodyEnd, ok := operationBody(tokens, bodyStart)
	if !ok {
		return nil, 0, false
	}
	owner, receiverName := owners[nameIndex], ""
	opFields := fields
	if language == "go" && candidate.receiverStart >= 0 && candidate.receiverEnd >= candidate.receiverStart {
		owner, receiverName = goReceiver(tokens[candidate.receiverStart:candidate.receiverEnd])
		opFields = receiverFields(fields, receiverName, owner)
	}
	if language == "go" {
		opFields = goLocalReceiverTypes(body, opFields)
	}
	packageVisible := language == "go" || language == "rust" || language == "java" && !hasModifier(tokens, nameIndex, "private")
	op := &operation{
		id: file.Path + "#" + tokens[nameIndex].text + "/" + itoa(ordinal), name: tokens[nameIndex].text,
		owner: owner, receiverName: receiverName, returnType: operationReturnType(tokens, close, bodyStart, language),
		language: language, pkg: pkg, file: index, params: parameterCount(parameters),
		paramNames: parameterNames(parameters, language), requiredParamNames: requiredParameterNames(parameters, language),
		stringParams: stringParameterNames(parameters, language), fieldTypes: opFields, exposed: candidate.exported,
		packageVisible: packageVisible, body: body,
	}
	op.parameterTypes = gradedParameterTypes(parameters, language)
	op.expressionBody = tokens[bodyStart].text == "=>" && bodyStart+1 < len(tokens) && tokens[bodyStart+1].text != "{"
	return op, bodyEnd, true
}

func operationParameters(tokens []token, open int, single bool) (int, []token, bool) {
	if single {
		if open >= len(tokens) || !isIdentifier(tokens[open].text) {
			return 0, nil, false
		}
		return open, tokens[open : open+1], true
	}
	if open >= len(tokens) || tokens[open].text != "(" {
		return 0, nil, false
	}
	close := matching(tokens, open, "(", ")")
	if close < 0 {
		return 0, nil, false
	}
	return close, tokens[open+1 : close], true
}

func operationBodyStart(tokens []token, close int) int {
	for i := close + 1; i < len(tokens); i++ {
		if tokens[i].text == "{" || tokens[i].text == "=>" {
			return i
		}
	}
	return -1
}

func operationBody(tokens []token, start int) ([]token, int, bool) {
	if tokens[start].text == "=>" {
		if start+1 < len(tokens) && tokens[start+1].text == "{" {
			end := matching(tokens, start+1, "{", "}")
			if end < 0 {
				return nil, 0, false
			}
			return tokens[start+2 : end], end, true
		}
		end := expressionBodyEnd(tokens, start+1)
		return tokens[start+1 : end], end, true
	}
	end := matching(tokens, start, "{", "}")
	if end < 0 {
		end = len(tokens)
	}
	return tokens[start+1 : end], end, true
}

func receiverFields(fields map[string]string, receiverName, owner string) map[string]string {
	if receiverName == "" || owner == "" {
		return fields
	}
	result := cloneStringMap(fields)
	if result == nil {
		result = map[string]string{}
	}
	result[receiverName] = owner
	return result
}

func operationReturnType(tokens []token, close, bodyStart int, language string) string {
	if language != "go" {
		return ""
	}
	return goReturnType(tokens[close+1 : bodyStart])
}

func exportedClassNames(tokens []token) map[string]bool {
	result := map[string]bool{}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text == "export" && tokens[i+1].text == "class" && isIdentifier(tokens[i+2].text) {
			result[tokens[i+2].text] = true
		}
	}
	return result
}

func sourceFieldTypes(tokens []token) map[string]string {
	result, ambiguous := map[string]string{}, map[string]bool{}
	add := func(name, fieldType string) {
		if ambiguous[name] {
			return
		}
		if previous, exists := result[name]; exists && previous != fieldType {
			delete(result, name)
			ambiguous[name] = true
			return
		}
		result[name] = fieldType
	}
	collectStructFieldTypes(tokens, add)
	collectDeclaredFieldTypes(tokens, add)
	return result
}

func collectStructFieldTypes(tokens []token, add func(string, string)) {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != "struct" || tokens[i+1].text != "{" {
			continue
		}
		end := matching(tokens, i+1, "{", "}")
		if end < 0 {
			continue
		}
		for field := i + 2; field+1 < end; field++ {
			if !isIdentifier(tokens[field].text) {
				continue
			}
			if tokens[field+1].text == ":" && field+2 < end && isIdentifier(tokens[field+2].text) {
				add(tokens[field].text, tokens[field+2].text)
			} else if isIdentifier(tokens[field+1].text) && (field == i+2 || tokens[field-1].text == "," || tokens[field-1].text == ";") {
				add(tokens[field].text, tokens[field+1].text)
			}
		}
	}
}

func collectDeclaredFieldTypes(tokens []token, add func(string, string)) {
	for i := 0; i+3 < len(tokens); i++ {
		if isIdentifier(tokens[i].text) && isIdentifier(tokens[i+1].text) && tokens[i+2].text == "=" && tokens[i+3].text == "new" && i+4 < len(tokens) && isIdentifier(tokens[i+4].text) {
			add(tokens[i+1].text, tokens[i+4].text)
		}
		if typedField(tokens, i) {
			add(tokens[i].text, tokens[i+2].text)
		}
	}
}

func typedField(tokens []token, index int) bool {
	if index+2 >= len(tokens) || !isIdentifier(tokens[index].text) || tokens[index+1].text != ":" || !isIdentifier(tokens[index+2].text) {
		return false
	}
	previous := ""
	if index > 0 {
		previous = tokens[index-1].text
	}
	switch previous {
	case "{", ",", ";", "pub", "private", "protected", "readonly", "mut":
		return true
	default:
		return false
	}
}

func goReceiver(tokens []token) (owner, name string) {
	identifiers := make([]string, 0, 2)
	for _, item := range tokens {
		if isIdentifier(item.text) {
			identifiers = append(identifiers, item.text)
		}
	}
	if len(identifiers) == 0 {
		return "", ""
	}
	if len(identifiers) >= 2 && isIdentifier(tokens[0].text) {
		return identifiers[1], identifiers[0]
	}
	return identifiers[0], ""
}

func goReturnType(tokens []token) string {
	if len(tokens) == 1 && tokens[0].text == "string" {
		return "string"
	}
	if len(tokens) == 3 && tokens[0].text == "(" && tokens[1].text == "string" && tokens[2].text == ")" {
		return "string"
	}
	return ""
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
